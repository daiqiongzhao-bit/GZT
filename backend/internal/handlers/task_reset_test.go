package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// setupResetDB 用独立内存库初始化 db.DB，并清空进程内的周期标记，
// 保证每个用例都会真正跑一遍重置逻辑（标记不清零会被上一个用例的 stamp 短路）。
func setupResetDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.Task{}, &models.Log{}, &models.TaskCompletion{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	resetMu.Lock()
	resetDailyStamp = ""
	resetMonthlyStamp = ""
	resetMu.Unlock()
}

// TestRollMonthlyDeadline 月度截止日推进：保持「几号」与时刻，月末溢出取当月最后一天
func TestRollMonthlyDeadline(t *testing.T) {
	cases := []struct {
		in    string
		year  int
		month time.Month
		want  string
		ok    bool
	}{
		{"2026-08-01T09:00", 2026, time.September, "2026-09-01T09:00", true},  // 常规：日与时分不变
		{"2026-08-15T09:00", 2026, time.September, "2026-09-15T09:00", true},  // 月中
		{"2026-08-31T09:00", 2026, time.September, "2026-09-30T09:00", true},  // 月末溢出：9 月无 31 号
		{"2026-01-31T09:00", 2026, time.February, "2026-02-28T09:00", true},   // 平年 2 月
		{"2028-01-31T09:00", 2028, time.February, "2028-02-29T09:00", true},   // 闰年 2 月
		{"2026-08-05T14:30", 2026, time.September, "2026-09-05T14:30", true},  // 非默认时刻也保留
		{"", 2026, time.September, "", false},                                 // 空截止日不处理
	}
	for _, c := range cases {
		got, ok := RollMonthlyDeadline(c.in, c.year, c.month)
		if ok != c.ok || got != c.want {
			t.Errorf("RollMonthlyDeadline(%q, %d, %v) = (%q, %v)，期望 (%q, %v)",
				c.in, c.year, c.month, got, ok, c.want, c.ok)
		}
	}
}

// TestResetDailyTasks 每日任务：昨天完成的回到待办，今天完成的保留
func TestResetDailyTasks(t *testing.T) {
	setupResetDB(t)
	yest := time.Now().AddDate(0, 0, -1)
	today := time.Now()

	db.DB.Create(&models.Task{Title: "昨日完成", Type: models.TaskTypeDaily, Status: models.TaskStatusDone,
		CompletedBy: "admin", CompletedAt: yest, Time: "09:00"})
	db.DB.Create(&models.Task{Title: "今日完成", Type: models.TaskTypeDaily, Status: models.TaskStatusDone,
		CompletedBy: "admin", CompletedAt: today, Time: "10:00"})
	db.DB.Create(&models.Task{Title: "从未完成", Type: models.TaskTypeDaily, Status: models.TaskStatusTodo, Time: "11:00"})

	n, _ := ResetRecurringTasks()
	if n != 1 {
		t.Fatalf("应只重置 1 条每日任务，实际 %d 条", n)
	}

	var a, b models.Task
	db.DB.Where("title = ?", "昨日完成").First(&a)
	if a.Status != models.TaskStatusTodo || a.CompletedBy != "" {
		t.Errorf("「昨日完成」应被重置为待办且清空完成人，实际 status=%s completed_by=%q", a.Status, a.CompletedBy)
	}
	db.DB.Where("title = ?", "今日完成").First(&b)
	if b.Status != models.TaskStatusDone || b.CompletedBy != "admin" {
		t.Errorf("「今日完成」本周期内已完成，不应被重置，实际 status=%s completed_by=%q", b.Status, b.CompletedBy)
	}
}

// TestResetMonthlyTasks 月度任务：上月完成的回到待办且截止日推进到当月，本月完成的保留
func TestResetMonthlyTasks(t *testing.T) {
	setupResetDB(t)
	now := time.Now()
	lastMonth := now.AddDate(0, -1, 0)
	lastMonthStr := lastMonth.Format("2006-01") + "-05T09:00" // 上月 5 号 09:00 截止

	db.DB.Create(&models.Task{Title: "上月完成", Type: models.TaskTypeMonthly, Status: models.TaskStatusDone,
		CompletedBy: "admin", CompletedAt: lastMonth, Deadline: lastMonthStr})
	db.DB.Create(&models.Task{Title: "本月完成", Type: models.TaskTypeMonthly, Status: models.TaskStatusDone,
		CompletedBy: "admin", CompletedAt: now, Deadline: now.Format("2006-01-02T09:00")})

	_, n := ResetRecurringTasks()
	// 只有「上月完成」需要变更（重置状态 + 推进截止日）；
	// 「本月完成」截止日已在本月、完成时间也在本月，应完全不动
	if n != 1 {
		t.Fatalf("应只变更 1 条月度任务（上月完成），实际 %d 条", n)
	}

	var a models.Task
	db.DB.Where("title = ?", "上月完成").First(&a)
	if a.Status != models.TaskStatusTodo {
		t.Errorf("「上月完成」应回到待办，实际 %s", a.Status)
	}
	if len(a.Deadline) < 7 || a.Deadline[:7] != now.Format("2006-01") {
		t.Errorf("「上月完成」截止日应推进到当月，实际 %q", a.Deadline)
	}
	if a.Deadline[11:16] != "09:00" {
		t.Errorf("截止时刻应保持 09:00，实际 %q", a.Deadline)
	}

	var b models.Task
	db.DB.Where("title = ?", "本月完成").First(&b)
	if b.Status != models.TaskStatusDone {
		t.Errorf("「本月完成」本周期内已完成，不应被重置，实际 %s", b.Status)
	}
}

// TestResetIsIdempotent 重置必须幂等：第二次执行不应再产生变更
func TestResetIsIdempotent(t *testing.T) {
	setupResetDB(t)
	db.DB.Create(&models.Task{Title: "昨日完成", Type: models.TaskTypeDaily, Status: models.TaskStatusDone,
		CompletedBy: "admin", CompletedAt: time.Now().AddDate(0, 0, -1), Time: "09:00"})

	n1, _ := ResetRecurringTasks()
	if n1 != 1 {
		t.Fatalf("首次应重置 1 条，实际 %d", n1)
	}
	// 清空标记，强制再跑一次（模拟进程重启）
	resetMu.Lock()
	resetDailyStamp = ""
	resetMonthlyStamp = ""
	resetMu.Unlock()
	n2, _ := ResetRecurringTasks()
	if n2 != 0 {
		t.Errorf("幂等性被破坏：第二次仍重置了 %d 条", n2)
	}
}

// TestResetKeepsOnceAndHistory 一次性任务不参与重置，且完成历史不被删除
func TestResetKeepsOnceAndHistory(t *testing.T) {
	setupResetDB(t)
	old := time.Now().AddDate(0, 0, -30)
	db.DB.Create(&models.Task{Title: "一次性", Type: models.TaskTypeOnce, Status: models.TaskStatusDone,
		CompletedBy: "admin", CompletedAt: old, Deadline: old.Format("2006-01-02T09:00")})
	db.DB.Create(&models.TaskCompletion{TaskID: 1, TaskTitle: "每日A", UserName: "admin", CompletedAt: old})

	ResetRecurringTasks()

	var once models.Task
	db.DB.Where("title = ?", "一次性").First(&once)
	if once.Status != models.TaskStatusDone {
		t.Errorf("一次性任务不应被重置，实际 %s", once.Status)
	}
	var cnt int64
	db.DB.Model(&models.TaskCompletion{}).Count(&cnt)
	if cnt != 1 {
		t.Errorf("重置不应删除完成历史，实际剩余 %d 条", cnt)
	}
}
