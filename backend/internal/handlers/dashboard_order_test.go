package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// setupDashDB 独立内存库，覆盖 Dashboard 需要的全部表
func setupDashDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	err = d.AutoMigrate(
		&models.Task{}, &models.Schedule{}, &models.Department{}, &models.User{},
		&models.ShiftConfig{}, &models.Setting{}, &models.Log{}, &models.TaskCompletion{},
	)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	resetMu.Lock()
	resetDailyStamp = ""
	resetMonthlyStamp = ""
	resetMu.Unlock()
	return d
}

// TestDashboardTodayListSortedByTime 守住「20:00 的晚班任务被排到 21:50 之后」的问题：
// Dashboard 的 today_task_list 必须按到点时间正序返回，
// 而不是数据库物理行序（≈ 创建顺序）—— 插件弹窗读的就是这个列表。
func TestDashboardTodayListSortedByTime(t *testing.T) {
	d := setupDashDB(t)

	now := time.Now()
	// 用「相对当前时刻」的时间，保证任何时候跑都不受系统时钟影响；
	// 同一批任务按 HH:MM 单调递增，故期望顺序与真实时钟无关。
	hm := func(delta time.Duration) string { return now.Add(delta).Format("15:04") }

	// 创建顺序刻意与时间顺序错开：晚班 20:00 这条最后创建（正是线上被顶到底部的那条）
	seed := []models.Task{
		{Title: "中班-20:00", Type: models.TaskTypeDaily, Shift: "中班", Time: hm(2 * time.Hour), Status: models.TaskStatusTodo},
		{Title: "中班-20:30", Type: models.TaskTypeDaily, Shift: "中班", Time: hm(150 * time.Minute), Status: models.TaskStatusTodo},
		{Title: "中班-21:00", Type: models.TaskTypeDaily, Shift: "中班", Time: hm(3 * time.Hour), Status: models.TaskStatusTodo},
		{Title: "中班-21:50", Type: models.TaskTypeDaily, Shift: "中班", Time: hm(230 * time.Minute), Status: models.TaskStatusTodo},
		{Title: "晚班-20:00", Type: models.TaskTypeDaily, Shift: "晚班", Time: hm(2 * time.Hour), Status: models.TaskStatusTodo},
	}
	for i := range seed {
		seed[i].CreatedAt = now.Add(time.Duration(i) * time.Second)
		if err := d.Create(&seed[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	Dashboard(c)

	if w.Code != http.StatusOK {
		t.Fatalf("Dashboard 返回 %d：%s", w.Code, w.Body.String())
	}
	var resp struct {
		Today []models.Task `json:"today_task_list"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(resp.Today))
	for _, x := range resp.Today {
		got = append(got, x.Title)
	}
	want := []string{"中班-20:00", "晚班-20:00", "中班-20:30", "中班-21:00", "中班-21:50"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("今日待办顺序错误\n  got : %v\n  want: %v", got, want)
	}

	// 顺序应与 /api/tasks（ListTasks）趋于一致：同一批任务在两个入口的首项必须相同
	if len(resp.Today) > 0 && resp.Today[0].Title != want[0] {
		t.Fatalf("首项应为 %s，实为 %s", want[0], resp.Today[0].Title)
	}
}

// TestTaskUrgencyKeyFallsBackToTime 无 deadline 的周期任务（每周等）应按到点时点排序，
// 不能因为没有 deadline 被一律沉到最底。
func TestTaskUrgencyKeyFallsBackToTime(t *testing.T) {
	nowStr := time.Now().Format("2006-01-02T15:04")
	early := models.Task{Type: "weekly", Time: "08:30"}
	late := models.Task{Type: "weekly", Time: "18:00"}
	none := models.Task{Type: "weekly"}

	if taskUrgencyKey(early, nowStr) >= taskUrgencyKey(late, nowStr) {
		t.Fatalf("每周任务应按到点时间正序：%s >= %s",
			taskUrgencyKey(early, nowStr), taskUrgencyKey(late, nowStr))
	}
	if taskUrgencyKey(none, nowStr) != "9999-12-31T23:59" {
		t.Fatalf("无时间的任务应排最后，实为 %s", taskUrgencyKey(none, nowStr))
	}
}
