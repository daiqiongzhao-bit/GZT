package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// TestScopeSemantics 人员范围口径（v0.8.0 与需求对齐）：
//   - 当班 = 今日有排班且非休息的人员（覆盖 早/中/晚/夜 等全部班次），休息不计入当班；
//   - 全员 = 系统内所有人员（含正在休息），与今日是否排班无关。
func TestScopeSemantics(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.Schedule{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d

	today := time.Now().Format("2006-01-02")
	peo := func(names ...string) string {
		b := "["
		for i, n := range names {
			if i > 0 {
				b += ","
			}
			b += `"` + n + `"`
		}
		return b + "]"
	}
	// 人员表：张三/李四/王五/赵六/钱七 均为系统人员
	for _, n := range []string{"张三", "李四", "王五", "赵六", "钱七"} {
		db.DB.Create(&models.User{Name: n, Username: n})
	}
	// 班表：早班2人、中班1人、休息2人
	db.DB.Create(&models.Schedule{Date: today, Shift: "早班", People: peo("张三", "李四")})
	db.DB.Create(&models.Schedule{Date: today, Shift: "中班", People: peo("王五")})
	db.DB.Create(&models.Schedule{Date: today, Shift: "休息", People: peo("赵六", "钱七")})

	// —— 当班：休息不计入（今日班表只统计真正上班的班次）——
	onDuty := todayOnDuty(nil)
	if _, ok := onDuty["休息"]; ok {
		t.Fatalf("休息班次不应出现在当班映射里，实际 onDuty=%v", onDuty)
	}
	// 中班任务 → 只@中班当班（王五）
	mid := taskShiftPeople(models.Task{Shift: "中班"}, onDuty, nil)
	if len(mid) != 1 || mid[0] != "王五" {
		t.Errorf("中班任务当班应为 [王五]，实际 %v", mid)
	}

	// —— 全员：所有人员（含正在休息），不看今日班表 ——
	all := taskShiftPeople(models.Task{Shift: "全员"}, onDuty, nil)
	allMap := map[string]bool{}
	for _, n := range all {
		allMap[n] = true
	}
	for _, n := range []string{"张三", "李四", "王五", "赵六", "钱七"} {
		if !allMap[n] {
			t.Errorf("全员任务应包含 %s（含休息人员），实际 %v", n, all)
		}
	}
	if len(all) != 5 {
		t.Errorf("全员应为 5 人（含休息的赵六/钱七），实际 %d 人 %v", len(all), all)
	}
}
