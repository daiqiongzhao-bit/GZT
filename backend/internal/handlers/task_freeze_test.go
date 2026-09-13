package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
)

// setupFreezeDB 用独立内存库初始化 db.DB，并清空进程内的周期标记
func setupFreezeDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.Task{}, &models.Log{}, &models.TaskCompletion{}, &models.Setting{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	resetMu.Lock()
	resetDailyStamp = ""
	resetMonthlyStamp = ""
	resetMu.Unlock()
}

// newTaskCtx 构造带 claims 与 :id 路径参数的 gin 上下文
func newTaskCtx(t *testing.T, method, path string, id uint, body string, cl *models.Claims) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var r *strings.Reader
	if body == "" {
		r = strings.NewReader("")
	} else {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(int(id))}}
	c.Set(middleware.CtxUserKey, cl)
	return c, w
}

// TestFrozenTaskSuppressedFromReminders 冻结后所有「提醒判定」一律为 false ——
// 这是到点推送 / 每日汇总 / 手动推送 / 导航角标是否排除该任务的唯一开关。
func TestFrozenTaskSuppressedFromReminders(t *testing.T) {
	setupFreezeDB(t)

	base := models.Task{
		Title:  "交接班巡检",
		Type:   models.TaskTypeDaily,
		Shift:  "全员",
		Time:   "00:00", // 早于当前时刻 → 未冻结时应判逾期
		Status: models.TaskStatusTodo,
	}

	// 未冻结：每日任务（未勾按周）应当「今日到期」
	live := base
	if !isDueToday(live) {
		t.Fatalf("未冻结的每日任务应判定为今日到期")
	}
	if !isOverdue(live) {
		t.Fatalf("未冻结且已过执行时间的每日任务应判定为逾期")
	}

	// 冻结后：全部提醒判定必须为 false
	frozen := base
	frozen.Frozen = true
	checks := []struct {
		name string
		got  bool
	}{
		{"isDueToday", isDueToday(frozen)},
		{"isDueThisMonth", isDueThisMonth(frozen)},
		{"isOverdue", isOverdue(frozen)},
		{"isSoonOverdue", isSoonOverdue(frozen)},
		{"isRunning", isRunning(frozen)},
		{"isStarting", isStarting(frozen)},
	}
	for _, ck := range checks {
		if ck.got {
			t.Errorf("冻结任务的 %s 应为 false，实际为 true", ck.name)
		}
	}
}

// TestFrozenTaskSurvivesRecurringReset 冻结任务不参与每日/月度周期重置，保持静止
func TestFrozenTaskSurvivesRecurringReset(t *testing.T) {
	setupFreezeDB(t)
	yesterday := time.Now().AddDate(0, 0, -1)

	live := models.Task{Title: "未冻结-昨日已完成", Type: models.TaskTypeDaily, Shift: "全员",
		Status: models.TaskStatusDone, CompletedAt: yesterday}
	frozenT := models.Task{Title: "冻结-昨日已完成", Type: models.TaskTypeDaily, Shift: "全员",
		Status: models.TaskStatusDone, CompletedAt: yesterday, Frozen: true}
	if err := db.DB.Create(&live).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&frozenT).Error; err != nil {
		t.Fatal(err)
	}

	ResetRecurringTasks()

	var gotLive, gotFrozen models.Task
	db.DB.First(&gotLive, live.ID)
	db.DB.First(&gotFrozen, frozenT.ID)

	if gotLive.Status != models.TaskStatusTodo {
		t.Errorf("未冻结的每日任务应被重置回 todo，实际 %s", gotLive.Status)
	}
	if gotFrozen.Status != models.TaskStatusDone {
		t.Errorf("冻结任务不应被重置，应保持 done，实际 %s", gotFrozen.Status)
	}
	if !gotFrozen.Frozen {
		t.Errorf("冻结标志不应被重置逻辑清掉")
	}
}

// TestFreezeTaskEndpoint 冻结/解冻接口：翻转、幂等、记录操作人与时间
func TestFreezeTaskEndpoint(t *testing.T) {
	setupFreezeDB(t)
	cl := &models.Claims{UserID: 1, Username: "admin", Role: models.RoleSuperAdmin, DeptID: 0}

	task := models.Task{Title: "月度盘点", Type: models.TaskTypeMonthly, Shift: "全员",
		Status: models.TaskStatusTodo, Deadline: "2026-09-30T09:00"}
	if err := db.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	// ① 冻结
	c, w := newTaskCtx(t, "POST", "/tasks/1/freeze", task.ID, `{"frozen":true}`, cl)
	FreezeTask(c)
	if w.Code != http.StatusOK {
		t.Fatalf("冻结应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var out models.Task
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Frozen || out.FrozenBy != "admin" || out.FrozenAt.IsZero() {
		t.Fatalf("冻结结果不正确: frozen=%v by=%q at=%v", out.Frozen, out.FrozenBy, out.FrozenAt)
	}
	var stored models.Task
	db.DB.First(&stored, task.ID)
	if !stored.Frozen {
		t.Fatalf("冻结状态未落库")
	}

	// ② 冻结中的任务不能勾完成
	c2, w2 := newTaskCtx(t, "POST", "/tasks/1/toggle", task.ID, `{"to":"done"}`, cl)
	ToggleTask(c2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("冻结任务勾完成应被拒绝(400)，实际 %d，body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "冻结") {
		t.Errorf("拒绝原因应提示已冻结，实际 %s", w2.Body.String())
	}
	db.DB.First(&stored, task.ID)
	if stored.Status == models.TaskStatusDone {
		t.Fatalf("冻结任务不应被标记完成")
	}

	// ③ 解冻
	c3, w3 := newTaskCtx(t, "POST", "/tasks/1/freeze", task.ID, `{"frozen":false}`, cl)
	FreezeTask(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("解冻应返回 200，实际 %d", w3.Code)
	}
	db.DB.First(&stored, task.ID)
	if stored.Frozen || stored.FrozenBy != "" || !stored.FrozenAt.IsZero() {
		t.Fatalf("解冻未清空标记: frozen=%v by=%q at=%v", stored.Frozen, stored.FrozenBy, stored.FrozenAt)
	}

	// ④ 解冻后恢复「今日到期」判定，提醒链路重新生效
	if !isDueThisMonth(stored) {
		t.Errorf("解冻后月度任务应重新参与提醒判定")
	}

	// ⑤ 幂等：重复冻结不应报错，状态保持一致
	c4, w4 := newTaskCtx(t, "POST", "/tasks/1/freeze", task.ID, `{"frozen":true}`, cl)
	FreezeTask(c4)
	c5, w5 := newTaskCtx(t, "POST", "/tasks/1/freeze", task.ID, `{"frozen":true}`, cl)
	FreezeTask(c5)
	if w4.Code != http.StatusOK || w5.Code != http.StatusOK {
		t.Fatalf("重复冻结应幂等返回 200，实际 %d / %d", w4.Code, w5.Code)
	}
	db.DB.First(&stored, task.ID)
	if !stored.Frozen {
		t.Fatalf("重复冻结后状态应为 frozen")
	}
}
