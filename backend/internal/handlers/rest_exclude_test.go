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

// TestTodayOnDutyExcludesRest 通知推送的「今日当班」必须排除休息班次：
// 休息的人不列进当班列表、不会被推送 @（与 Dashboard 口径一致，v0.14.2 修复）
func TestTodayOnDutyExcludesRest(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.Schedule{}); err != nil {
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
	db.DB.Create(&models.Schedule{Date: today, Shift: "早班", People: peo("张三", "李四")})
	db.DB.Create(&models.Schedule{Date: today, Shift: "中班", People: peo("王五")})
	db.DB.Create(&models.Schedule{Date: today, Shift: "休息", People: peo("赵六", "钱七")})

	onDuty := todayOnDuty(nil)
	if _, ok := onDuty["休息"]; ok {
		t.Fatalf("休息班次不应出现在当班映射里，实际 onDuty=%v", onDuty)
	}
	all := taskShiftPeople(models.Task{Shift: "全员"}, onDuty)
	for _, n := range []string{"赵六", "钱七"} {
		for _, got := range all {
			if got == n {
				t.Errorf("休息人员 %s 不应出现在全员任务的当班/@名单里，实际 %v", n, all)
			}
		}
	}
	if len(all) != 3 {
		t.Errorf("当班应为 3 人（早2+中1），实际 %d 人 %v", len(all), all)
	}
}
