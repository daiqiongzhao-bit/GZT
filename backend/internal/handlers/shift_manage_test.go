package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
)

// setupAPIDB 建立带 HTTP 层的测试环境。
func setupAPIDB(t *testing.T) *gin.Engine {
	t.Helper()
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"})
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// asUser 构造携带指定身份的请求上下文。
func asUser(role models.Role, userID, deptID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("claims", &models.Claims{
			UserID: userID, Username: fmt.Sprintf("u%d", userID),
			Role: role, DeptID: deptID,
		})
		c.Next()
	}
}

// call 执行一次请求并把响应解析为 map。
func call(t *testing.T, method, path string, body interface{}, claims *models.Claims, h gin.HandlerFunc) (int, map[string]interface{}) {
	t.Helper()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("claims", claims)
		}
		c.Next()
	})
	switch method {
	case http.MethodGet:
		r.GET(path, h)
	case http.MethodPost:
		r.POST(path, h)
	case http.MethodPut:
		r.PUT(path, h)
	case http.MethodDelete:
		r.DELETE(path, h)
	}
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	out := map[string]interface{}{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

var (
	superClaims = &models.Claims{UserID: 1, Username: "admin", Role: models.RoleSuperAdmin, DeptID: 0}
	execClaims  = &models.Claims{UserID: 99, Username: "emp", Role: models.RoleExecutor, DeptID: 1}
)

// TestRuleDefaultsReturned 未配置规则时应返回默认值（连续休3/连续上6/月出勤22）。
func TestRuleDefaultsReturned(t *testing.T) {
	setupAPIDB(t)
	code, body := call(t, http.MethodGet, "/r", nil, superClaims,
		func(c *gin.Context) { c.Request.URL.RawQuery = "dept_id=1"; GetShiftRule(c) })
	if code != 200 {
		t.Fatalf("状态码 %d", code)
	}
	if body["max_rest_streak"].(float64) != 3 ||
		body["max_work_streak"].(float64) != 6 ||
		body["month_work_days"].(float64) != 22 {
		t.Fatalf("默认值不符: %+v", body)
	}
	t.Logf("默认规则返回正确: 连续休%v 连续上%v 月出勤%v",
		body["max_rest_streak"], body["max_work_streak"], body["month_work_days"])
}

// TestUpdateRuleValidatesRange 参数越界必须被拒绝。
func TestUpdateRuleValidatesRange(t *testing.T) {
	setupAPIDB(t)
	cases := []struct {
		label string
		body  map[string]interface{}
	}{
		{"连续休为0", map[string]interface{}{"dept_id": 1, "max_rest_streak": 0, "max_work_streak": 6, "month_work_days": 22}},
		{"连续上为99", map[string]interface{}{"dept_id": 1, "max_rest_streak": 3, "max_work_streak": 99, "month_work_days": 22}},
		{"月出勤为0", map[string]interface{}{"dept_id": 1, "max_rest_streak": 3, "max_work_streak": 6, "month_work_days": 0}},
		{"每班人数为负", map[string]interface{}{"dept_id": 1, "max_rest_streak": 3, "max_work_streak": 6, "month_work_days": 22, "min_per_shift": -1}},
		{"早班名不存在", map[string]interface{}{"dept_id": 1, "max_rest_streak": 3, "max_work_streak": 6, "month_work_days": 22, "morning_shift_name": "火星班"}},
	}
	for _, c := range cases {
		code, body := call(t, http.MethodPut, "/r", c.body, superClaims, UpdateShiftRule)
		if code != http.StatusBadRequest {
			t.Fatalf("[%s] 应返回 400，实际 %d (%+v)", c.label, code, body)
		}
	}
	t.Log("越界与非法班次名均已被拒绝")
}

// TestUpdateRuleSavesAndReadsBack 保存规则后回读一致。
func TestUpdateRuleSavesAndReadsBack(t *testing.T) {
	setupAPIDB(t)
	payload := map[string]interface{}{
		"dept_id": 1, "max_rest_streak": 2, "max_work_streak": 5,
		"month_work_days": 24, "min_per_shift": 2,
		"require_morning_before_rest": true, "require_evening_after_rest": true,
		"morning_shift_name": "早班", "evening_shift_name": "晚班",
	}
	code, body := call(t, http.MethodPut, "/r", payload, superClaims, UpdateShiftRule)
	if code != 200 {
		t.Fatalf("保存失败 %d (%+v)", code, body)
	}
	// 回读
	_, got := call(t, http.MethodGet, "/r", nil, superClaims,
		func(c *gin.Context) { c.Request.URL.RawQuery = "dept_id=1"; GetShiftRule(c) })
	if got["max_rest_streak"].(float64) != 2 || got["min_per_shift"].(float64) != 2 ||
		got["require_morning_before_rest"].(bool) != true {
		t.Fatalf("回读不一致: %+v", got)
	}
	t.Logf("规则已保存并回读一致: 连续休%v 连续上%v 月出勤%v 每班≥%v",
		got["max_rest_streak"], got["max_work_streak"], got["month_work_days"], got["min_per_shift"])
}

// TestEmployeeCannotEditLockedRequest 规则4 核心：
// 员工提交需求后即锁定，本人不可再改、不可删除；管理员可解锁。
func TestEmployeeCannotEditLockedRequest(t *testing.T) {
	setupAPIDB(t)
	var u models.User
	db.DB.Where("emp_no = ?", "A001").First(&u)

	// 员工本人提交
	empClaims := &models.Claims{UserID: u.ID, Username: u.Username, Role: models.RoleExecutor, DeptID: 1}
	code, body := call(t, http.MethodPost, "/r", map[string]interface{}{
		"type": "rest", "repeat": "once", "start_date": "2026-09-25", "end_date": "2026-09-28",
	}, empClaims, CreateShiftRequest)
	if code != 200 {
		t.Fatalf("提交失败 %d (%+v)", code, body)
	}
	if body["status"] != models.PrefStatusLocked {
		t.Fatalf("提交后应为 locked，实际 %v", body["status"])
	}
	rid := uint(body["id"].(float64))

	// 员工本人尝试修改 → 403
	code, body = call(t, http.MethodPut, "/r", map[string]interface{}{
		"start_date": "2026-09-20", "end_date": "2026-09-21",
	}, empClaims, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(rid)}}
		UpdateShiftRequest(c)
	})
	if code != http.StatusForbidden {
		t.Fatalf("员工修改锁定需求应 403，实际 %d (%+v)", code, body)
	}

	// 员工本人尝试删除 → 403
	code, _ = call(t, http.MethodDelete, "/r", nil, empClaims,
		func(c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(rid)}}
			DeleteShiftRequest(c)
		})
	if code != http.StatusForbidden {
		t.Fatalf("员工删除锁定需求应 403，实际 %d", code)
	}

	// 员工尝试解锁 → 403
	code, _ = call(t, http.MethodPost, "/r", nil, empClaims,
		func(c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(rid)}}
			UnlockShiftRequest(c)
		})
	if code != http.StatusForbidden {
		t.Fatalf("员工解锁应 403，实际 %d", code)
	}

	// 管理员解锁 → 200，状态变 pending
	code, body = call(t, http.MethodPost, "/r", nil, superClaims,
		func(c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(rid)}}
			UnlockShiftRequest(c)
		})
	if code != 200 {
		t.Fatalf("管理员解锁应 200，实际 %d (%+v)", code, body)
	}
	if body["status"] != models.PrefStatusPending {
		t.Fatalf("解锁后应为 pending，实际 %v", body["status"])
	}

	// 解锁后员工可改
	code, _ = call(t, http.MethodPut, "/r", map[string]interface{}{
		"start_date": "2026-09-20", "end_date": "2026-09-21",
	}, empClaims, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(rid)}}
		UpdateShiftRequest(c)
	})
	if code != 200 {
		t.Fatalf("解锁后员工修改应 200，实际 %d", code)
	}
	t.Log("锁定不可改 / 员工不可解锁 / 管理员解锁后可改 —— 全部符合预期")
}

