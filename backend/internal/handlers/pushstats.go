package handlers

// ============================================================================
// 推送运行看板（v0.41.1）
//
// 需求：在 Dashboard 上看到"推送任务跑得怎么样"，而不必点进企微推送页翻日志。
//
// 数据来源刻意只取 wp_logs（企微推送模块的运行日志）。两点说明：
//
//  1. 为什么不是"全站推送"：浏览器 Web Push 目前**没有投递日志**（只有订阅表
//     PushSubscription，发送成功/失败都没落库），硬做只会得到一个永远为 0 的卡片。
//     给 Web Push 补投递埋点属于新增表结构，按约定挪到第二波（v0.41.2）去做。
//     这里先把已有数据的企微推送讲清楚，保证卡片上的每个数字都真实可核对。
//
//  2. 为什么必须降级而不是报错：wp_logs 由 wecompush 模块建表，Dashboard 是全局页，
//     任何人都会请求。若该表因故不存在，查询报错会让**整页 Dashboard 500**。
//     因此先 HasTable 探测，缺失时返回带 available=false 的空统计，页面据此隐藏卡片。
//
// 时间一律走 config.TZ()/Now()（v0.40.13 P1-3 引入），不碰全局 time.Local。
// ============================================================================

import (
	"math"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/wecompush"

	"github.com/gin-gonic/gin"
)

// statusAgg 按状态聚合的运行数
type statusAgg struct {
	Status string
	N      int64
	Sum    int64
}

// pushRunStats 汇总企微推送的运行情况，供 Dashboard 展示。
// 返回 available=false 表示 wp_logs 不可用（表不存在），调用方应隐藏该卡片。
func pushRunStats() gin.H {
	degraded := gin.H{"available": false}
	if db.DB == nil {
		return degraded
	}
	if !db.DB.Migrator().HasTable(&wecompush.WpLog{}) {
		return degraded
	}

	loc := config.TZ()
	now := config.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	weekStart := todayStart.AddDate(0, 0, -6) // 含今天共 7 天

	// 试跑（dry-run）是界面上的交互操作，不算真实运行，与企微推送页既有口径保持一致
	real := []string{"schedule", "manual", "catchup"}

	today := aggSince(todayStart, real)
	week := aggSince(weekStart, real)

	var last wecompush.WpLog
	hasLast := false
	// 同样排除试跑：否则"真跑失败、随后点了一次试跑成功"会让看板显示"最后运行：成功"，
	// 把失败掩盖掉——而看板的意义恰恰是第一时间暴露失败。
	if err := db.DB.Where("trigger <> ?", "dry-run").Order("id desc").First(&last).Error; err == nil {
		hasLast = true
	}

	var recentFail []wecompush.WpLog
	db.DB.Where("status = ?", "failed").Order("id desc").Limit(5).Find(&recentFail)

	rate := 0.0
	if week.Runs > 0 {
		rate = float64(week.Success) / float64(week.Runs) * 100
	}

	out := gin.H{
		"available":    true,
		"today_runs":   today.Runs,
		"today_ok":     today.Success,
		"today_failed": today.Failed,
		"today_empty":  today.Empty,
		"today_pushed": today.Sum,
		"week_runs":    week.Runs,
		"week_ok":      week.Success,
		"week_failed":  week.Failed,
		"week_pushed":  week.Sum,
		// 保留一位小数，避免前端出现 66.66666666666667 这种脏数字
		"week_success_rate": round1(rate),
		"recent_failed":     recentFail,
	}
	if hasLast {
		out["last_run_at"] = last.RunAt.In(loc).Format("2006-01-02 15:04:05")
		out["last_status"] = last.Status
		out["last_task"] = last.TaskName
	}
	return out
}

// aggSince 统计某个时间点以来的运行情况（按状态分组，一次查询拿全，避免 4 次 COUNT）
func aggSince(since time.Time, triggers []string) struct {
	Runs    int64
	Success int64
	Failed  int64
	Empty   int64
	Sum     int64
} {
	var out struct {
		Runs    int64
		Success int64
		Failed  int64
		Empty   int64
		Sum     int64
	}
	var rows []statusAgg
	if err := db.DB.Model(&wecompush.WpLog{}).
		Select("status, COUNT(*) AS n, COALESCE(SUM(count),0) AS sum").
		Where("run_at >= ? AND trigger IN ?", since, triggers).
		Group("status").Scan(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out.Runs += r.N
		out.Sum += r.Sum
		switch r.Status {
		case "success":
			out.Success = r.N
		case "failed":
			out.Failed = r.N
		case "empty":
			out.Empty = r.N
		}
	}
	return out
}

// round1 保留一位小数
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
