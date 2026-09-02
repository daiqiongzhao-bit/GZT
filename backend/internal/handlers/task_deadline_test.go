package handlers

import (
	"testing"
	"time"

	"shiftworkbench/internal/models"
)

// TestMonthlyOverdueEndOfDay 月度任务以「截止日当天结束」为界：
// 截止日当天任何时候完成都算准时，次日仍未完成才判逾期。
//
// 注意：deadline 里的 09:00 只是晨间推送提醒时点，不是完成期限——
// 月度工作（超时订单、盘点建单等）一整天都能处理，
// 若按 09:00 判逾期，等于早上 9 点一到就把当天该做的活全标红。
func TestMonthlyOverdueEndOfDay(t *testing.T) {
	now := time.Now()
	fmt := "2006-01-02"
	mk := func(dl string) models.Task {
		return models.Task{Type: models.TaskTypeMonthly, Deadline: dl, Status: models.TaskStatusTodo}
	}

	if !isOverdue(mk(now.AddDate(0, 0, -1).Format(fmt) + "T09:00")) {
		t.Error("昨天截止的月度任务应逾期")
	}
	if isOverdue(mk(now.AddDate(0, 0, 1).Format(fmt) + "T09:00")) {
		t.Error("明天截止的月度任务不应逾期")
	}
	if isOverdue(mk("")) {
		t.Error("无截止时间的月度任务不应逾期")
	}
	if isOverdue(mk("垃圾数据")) {
		t.Error("格式非法的截止时间不应判逾期")
	}
	done := mk(now.AddDate(0, 0, -1).Format(fmt) + "T09:00")
	done.Status = models.TaskStatusDone
	if isOverdue(done) {
		t.Error("已完成的月度任务不应逾期")
	}

	// 跨月回归：上月的今天，旧逻辑只比「日」会误判成「今天到期」
	if !isOverdue(mk(now.AddDate(0, -1, 0).Format(fmt) + "T09:00")) {
		t.Error("上月的月度任务应逾期（跨月不得误判）")
	}

	// 当天截止：整天都算数，无论当前几点都不逾期。
	// 这里刻意用 09:00 与 00:01 两种截止时刻，验证时刻已不再参与逾期判定
	for _, hm := range []string{"00:01", "09:00", "12:00", "23:59"} {
		if isOverdue(mk(now.Format(fmt) + "T" + hm)) {
			t.Errorf("今天 %s 截止的月度任务，当天内不应逾期（当前 %s）", hm, now.Format("15:04"))
		}
	}
}

// TestNormalizeTaskDeadline 月度任务缺省时间补 09:00（晨间提醒时点），其余类型补 00:00，显式给的时间不被覆盖
func TestNormalizeTaskDeadline(t *testing.T) {
	cases := []struct {
		typ, in, want string
	}{
		{"monthly", "2026-08-01", "2026-08-01T09:00"},
		{"monthly", "2026-08-01 17:30", "2026-08-01T17:30"}, // 显式指定时间不覆盖
		{"monthly", "2026-08-01T18:00", "2026-08-01T18:00"},
		{"monthly", "", ""},
		{"monthly", "1号", ""},
		{"once", "2026-08-01", "2026-08-01T00:00"},
		{"daily", "2026-08-01", "2026-08-01T00:00"},
		{"", "2026-08-01", "2026-08-01T00:00"},
	}
	for _, c := range cases {
		if got := normalizeTaskDeadline(c.typ, c.in); got != c.want {
			t.Errorf("normalizeTaskDeadline(%q, %q) = %q，期望 %q", c.typ, c.in, got, c.want)
		}
	}
	if monthlyDueTime != "09:00" {
		t.Errorf("月度默认提醒时点应为 09:00，实际 %s", monthlyDueTime)
	}
}