// TestEmployeeSeesOnlyOwnRequests 员工只能看到自己的需求。
func TestEmployeeSeesOnlyOwnRequests(t *testing.T) {
	setupAPIDB(t)
	var u1, u2 models.User
	db.DB.Where("emp_no = ?", "A001").First(&u1)
	db.DB.Where("emp_no = ?", "A002").First(&u2)

	// 两人各提交一条
	for _, u := range []models.User{u1, u2} {
		cl := &models.Claims{UserID: u.ID, Role: models.RoleExecutor, DeptID: 1}
		code, body := call(t, http.MethodPost, "/r", map[string]interface{}{
			"type": "rest", "repeat": "once", "start_date": "2026-09-10", "end_date": "2026-09-10",
		}, cl, CreateShiftRequest)
		if code != 200 {
			t.Fatalf("提交失败: %+v", body)
		}
	}

	// 员工 u1 只能看到 1 条
	cl := &models.Claims{UserID: u1.ID, Role: models.RoleExecutor, DeptID: 1}
	code, _ := call(t, http.MethodGet, "/r", nil, cl, ListShiftRequests)
	if code != 200 {
		t.Fatalf("列表失败 %d", code)
	}
	var list []models.ShiftRequest
	db.DB.Where("user_id = ?", u1.ID).Find(&list)
	if len(list) != 1 || list[0].UserID != u1.ID {
		t.Fatalf("员工应只看到自己的 1 条需求，实际 %d 条", len(list))
	}
	t.Log("员工可见范围已按本人隔离")
}

