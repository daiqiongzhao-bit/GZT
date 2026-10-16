package handlers

import (
	"fmt"
	"testing"
	"time"

	"shiftworkbench/internal/models"
)

// TestTaskDueAtIgnoresDoneStatus v0.41.16：到点判断只认排期，
// 员工在到点前提前完成（status=done）不应导致漏推。
func TestTaskDueAtIgnoresDoneStatus(t *testing.T) {
	task := models.Task{Type: models.TaskTypeDaily, Time: "09:30", Status: models.TaskStatusDone}
	if !taskDueAt(task, "09:30", "2026-10-16") {
		t.Fatalf("已完成的任务到点也应推送（v0.41.16 修复漏推）")
	}
	if taskDueAt(task, "09:31", "2026-10-16") {
		t.Fatalf("非到点分钟不应推送")
	}
}

// TestTaskDueAtDailyNoWeekdays 未勾选星期=每天都推。
func TestTaskDueAtDailyNoWeekdays(t *testing.T) {
	task := models.Task{Type: models.TaskTypeDaily, Time: "09:00"}
	if !taskDueAt(task, "09:00", "2026-10-16") {
		t.Fatalf("未勾选星期时应每天推")
	}
}

// TestTaskDueAtWeekly 按周任务只在命中星期推（1=周一…7=周日）。
func TestTaskDueAtWeekly(t *testing.T) {
	wd := (int(time.Now().Weekday())+6)%7 + 1
	other := wd%7 + 1
	task := models.Task{Type: models.TaskTypeDaily, Time: "09:30", WeekDays: fmt.Sprint(wd)}
	if !taskDueAt(task, "09:30", "2026-10-16") {
		t.Fatalf("命中星期应推送, wd=%d", wd)
	}
	task.WeekDays = fmt.Sprint(other)
	if taskDueAt(task, "09:30", "2026-10-16") {
		t.Fatalf("非命中星期不应推送, wd=%d other=%d", wd, other)
	}
}

// TestTaskDueAtOnceMonthly 单次/每月按 deadline 精确匹配。
func TestTaskDueAtOnceMonthly(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")
	hm := now.Format("15:04")
	once := models.Task{Type: models.TaskTypeOnce, Deadline: now.Format("2006-01-02T15:04")}
	if !taskDueAt(once, hm, today) {
		t.Fatalf("单次任务到点应推送")
	}
	mon := models.Task{Type: models.TaskTypeMonthly, Deadline: now.Format("2006-01-02T15:04")}
	if !taskDueAt(mon, hm, today) {
		t.Fatalf("每月任务到点应推送")
	}
}
