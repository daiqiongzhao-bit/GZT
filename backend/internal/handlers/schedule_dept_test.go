package handlers

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

func openTestDB(t *testing.T, dst ...interface{}) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(dst...); err != nil {
		t.Fatal(err)
	}
	db.DB = d
}

// TestValidatePeopleDept v0.0.5：排班人员必须归属该部门（或其上级部门），
// 防止把 A 部门的人排进 B 部门班表（历史 bug：刘海龙属三亚预订仓却被排进信息部）
func TestValidatePeopleDept(t *testing.T) {
	openTestDB(t, &models.User{}, &models.Department{})

	db.DB.Create(&models.Department{ID: 6, Name: "信息部"})
	db.DB.Create(&models.Department{ID: 7, Name: "物流部"})
	db.DB.Create(&models.Department{ID: 8, Name: "三亚预订仓", ParentID: 7})
	db.DB.Create(&models.User{Username: "2748", Name: "刘海龙", DeptID: 8})
	db.DB.Create(&models.User{Username: "9001", Name: "信息部小李", DeptID: 6})

	// 跨部门排班必须被拒绝
	if err := validatePeopleDept(6, []string{"刘海龙"}); err == nil {
		t.Fatal("应拒绝把预订仓的刘海龙排进信息部")
	}
	// 本部门正常放行
	if err := validatePeopleDept(8, []string{"刘海龙"}); err != nil {
		t.Fatalf("本部门排班不应被拒绝: %v", err)
	}
	if err := validatePeopleDept(6, []string{"信息部小李"}); err != nil {
		t.Fatalf("本部门排班不应被拒绝: %v", err)
	}
	// 无对应账号的历史姓名不做强校验，避免阻断既有数据
	if err := validatePeopleDept(8, []string{"历史遗留姓名"}); err != nil {
		t.Fatalf("无对应账号的历史姓名不应拦截: %v", err)
	}
}

// TestReplacePersonShiftsCrossDept v0.0.5：同人同天只保留一个班次（跨部门也清理）。
// 原实现带 dept_id 条件，跨部门就失效，导致超管视角下出现「一人两个班次」。
func TestReplacePersonShiftsCrossDept(t *testing.T) {
	openTestDB(t, &models.Schedule{}, &models.Department{})

	db.DB.Create(&models.Schedule{Date: "2026-09-05", Shift: "休息", People: `["刘海龙"]`, DeptID: 8})

	replaced := replacePersonShifts("2026-09-05", 6, []string{"刘海龙"}, 0)
	if len(replaced) != 1 || replaced[0] != "刘海龙" {
		t.Fatalf("跨部门旧班次也应被替换，实际 %v", replaced)
	}

	var n int64
	db.DB.Model(&models.Schedule{}).Where("date = ?", "2026-09-05").Count(&n)
	if n != 0 {
		t.Fatalf("清理后 2026-09-05 应无残留记录，实际 %d 条", n)
	}
}

// TestReplacePersonShiftsKeepsOthers 替换时不应误伤同日期的其他人
func TestReplacePersonShiftsKeepsOthers(t *testing.T) {
	openTestDB(t, &models.Schedule{}, &models.Department{})

	db.DB.Create(&models.Schedule{Date: "2026-09-06", Shift: "休息", People: `["刘海龙","陈毓山"]`, DeptID: 8})

	replacePersonShifts("2026-09-06", 8, []string{"刘海龙"}, 0)

	var s models.Schedule
	if err := db.DB.Where("date = ?", "2026-09-06").First(&s).Error; err != nil {
		t.Fatalf("多人记录不应被整条删除: %v", err)
	}
	if s.People != `["陈毓山"]` {
		t.Fatalf("应只移除刘海龙、保留陈毓山，实际 people=%s", s.People)
	}
}
