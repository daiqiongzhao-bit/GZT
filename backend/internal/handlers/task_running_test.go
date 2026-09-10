package handlers

import (
	"testing"
	"time"

	"shiftworkbench/internal/models"
)

// TestDailyTaskRunningWindow v0.14.0：每日任务的三段状态机
//
// 以 09:00 任务、宽限 30 分钟为例：
//
//	~ 08:59      正常（soon=false running=false overdue=false）
//	09:00~09:30  正在执行（running=true）
//	09:30 之后   逾期（overdue=true, running=false）
//
// 背景：此前以「开始时间」前推 30 分钟（08:30~09:00）会标橙色「即将逾期」，
// 但此刻任务还没开始，执行人看到预警会误以为要出事；
// 而 09:00~09:30 这段真正干活的窗口却没有任何标记，看起来像「没开始」。
// 现在改为：到点前保持正常，到点后显示「正在执行」，超宽限期才逾期。
func TestDailyTaskRunningWindow(t *testing.T) {
	// 直接注入宽限期，避免依赖数据库
	graceMinutesCache = 30
	defer func() { graceMinutesCache = -2 }()

	now := time.Now()
	// 相对当前时间构造每日任务，覆盖三个窗口
	mk := func(offsetMin int) models.Task {
		return models.Task{
			Type:   models.TaskTypeDaily,
			Time:   now.Add(time.Duration(offsetMin) * time.Minute).Format("15:04"),
			Status: models.TaskStatusTodo,
		}
	}

	cases := []struct {
		name    string
		offset  int // 任务开始时间 = now + offset 分钟（负数表示已过去）
		wantRun bool
		wantOd  bool
		wantSnd bool
	}{
		{"未到点_还有一小时", 60, false, false, false},
		{"未到点_旧逻辑误报的窗口", 15, false, false, false},
		{"未到点_差一分钟", 1, false, false, false},
		{"刚到点_开始执行", 0, true, false, false},
		{"执行中_已过10分钟", -10, true, false, false},
		{"执行中_已过29分钟", -29, true, false, false},
		{"刚好超宽限_逾期", -31, false, true, false},
		{"已逾期_一小时", -60, false, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := mk(c.offset)
			if got := isRunning(task); got != c.wantRun {
				t.Errorf("running = %v, want %v", got, c.wantRun)
			}
			if got := isOverdue(task); got != c.wantOd {
				t.Errorf("overdue = %v, want %v", got, c.wantOd)
			}
			if got := isSoonOverdue(task); got != c.wantSnd {
				t.Errorf("soon_overdue = %v, want %v", got, c.wantSnd)
			}
		})
	}
}

// TestRunningLeftMinutes 正在执行时应给出剩余分钟数，用于提示「还剩约 N 分钟」。
func TestRunningLeftMinutes(t *testing.T) {
	graceMinutesCache = 30
	defer func() { graceMinutesCache = -2 }()

	now := time.Now()
	task := models.Task{
		Type:   models.TaskTypeDaily,
		Time:   now.Add(-10 * time.Minute).Format("15:04"),
		Status: models.TaskStatusTodo,
	}
	left := runningLeftMinutes(task)
	if left < 19 || left > 20 {
		t.Errorf("开始 10 分钟后应剩约 20 分钟，实际 %d", left)
	}

	// 未开始的任务不应有剩余时间
	future := models.Task{
		Type:   models.TaskTypeDaily,
		Time:   now.Add(60 * time.Minute).Format("15:04"),
		Status: models.TaskStatusTodo,
	}
	if got := runningLeftMinutes(future); got != 0 {
		t.Errorf("未开始的任务剩余应为 0，实际 %d", got)
	}
}

// TestRunningIgnoresDone 已完成的任务不进入「正在执行」，避免已完成的活在界面上闪蓝标。
func TestRunningIgnoresDone(t *testing.T) {
	graceMinutesCache = 30
	defer func() { graceMinutesCache = -2 }()

	now := time.Now()
	task := models.Task{
		Type:     models.TaskTypeDaily,
		Time:     now.Add(-10 * time.Minute).Format("15:04"),
		Status:   models.TaskStatusDone,
		Deadline: "",
	}
	if isRunning(task) {
		t.Error("已完成的任务不应显示正在执行")
	}
}

// TestZeroGraceNoRunningWindow 宽限期为 0（到点即逾期）时不存在执行窗口。
func TestZeroGraceNoRunningWindow(t *testing.T) {
	graceMinutesCache = 0
	defer func() { graceMinutesCache = -2 }()

	now := time.Now()
	task := models.Task{
		Type:   models.TaskTypeDaily,
		Time:   now.Add(-1 * time.Minute).Format("15:04"),
		Status: models.TaskStatusTodo,
	}
	if isRunning(task) {
		t.Error("宽限期为 0 时不应有「正在执行」窗口")
	}
	if !isOverdue(task) {
		t.Error("宽限期为 0 时到点即应逾期")
	}
}
