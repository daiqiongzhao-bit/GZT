package handlers

import (
	"testing"

	"shiftworkbench/internal/models"
)

// TestTaskNotifyPeopleAssigneePriority v0.41.15：
// 任务指定了负责人时只@负责人，不因班次为「全员」而扩展到所有人员。
func TestTaskNotifyPeopleAssigneePriority(t *testing.T) {
	task := models.Task{Shift: "全员", Assignee: "刘海龙"}
	got := taskNotifyPeople(task, nil, nil)
	if len(got) != 1 || got[0] != "刘海龙" {
		t.Fatalf("有负责人时应只@负责人，got=%v", got)
	}
}

// TestTaskNotifyPeopleFallbackToShift 无负责人时按班次取当班人员（onDuty 为空则无人可@）。
func TestTaskNotifyPeopleFallbackToShift(t *testing.T) {
	task := models.Task{Shift: "早班"}
	if got := taskNotifyPeople(task, map[string][]string{}, nil); len(got) != 0 {
		t.Fatalf("无负责人且班表为空时应无人可@，got=%v", got)
	}
}