// TestGenerateRejectsInfeasible 人手不足时生成接口返回 400 与可操作提示。
func TestGenerateRejectsInfeasible(t *testing.T) {
	setupAPIDB(t)
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 3, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 22})

	code, body := call(t, http.MethodPost, "/r", map[string]interface{}{
		"dept_id": 1, "year": 2026, "month": 9,
	}, superClaims, GenerateSchedule)
	if code != http.StatusBadRequest {
		t.Fatalf("应 400，实际 %d (%+v)", code, body)
	}
	msg, _ := body["error"].(string)
	if msg == "" || !bytes.Contains([]byte(msg), []byte("人手不足")) {
		t.Fatalf("错误信息应含「人手不足」，实际: %s", msg)
	}
	t.Logf("生成接口正确拦截: %s", msg)
}

// TestGenerateAndApplyFlow 端到端：生成 → 应用 → 回读班表。
func TestGenerateAndApplyFlow(t *testing.T) {
	setupAPIDB(t)
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: 1, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 22,
	})
	// 4 人应对 4 个班次
	db.DB.Create(&models.User{Name: "赵六", Username: "A004", EmpNo: "A004", Role: models.RoleExecutor, DeptID: 1})

	code, body := call(t, http.MethodPost, "/r", map[string]interface{}{
		"dept_id": 1, "year": 2026, "month": 9,
	}, superClaims, GenerateSchedule)
	if code != 200 {
		t.Fatalf("生成失败 %d (%+v)", code, body)
	}
	planRaw, _ := body["plan"].(map[string]interface{})
	if len(planRaw) == 0 {
		t.Fatal("生成结果为空")
	}

	// 转成应用接口需要的结构
	plan := map[string]map[string]string{}
	for date, v := range planRaw {
		m := map[string]string{}
		for name, sh := range v.(map[string]interface{}) {
			m[name] = fmt.Sprint(sh)
		}
		plan[date] = m
	}

	code, body = call(t, http.MethodPost, "/r", map[string]interface{}{
		"dept_id": 1, "year": 2026, "month": 9, "plan": plan,
	}, superClaims, ApplyPlan)
	if code != 200 {
		t.Fatalf("应用失败 %d (%+v)", code, body)
	}
	created := int(body["created"].(float64))
	if created == 0 {
		t.Fatal("应用后未创建任何班次记录")
	}

	// 回读数据库核验
	var cnt int64
	db.DB.Model(&models.Schedule{}).Where("dept_id = ? AND date >= ? AND date <= ?", 1, "2026-09-01", "2026-09-30").Count(&cnt)
	if cnt == 0 {
		t.Fatal("数据库中无该月班表记录")
	}

	// 休息不应落库
	var restRows int64
	db.DB.Model(&models.Schedule{}).Where("shift = ?", RestShift).Count(&restRows)
	if restRows != 0 {
		t.Fatalf("休息不应作为班次落库，实际 %d 条", restRows)
	}
	// 内部哨兵不得外泄
	var pendRows int64
	db.DB.Model(&models.Schedule{}).Where("shift = ?", PendingShift).Count(&pendRows)
	if pendRows != 0 {
		t.Fatalf("待定占位不得落库，实际 %d 条", pendRows)
	}
	t.Logf("端到端通过：应用 %d 条班次记录，库中 %d 条，无休息/占位残留", created, cnt)
}

