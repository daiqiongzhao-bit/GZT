package handlers

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// setupHookDB 用独立内存库初始化 db.DB（每个测试一个库，避免共享冲突）
func setupHookDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.Department{}, &models.Webhook{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d
}

// TestHookBelongsToDept v0.12.9：祖先部门的 Webhook 应能收到子部门任务（物流部群看到预订仓任务）
func TestHookBelongsToDept(t *testing.T) {
	setupHookDB(t)
	db.DB.Create(&models.Department{ID: 7, Name: "物流部", ParentID: 0})
	db.DB.Create(&models.Department{ID: 8, Name: "三亚预订仓", ParentID: 7})

	h7 := models.Webhook{Name: "物流部", DeptID: 7, Type: "wecom", URL: "x"}
	h8 := models.Webhook{Name: "预订仓", DeptID: 8, Type: "wecom", URL: "x"}
	h0 := models.Webhook{Name: "全局", DeptID: 0, Type: "wecom", URL: "x"}

	if !hookBelongsToDept(h7, 8) {
		t.Error("物流部(7) 的 webhook 应能收到子部门 三亚预订仓(8) 的任务")
	}
	if !hookBelongsToDept(h7, 7) {
		t.Error("同部门(7→7) 应匹配")
	}
	if !hookBelongsToDept(h8, 8) {
		t.Error("同部门(8→8) 应匹配")
	}
	if hookBelongsToDept(h8, 7) {
		t.Error("子部门 webhook 不应收到父部门任务")
	}
	if !hookBelongsToDept(h0, 8) {
		t.Error("全局 webhook(dept=0) 应匹配任意部门")
	}
	if !hookBelongsToDept(h0, 999) {
		t.Error("全局 webhook 应匹配任意部门（含不存在部门）")
	}
}

// TestWebhooksForScope 部门管理员（scope=[8]）应看到 物流部(7) 与 三亚预订仓(8) 的 webhook
func TestWebhooksForScope(t *testing.T) {
	setupHookDB(t)
	db.DB.Create(&models.Department{ID: 7, Name: "物流部", ParentID: 0})
	db.DB.Create(&models.Department{ID: 8, Name: "三亚预订仓", ParentID: 7})
	db.DB.Create(&models.Webhook{Name: "物流部", DeptID: 7, Type: "wecom", URL: "a"})
	db.DB.Create(&models.Webhook{Name: "预订仓", DeptID: 8, Type: "wecom", URL: "b"})
	db.DB.Create(&models.Webhook{Name: "全局", DeptID: 0, Type: "wecom", URL: "c"})

	got := webhooksForScope([]uint{8})
	names := map[string]bool{}
	for _, h := range got {
		names[h.Name] = true
	}
	if !names["物流部"] || !names["预订仓"] || !names["全局"] {
		t.Errorf("scope=[8] 应包含祖先(物流部)、本部门(预订仓)、全局，实际 %v", names)
	}

	all := webhooksForScope(nil)
	if len(all) != 3 {
		t.Errorf("超管 scope=nil 应返回全部 webhook，实际 %d", len(all))
	}
}
