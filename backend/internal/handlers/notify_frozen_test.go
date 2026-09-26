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

// TestFrozenExcludedFromOnDuty 冻结 / 休假账号即使仍在今日班表里，也不得被 @。
// 回归：v0.29.0 之前 todayOnDuty 只看班表、不校验 frozen，导致已冻结账号（如戴琼照）
// 仍出现在「当班」名单并被企业微信 @ 到。
func TestFrozenExcludedFromOnDuty(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateAll(d); err != nil {
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
	// 阿正常在岗；阿冻已冻结；阿休休假——三人都被排进今早的早班
	db.DB.Create(&models.User{Name: "阿正", Username: "u_ok", InGroup: true, Mobile: "13800000001"})
	db.DB.Create(&models.User{Name: "阿冻", Username: "u_frozen", Frozen: true, InGroup: true, Mobile: "13800000002"})
	db.DB.Create(&models.User{Name: "阿休", Username: "u_leave", OnLeave: true, InGroup: true, Mobile: "13800000003"})
	db.DB.Create(&models.Schedule{Date: today, Shift: "早班", People: peo("阿正", "阿冻", "阿休")})

	// —— 当班映射：冻结/休假账号不得计入 ——
	onDuty := todayOnDuty(nil)
	got := onDuty["早班"]
	if len(got) != 1 || got[0] != "阿正" {
		t.Fatalf("早班当班应只剩 [阿正]（排除冻结/休假），实际 %v", got)
	}

	// —— 任务 @ 名单：具体班次不得含冻结/休假 ——
	people := taskShiftPeople(models.Task{Shift: "早班"}, onDuty, nil)
	pm := map[string]bool{}
	for _, p := range people {
		pm[p] = true
	}
	if pm["阿冻"] || pm["阿休"] {
		t.Errorf("早班任务 @ 名单不应含冻结/休假账号，实际 %v", people)
	}

	// —— 手机号兜底：任何人（含负责人名单）都不该 @ 到冻结/休假账号 ——
	mobiles := peopleMobiles([]string{"阿正", "阿冻", "阿休"})
	if len(mobiles) != 1 || mobiles[0] != "13800000001" {
		t.Errorf("peopleMobiles 应只剩阿正的手机号，实际 %v", mobiles)
	}

	// —— 全员任务同样排除冻结/休假 ——
	all := taskShiftPeople(models.Task{Shift: "全员"}, onDuty, nil)
	for _, n := range all {
		if n == "阿冻" || n == "阿休" {
			t.Errorf("全员任务不应含冻结/休假账号 %s，实际 %v", n, all)
		}
	}
}
