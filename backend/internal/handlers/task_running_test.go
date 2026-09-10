package handlers

import (
	"testing"
	"time"

	"shiftworkbench/internal/models"
)

// TestDailyTaskRunningWindow v0.14.1：每日任务的四段状态机
//
// 以 09:00 任务、宽限/提前量 30 分钟为例：
//
//	~ 08:29      正常（starting=false running=false overdue=false）
//	08:30~08:59  即将开始（starting=true）
//	09:00~09:30  正在执行（running=true）
//	09:30 之后   逾期（overdue=true, running=false）
//
// 背景：
//   - v0.14.0 之前：以「开始时间」前推 30 分钟（08:30~09:00）标橙色「即将逾期」，
//     但此刻任务还没开始，执行人看到预警会误以为要出事；
//     而 09:00~09:30 这段真正干活的窗口却没有任何标记，看起来像「没开始」。
//   - v0.14.0：到点前保持正常，到点后显示「正在执行」，超宽限期才逾期。
//   - v0.14.1：到点前 30 分钟内改为青色「即将开始」，既不误报逾期，也给执行人提前量。
func TestDailyTaskRunningWindow(t *testing.T) {
	// 直接注入宽限期，避免依赖数据库
	graceMinutesCache = 30
	defer func() { graceMinutesCache = -2 }()

	now := time.Now()
	// 相对当前时间构造每日任务，覆盖四个窗口
	mk := func(offsetMin int) models.Task {
		return models.Task{
			Type:   models.TaskTypeDaily,
			Time:   now.Add(time.Duration(offsetMin) * time.Minute).Format("15:04"),
			Status: models.TaskStatusTodo,
		}
	}

	cases := []struct {
		name       string
		offset     int  // 任务开始时间 = now + offset 分钟（负数表示已过去）
		wantStart  bool // 即将开始
		wantRun    bool // 正在执行
		wantOd     bool // 逾期
		wantSoonOd bool // 即将逾期（每日任务恒 false）
	}{
		{"未到点_还有一小时", 60, false, false, false, false},
		{"未到点_刚出提前量窗口", 31, false, false, false, false},
		{"即将开始_还有29分钟", 29, true, false, false, false},
		{"即将开始_还有15分钟", 15, true, false, false, false},
		{"即将开始_差一分钟", 1, true, false, false, false},
		{"刚到点_开始执行", 0, false, true, false, false},
		{"执行中_已过10分钟", -10, false, true, false, false},
		{"执行中_已过29分钟", -29, false, true, false, false},
		{"刚好超宽限_逾期", -31, false, false, true, false},
		{"已逾期_一小时", -60, false, false, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := mk(c.offset)
			if got := isStarting(task); got != c.wantStart {
				t.Errorf("starting = %v, want %v", got, c.wantStart)
			}
			if got := isRunning(task); got != c.wantRun {
				t.Errorf("running = %v, want %v", got, c.wantRun)
			}
			if got := isOverdue(task); got != c.wantOd {
				t.Errorf("overdue = %v, want %v", got, c.wantOd)
			}
			if got := isSoonOverdue(task); got != c.wantSoonOd {
				t.Errorf("soon_overdue = %v, want %v", got, c.wantSoonOd)
			}
		})
	}
}

// TestStartingInMinutes 即将开始的任务应给出「还有约 N 分钟开始」，
// 且窗口外的任务返回 0，避免界面出现无意义文案。
func TestStartingInMinutes(t *testing.T) {
	graceMinutesCache = 30
	defer func() { graceMinutesCache = -2 }()

	now := time.Now()
	soon := models.Task{
		Type:   models.TaskTypeDaily,
		Time:   now.Add(10 * time.Minute).Format("15:04"),
		Status: models.TaskStatusTodo,
	}
	if in := startingInMinutes(soon); in < 9 || in > 10 {
		t.Errorf("10 分钟后开始应提示约 10 分钟，实际 %d", in)
	}

	far := models.Task{
		Type:   models.TaskTypeDaily,
		Time:   now.Add(90 * time.Minute).Format("15:04"),
		Status: models.TaskStatusTodo,
	}
	if got := startingInMinutes(far); got != 0 {
		t.Errorf("窗口外的任务 starting_in 应为 0，实际 %d", got)
	}
}

// TestStartingOnlyForDaily v0.14.1：「即将开始」只针对每日任务。
// 单次/月度任务的 deadline 就是截止时刻，已由 soon_overdue 覆盖，不应再叠一层。
func TestStartingOnlyForDaily(t *testing.T) {
	graceMinutesCache = 30
	defer func() { graceMinutesCache = -2 }()

	dl := time.Now().Add(10 * time.Minute).Format("2006-01-02T15:04")
	for _, typ := range []string{models.TaskTypeOnce, models.TaskTypeMonthly} {
		task := models.Task{Type: typ, Deadline: dl, Status: models.TaskStatusTodo}
		if isStarting(task) {
			t.Errorf("%s 任务不应显示「即将开始」", typ)
		}
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