// TestApplyEnforcesLockedRequests 应用时即使前端传回覆盖了锁定休假的计划，
// 也必须强制还原（规则4 的最后一道防线）。
func TestApplyEnforcesLockedRequests(t *testing.T) {
	setupAPIDB(t)
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 0, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 22})
	var u models.User
	db.DB.Where("emp_no = ?", "A001").First(&u)
	db.DB.Create(&models.ShiftRequest{
		UserID: u.ID, UserName: u.Name, DeptID: 1,
		Type: models.PrefTypeRest, Repeat: models.PrefRepeatOnce,
		StartDate: "2026-09-15", EndDate: "2026-09-15",
		Status: models.PrefStatusLocked,
	})

	// 恶意/误操作的计划：把锁定休假日排成早班
	plan := map[string]map[string]string{
		"2026-09-15": {"张三": "早班", "李四": "早班", "王五": "早班"},
	}
	code, body := call(t, http.MethodPost, "/r", map[string]interface{}{
		"dept_id": 1, "year": 2026, "month": 9, "plan": plan,
	}, superClaims, ApplyPlan)
	if code != 200 {
		t.Fatalf("应用失败 %d (%+v)", code, body)
	}

	// 数据库中 9/15 早班的名单不应包含张三
	var rows []models.Schedule
	db.DB.Where("date = ?", "2026-09-15").Find(&rows)
	for _, r := range rows {
		var names []string
		json.Unmarshal([]byte(r.People), &names)
		for _, n := range names {
			if n == "张三" {
				t.Fatalf("张三已锁定休假，不应被排入 %s", r.Shift)
			}
		}
	}
	t.Log("应用阶段已强制还原锁定休假（最后一道防线生效）")
}

// TestSpecialWorkdayCRUD 特殊工作日增删与去重。
func TestSpecialWorkdayCRUD(t *testing.T) {
	setupAPIDB(t)
	payload := map[string]interface{}{
		"dept_id": 1, "date": "2026-09-15", "name": "店庆", "all_staff": true,
	}
	code, body := call(t, http.MethodPost, "/r", payload, superClaims, UpsertSpecialWorkDay)
	if code != 200 {
		t.Fatalf("新增失败 %d (%+v)", code, body)
	}
	sid := uint(body["id"].(float64))

	// 重复日期应拒绝
	code, _ = call(t, http.MethodPost, "/r", payload, superClaims, UpsertSpecialWorkDay)
	if code != http.StatusBadRequest {
		t.Fatalf("重复日期应 400，实际 %d", code)
	}

	// 日期格式错误应拒绝
	code, _ = call(t, http.MethodPost, "/r", map[string]interface{}{
		"dept_id": 1, "date": "2026/09/16", "name": "测试",
	}, superClaims, UpsertSpecialWorkDay)
	if code != http.StatusBadRequest {
		t.Fatalf("非法日期应 400，实际 %d", code)
	}

	// 删除
	code, _ = call(t, http.MethodDelete, "/r", nil, superClaims,
		func(c *gin.Context) {
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(sid)}}
			DeleteSpecialWorkDay(c)
		})
	if code != 200 {
		t.Fatalf("删除应 200，实际 %d", code)
	}
	var cnt int64
	db.DB.Model(&models.SpecialWorkDay{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("删除后应为 0 条，实际 %d", cnt)
	}
	t.Log("特殊工作日增删/去重/格式校验均正确")
}

// TestFixedPrefRejectsUnknownShift 固定班次必须在本部门班次中存在。
func TestFixedPrefRejectsUnknownShift(t *testing.T) {
	setupAPIDB(t)
	var u models.User
	db.DB.Where("emp_no = ?", "A001").First(&u)

	code, body := call(t, http.MethodPut, "/r", map[string]interface{}{
		"user_id": u.ID, "mode": "fixed", "fixed_shift": "不存在的班",
	}, superClaims, UpsertUserShiftPref)
	if code != http.StatusBadRequest {
		t.Fatalf("非法班次应 400，实际 %d (%+v)", code, body)
	}

	// 合法设置
	code, body = call(t, http.MethodPut, "/r", map[string]interface{}{
		"user_id": u.ID, "mode": "fixed", "fixed_shift": "早班",
	}, superClaims, UpsertUserShiftPref)
	if code != 200 {
		t.Fatalf("合法设置应 200，实际 %d (%+v)", code, body)
	}
	if body["mode"] != models.ShiftModeFixed || body["fixed_shift"] != "早班" {
		t.Fatalf("回读不一致: %+v", body)
	}
	t.Log("固定班次校验与保存正确")
}

// TestExecutorCannotGenerate 执行者无权生成排班。
func TestExecutorCannotGenerate(t *testing.T) {
	setupAPIDB(t)
	r := gin.New()
	r.Use(asUser(models.RoleExecutor, 99, 1))
	r.POST("/g", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), GenerateSchedule)

	req := httptest.NewRequest(http.MethodPost, "/g",
		bytes.NewBufferString(`{"dept_id":1,"year":2026,"month":9}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("执行者生成排班应 403，实际 %d (%s)", w.Code, w.Body.String())
	}
	t.Log("执行者越权生成已被拦截")
}
