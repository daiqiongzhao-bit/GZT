package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
)

// setupV020 sets up an in-memory DB with required models and returns a gin engine
// whose /test route injects the given claims then calls the handler.
func setupV020(t *testing.T, handler gin.HandlerFunc, cl *models.Claims) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.User{}, &models.Department{}, &models.Notification{}, &models.Log{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d

	r := gin.New()
	// 注入 claims 的中间件，等效于 AuthRequired 之后的 currentClaims
	claimsSet := func(c *gin.Context) {
		c.Set(middleware.CtxUserKey, cl)
		c.Next()
	}
	r.POST("/test", claimsSet, handler)
	return r, func() {}
}

// doBroadcast sends a JSON POST to the /test route.
func doBroadcast(t *testing.T, r *gin.Engine, deptID uint, title, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"dept_id": deptID, "title": title, "content": content})
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedBroadcastUsers(t *testing.T) {
	t.Helper()
	// 部门 1：两名可用 + 一名冻结；部门 2：一名可用
	db.DB.Create(&models.Department{ID: 1, Name: "预订仓"})
	db.DB.Create(&models.Department{ID: 2, Name: "信息部"})
	db.DB.Create(&models.User{Username: "a1", Name: "张三", DeptID: 1, Frozen: false})
	db.DB.Create(&models.User{Username: "a2", Name: "李四", DeptID: 1, Frozen: false})
	db.DB.Create(&models.User{Username: "a3", Name: "冻结王", DeptID: 1, Frozen: true})
	db.DB.Create(&models.User{Username: "b1", Name: "陈五", DeptID: 2, Frozen: false})
}

// TestBroadcastDeptAdminOwnDept 部门管理员发广播：只给本部门非冻结成员，含本人，不带入冻结与它部门（v0.2.0）
func TestBroadcastDeptAdminOwnDept(t *testing.T) {
	r, _ := setupV020(t, BroadcastNotification, &models.Claims{
		UserID: 1, Username: "a1", Role: models.RoleDeptAdmin, DeptID: 1, Client: models.ClientWeb,
	})
	seedBroadcastUsers(t)
	w := doBroadcast(t, r, 0, "下午到点巡查", "请各岗位按时完成")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", w.Code, w.Body.String())
	}
	// 部门 1 可用 2 人（张三含本人 actor + 李四），冻结王、陈五不在
	var n []models.Notification
	db.DB.Find(&n)
	names := map[string]bool{}
	for _, x := range n {
		var u models.User
		db.DB.First(&u, x.UserID)
		names[u.Name] = true
	}
	if len(n) != 2 {
		t.Errorf("应创建 2 条通知（张三、李四），实际 %d", len(n))
	}
	if !names["张三"] || !names["李四"] {
		t.Errorf("通知接收人应为 张三、李四，实际 %v", names)
	}
	if names["冻结王"] || names["陈五"] {
		t.Errorf("冻结/它部门人员不应收到广播，实际 %v", names)
	}
}

// TestBroadcastDeptAdminForbidden 部门管理员无权给其他部门广播 -> 403
func TestBroadcastDeptAdminForbidden(t *testing.T) {
	r, _ := setupV020(t, BroadcastNotification, &models.Claims{
		UserID: 1, Username: "a1", Role: models.RoleDeptAdmin, DeptID: 1, Client: models.ClientWeb,
	})
	seedBroadcastUsers(t)
	w := doBroadcast(t, r, 2, "越权", "越权内容")
	if w.Code != http.StatusForbidden {
		t.Fatalf("期望 403，实际 %d", w.Code)
	}
}

// TestBroadcastSuperAdminRequiresDept 超管未选部门 -> 400；选定后按部门广播
func TestBroadcastSuperAdminRequiresDept(t *testing.T) {
	r, _ := setupV020(t, BroadcastNotification, &models.Claims{
		UserID: 9, Username: "admin", Role: models.RoleSuperAdmin, DeptID: 0, Client: models.ClientWeb,
	})
	seedBroadcastUsers(t)
	// 未选部门
	w0 := doBroadcast(t, r, 0, "全员", "内容")
	if w0.Code != http.StatusBadRequest {
		t.Fatalf("超管未选部门应 400，实际 %d", w0.Code)
	}
	// 选定部门 2
	w1 := doBroadcast(t, r, 2, "信息部通知", "本周五巡检")
	if w1.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", w1.Code, w1.Body.String())
	}
	var n []models.Notification
	db.DB.Find(&n)
	if len(n) != 1 {
		t.Errorf("部门 2 只有 1 名可用成员，应创建 1 条，实际 %d", len(n))
	}
}

// TestBroadcastMissingFields 标题/内容必填 -> 400
func TestBroadcastMissingFields(t *testing.T) {
	r, _ := setupV020(t, BroadcastNotification, &models.Claims{
		UserID: 1, Username: "a1", Role: models.RoleDeptAdmin, DeptID: 1, Client: models.ClientWeb,
	})
	seedBroadcastUsers(t)
	w := doBroadcast(t, r, 0, "", "只有内容")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺标题应 400，实际 %d", w.Code)
	}
}
