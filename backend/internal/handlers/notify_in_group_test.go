package handlers

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// TestInGroupFiltersMobiles @手机号只收集「已加入通知群」的用户：
// 未入群的人无法被企业微信@到，手机号不应进入 mentioned_mobile_list（v0.15.2）
func TestInGroupFiltersMobiles(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d

	db.DB.Create(&models.User{Username: "dai01", Name: "艾特戴", Mobile: "13800000011", InGroup: true})
	db.DB.Create(&models.User{Username: "liu01", Name: "名单刘", Mobile: "13800000012", InGroup: false})

	m := peopleMobiles([]string{"艾特戴", "名单刘"})
	if len(m) != 1 || m[0] != "13800000011" {
		t.Fatalf("peopleMobiles 应只含已入群的手机号，实际 %v", m)
	}

	set := inGroupSet()
	if !set["艾特戴"] || set["名单刘"] {
		t.Fatalf("inGroupSet 结果错误: %v", set)
	}
}

// TestBuildReminderHidesGroupMembers 汇总名单不重复列出已入群人员：
// 已入群者由 @ 提醒，名单只显示未入群者（v0.15.2）
func TestBuildReminderHidesGroupMembers(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.User{}, &models.Schedule{}, &models.Task{}); err != nil {
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
	db.DB.Create(&models.User{Username: "dai01", Name: "艾特戴", Mobile: "13800000011", InGroup: true})
	db.DB.Create(&models.User{Username: "liu01", Name: "名单刘", Mobile: "13800000012", InGroup: false})
	db.DB.Create(&models.Schedule{Date: today, Shift: "早班", People: peo("艾特戴", "名单刘")})
	db.DB.Create(&models.Task{
		Title:  "发数据",
		Shift:  "早班",
		Type:   models.TaskTypeDaily,
		Time:   "09:00",
		Status: models.TaskStatusTodo,
	})

	content := buildReminderContent(nil)
	if content == "" {
		t.Fatal("应有今日任务汇总内容")
	}
	if !strings.Contains(content, "名单刘") {
		t.Errorf("名单应包含未入群的「名单刘」，实际内容:\n%s", content)
	}
	if strings.Contains(content, "艾特戴") {
		t.Errorf("名单不应包含已入群的「艾特戴」（会收到@提醒），实际内容:\n%s", content)
	}
}
