package handlers

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// setupUserRuleDB 用户规则校验测试库
func setupUserRuleDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	db.DB.Create(&models.User{Name: "张三", Username: "A001", EmpNo: "A001", DeptID: 8, Mobile: "13800000001"})
	db.DB.Create(&models.User{Name: "李四", Username: "A002", EmpNo: "A002", DeptID: 9, Mobile: "13800000002"})
}

// TestValidateUserProfile 同部门禁重名 + 手机号 11 位校验
func TestValidateUserProfile(t *testing.T) {
	setupUserRuleDB(t)

	cases := []struct {
		label        string
		name         string
		deptID       uint
		mobile       string
		exclude      uint
		wantMsgEmpty bool
	}{
		{"同部门重名拒绝", "张三", 8, "13800000000", 0, false},
		{"不同部门同名允许", "张三", 9, "13800000000", 0, true},
		{"编辑时排除本人允许", "张三", 8, "13800000000", 1, true},
		{"手机号 10 位拒绝", "王五", 8, "1380000000", 0, false},
		{"手机号 12 位拒绝", "王五", 8, "138000000001", 0, false},
		{"手机号非 1 开头拒绝", "王五", 8, "23800000001", 0, false},
		{"手机号带空格归一化后通过", "王五", 8, "138 0000 0000", 0, true},
		{"手机号 +86 归一化后通过", "王五", 8, "+86 13800000000", 0, true},
		{"手机号为空允许", "王五", 8, "", 0, true},
		{"合法手机号通过", "王五", 8, "13800000011", 0, true},
	}
	for _, c := range cases {
		msg := validateUserProfile(c.name, c.deptID, normalizeMobile(c.mobile), c.exclude)
		gotEmpty := msg == ""
		if gotEmpty != c.wantMsgEmpty {
			t.Errorf("[%s] validateUserProfile(%q,%d,%q) = %q，期望%s", c.label, c.name, c.deptID, c.mobile, msg,
				map[bool]string{true: "通过", false: "拒绝"}[c.wantMsgEmpty])
		}
	}
}
