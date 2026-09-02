package handlers

import (
	"fmt"
	"sync"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// ===================== 周期任务自动重置 =====================
//
// 业务背景：每日任务是「每天都要做一遍的重复工作」，月度任务是「每月做一遍」。
// 因此它们的完成状态不能永久保留——新的一天 / 新的一月开始，必须全部回到待办。
// 否则会出现：8/31 完成的任务到 9/1 仍显示「已完成」，而 9/1 该做的事根本没人做，
// 用户只能每天手动把几十条任务一条条点回「待办」。
//
// 设计要点：
//  1. 幂等：按「完成时间是否属于当前周期」判断，重复执行无副作用，
//     进程重启、容器休眠、定时任务错过都不会导致状态错乱；
//  2. 双重触发：接口读取前懒执行 + 每天 00:01 定时兜底，任一触发即可生效；
//  3. 只改状态不动历史：task_completions 表的完成审计记录原样保留，仍可回溯查询；
//  4. 一次性任务（once）有固定截止日，不参与重置。

var resetMu sync.Mutex

// resetDailyStamp / resetMonthlyStamp：进程内「上次重置所处的周期标记」。
// 同一周期内只真正落库一次，避免每个接口请求都写库。
var (
	resetDailyStamp   string
	resetMonthlyStamp string
)

// completedIn 判断完成时间是否落在当前周期内。
// 零值时间（从未完成）与异常年份（<2000，历史脏数据）均视为「不属于当前周期」。
func completedIn(t time.Time, layout, cur string) bool {
	if t.IsZero() || t.Year() < 2000 {
		return false
	}
	return t.Format(layout) == cur
}

// RollMonthlyDeadline 把月度任务截止日推进到指定年月，保持「几号」与时刻不变。
// 月末溢出自动取当月最后一天：8/31 → 9/30（9 月无 31 号），1/31 → 2/28（闰年 2/29）。
func RollMonthlyDeadline(deadline string, year int, month time.Month) (string, bool) {
	if len(deadline) < 10 {
		return deadline, false
	}
	src, err := time.ParseInLocation("2006-01-02", deadline[:10], time.Local)
	if err != nil {
		return deadline, false
	}
	day := src.Day()
	hh, mm := 9, 0 // 无时刻信息时按 09:00 处理（与月度任务默认截止时间一致）
	if len(deadline) >= 16 {
		if c, err := time.ParseInLocation("15:04", deadline[11:16], time.Local); err == nil {
			hh, mm = c.Hour(), c.Minute()
		}
	}
	// 目标月最后一天 = 下个月的第 0 天
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.Local).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, hh, mm, 0, 0, time.Local).Format("2006-01-02T15:04"), true
}

// ResetRecurringTasks 幂等重置周期任务，返回本次实际变更的 (每日条数, 月度条数)。
//   - daily  ：完成时间不属于今天的一律回到待办（今天内完成的保留）
//   - monthly：完成时间不属于本月的回到待办，同时把早于本月的截止日推进到当月
//   - once   ：不参与重置
func ResetRecurringTasks() (int, int) {
	resetMu.Lock()
	defer resetMu.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")
	thisMonth := now.Format("2006-01")
	dailyN, monthlyN := 0, 0
	monthlyReset, monthlyRolled := 0, 0 // 月度：状态重置条数 / 截止日推进条数（两者可重叠）

	// —— 每日任务：跨日即回到待办 ——
	if resetDailyStamp != today {
		var list []models.Task
		db.DB.Where("type = ? AND status = ?", models.TaskTypeDaily, models.TaskStatusDone).Find(&list)
		for _, t := range list {
			// 今天内完成的视为「本周期已做」，保留；其余（昨天及更早 / 无完成时间）一律重置
			if completedIn(t.CompletedAt, "2006-01-02", today) {
				continue
			}
			db.DB.Model(&models.Task{}).Where("id = ?", t.ID).Updates(map[string]interface{}{
				"status":       models.TaskStatusTodo,
				"completed_by": "",
			})
			dailyN++
		}
		resetDailyStamp = today
	}

	// —— 月度任务：跨月回到待办，且截止日推进到当月 ——
	if resetMonthlyStamp != thisMonth {
		var list []models.Task
		db.DB.Where("type = ?", models.TaskTypeMonthly).Find(&list)
		for _, t := range list {
			changed := map[string]interface{}{}
			// 1) 截止日早于本月 → 推进到当月（保持「几号」与时刻，月末自动取最后一天）
			if len(t.Deadline) >= 7 && t.Deadline[:7] < thisMonth {
				if nd, ok := RollMonthlyDeadline(t.Deadline, now.Year(), now.Month()); ok && nd != t.Deadline {
					changed["deadline"] = nd
				}
			}
			// 2) 已完成但完成时间不属于本月 → 回到待办（本月内完成的保留，不误伤）
			if t.Status == models.TaskStatusDone && !completedIn(t.CompletedAt, "2006-01", thisMonth) {
				changed["status"] = models.TaskStatusTodo
				changed["completed_by"] = ""
			}
			if len(changed) > 0 {
				db.DB.Model(&models.Task{}).Where("id = ?", t.ID).Updates(changed)
				monthlyN++
				if _, ok := changed["status"]; ok {
					monthlyReset++
				}
				if _, ok := changed["deadline"]; ok {
					monthlyRolled++
				}
			}
		}
		resetMonthlyStamp = thisMonth
	}

	if dailyN > 0 || monthlyN > 0 {
		// 文案区分「状态重置」与「截止日推进」：两者不等同，
		// 本月内已完成的任务只会推进截止日、不会回到待办，日志若混写会误导排查
		addLog(nil, 0, "系统", fmt.Sprintf(
			"周期任务自动重置（%s）：每日 %d 条回到待办；月度 %d 条回到待办、%d 条截止日推进到当月",
			thisMonth, dailyN, monthlyReset, monthlyRolled))
	}
	return dailyN, monthlyN
}
