package handlers

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// setupShiftRuleDB 一人一天一班规则测试库
func setupShiftRuleDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.Schedule{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d
}

// TestReplacePersonShifts 一人一天只在一个班次：写入新班次时替换当天旧班次
func TestReplacePersonShifts(t *testing.T) {
	setupShiftRuleDB(t)

	// 预置：张三 9/1 早班 + 中班（异常多班次），李四 9/1 早班，王五 9/1 与赵六同条中班记录
	db.DB.Create(&models.Schedule{ID: 1, Date: "2026-09-01", Shift: "早班", People: `["张三"]`, DeptID: 8})
	db.DB.Create(&models.Schedule{ID: 2, Date: "2026-09-01", Shift: "中班", People: `["张三"]`, DeptID: 8})
	db.DB.Create(&models.Schedule{ID: 3, Date: "2026-09-01", Shift: "早班", People: `["李四"]`, DeptID: 8})
	db.DB.Create(&models.Schedule{ID: 4, Date: "2026-09-01", Shift: "中班", People: `["王五","赵六"]`, DeptID: 8})
	db.DB.Create(&models.Schedule{ID: 5, Date: "2026-09-02", Shift: "早班", People: `["张三"]`, DeptID: 8})

	// 给张三排 9/1 晚班 → 应清掉早班+中班两条旧记录（新记录由调用方创建）
	replaced := replacePersonShifts("2026-09-01", 8, []string{"张三"}, 0)
	db.DB.Create(&models.Schedule{Date: "2026-09-01", Shift: "晚班", People: `["张三"]`, DeptID: 8})
	if len(replaced) != 1 || replaced[0] != "张三" {
		t.Fatalf("应替换张三，实际 %v", replaced)
	}
	var cnt int64
	db.DB.Model(&models.Schedule{}).Where("date = ? AND people LIKE ?", "2026-09-01", `%张三%`).Count(&cnt)
	if cnt != 1 {
		t.Fatalf("张三 9/1 应只剩 1 条记录（晚班），实际 %d 条", cnt)
	}
	var zhang models.Schedule
	db.DB.Where("date = ? AND people LIKE ?", "2026-09-01", `%张三%`).First(&zhang)
	if zhang.Shift != "晚班" {
		t.Errorf("张三应在新班次晚班，实际 %s", zhang.Shift)
	}

	// 给王五排 9/1 晚班 → 多人记录里移除王五，赵六保留
	replaced2 := replacePersonShifts("2026-09-01", 8, []string{"王五"}, 0)
	db.DB.Create(&models.Schedule{Date: "2026-09-01", Shift: "晚班", People: `["王五"]`, DeptID: 8})
	if len(replaced2) != 1 || replaced2[0] != "王五" {
		t.Fatalf("应替换王五，实际 %v", replaced2)
	}
	var zhao models.Schedule
	db.DB.Where("id = ?", 4).First(&zhao)
	if zhao.People != `["赵六"]` {
		t.Errorf("原中班多人记录应只剩赵六，实际 %s", zhao.People)
	}

	// 张三 9/2 的早班不受影响（不同日期）
	var cnt2 int64
	db.DB.Model(&models.Schedule{}).Where("date = ?", "2026-09-02").Count(&cnt2)
	if cnt2 != 1 {
		t.Errorf("9/2 记录不应被误删，实际 %d 条", cnt2)
	}
}

// TestReplacePersonShiftsExcludeSelf 编辑时排除自身：改自己班次不把自己删掉
func TestReplacePersonShiftsExcludeSelf(t *testing.T) {
	setupShiftRuleDB(t)
	db.DB.Create(&models.Schedule{ID: 10, Date: "2026-09-01", Shift: "早班", People: `["张三"]`, DeptID: 8})

	// 编辑 id=10（张三从早班改中班）：exclude 自身，replace 应返回空
	replaced := replacePersonShifts("2026-09-01", 8, []string{"张三"}, 10)
	if len(replaced) != 0 {
		t.Errorf("编辑自身不应触发替换，实际 %v", replaced)
	}
	var cnt int64
	db.DB.Model(&models.Schedule{}).Where("date = ?", "2026-09-01").Count(&cnt)
	if cnt != 1 {
		t.Errorf("编辑后应仍只有 1 条记录，实际 %d", cnt)
	}
}
