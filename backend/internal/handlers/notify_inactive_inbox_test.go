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

// setupInactiveDB 建一个只含通知相关表的内存库，供「冻结/休假不打扰」用例共用。
// 同时把 VAPID 密钥置为非空，使 EnsurePushKeys 直接短路——测试不落盘、不联网。
func setupInactiveDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(
		&models.User{}, &models.Notification{}, &models.Log{}, &models.PushSubscription{},
	); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	vapidPub, vapidPriv = "test-vapid-pub", "test-vapid-priv"
}

// seedInactiveUsers 建 管理员 + 阿正(正常) / 阿冻(冻结) / 阿休(休假)。
func seedInactiveUsers(t *testing.T) (admin, ok, frozen, leave models.User) {
	t.Helper()
	admin = models.User{Name: "管理员", Username: "admin", Role: models.RoleSuperAdmin}
	db.DB.Create(&admin)
	ok = models.User{Name: "阿正", Username: "u_ok"}
	db.DB.Create(&ok)
	frozen = models.User{Name: "阿冻", Username: "u_frozen", Frozen: true}
	db.DB.Create(&frozen)
	leave = models.User{Name: "阿休", Username: "u_leave", OnLeave: true}
	db.DB.Create(&leave)
	return
}

// TestNotifyPeopleByNameExcludesInactive 班表更新等批量站内通知不得投递给已冻结 / 休假账号。
// 回归：v0.29.2 之前 notifyPeopleByName 只按姓名查人、不过滤 frozen/on_leave，
// 导致休假成员（能登录）在班表被改动时仍收到站内通知 + Web Push。
func TestNotifyPeopleByNameExcludesInactive(t *testing.T) {
	setupInactiveDB(t)
	admin, _, _, _ := seedInactiveUsers(t)

	notifyPeopleByName([]string{"阿正", "阿冻", "阿休"}, "schedule", "班表更新", "您的班表有更新", admin.ID, "管理员")

	var ns []models.Notification
	db.DB.Find(&ns)
	if len(ns) != 1 {
		t.Fatalf("应只给「阿正」创建 1 条站内通知，实际 %d 条", len(ns))
	}
	var got models.User
	db.DB.First(&got, ns[0].UserID)
	if got.Name != "阿正" {
		t.Errorf("通知接收人应为「阿正」，实际「%s」", got.Name)
	}
}

// TestNudgeExcludesInactive 催办广播不得再催已冻结 / 休假的账号——
// 包括修复前就已落库的历史通知行（催办是遍历既有行，不重新查部门成员）。
func TestNudgeExcludesInactive(t *testing.T) {
	setupInactiveDB(t)
	admin, ok, frozen, leave := seedInactiveUsers(t)

	// 三人都在修复前收到了同一条广播，且均未读
	for _, uid := range []uint{ok.ID, frozen.ID, leave.ID} {
		db.DB.Create(&models.Notification{
			UserID: uid, Kind: "broadcast", BroadcastID: "B1",
			Title: "国庆排班调整", Content: "请查看", ActorID: admin.ID, Read: false,
		})
	}

	r := gin.New()
	cl := &models.Claims{UserID: admin.ID, Username: "admin", Role: models.RoleSuperAdmin}
	r.POST("/nudge/:bid", func(c *gin.Context) {
		c.Set(middleware.CtxUserKey, cl)
		c.Next()
	}, NudgeBroadcast)

	req := httptest.NewRequest(http.MethodPost, "/nudge/B1", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", w.Code, w.Body.String())
	}
	var nudges []models.Notification
	db.DB.Where("kind = ?", "broadcast_nudge").Find(&nudges)
	if len(nudges) != 1 {
		t.Fatalf("催办应只发给 1 人（阿正），实际 %d 条", len(nudges))
	}
	if nudges[0].UserID != ok.ID {
		t.Errorf("催办对象应为「阿正」(id=%d)，实际 user_id=%d", ok.ID, nudges[0].UserID)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if n, _ := body["sent"].(float64); int(n) != 1 {
		t.Errorf("响应 sent 应为 1，实际 %v", body["sent"])
	}
}
