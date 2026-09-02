package handlers

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// TestUnknownScheduleNames 班表导入的无账号人员检测：按姓名匹配 users
func TestUnknownScheduleNames(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	d.Create(&models.User{Name: "张三", Username: "A001", EmpNo: "A001"})
	d.Create(&models.User{Name: "李四", Username: "A002", EmpNo: "A002"})

	got := unknownScheduleNames(map[string]bool{"张三": true, "王五": true, "李四": true, "赵六": true})
	want := []string{"王五", "赵六"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknownScheduleNames = %v，期望 %v", got, want)
	}
	if unknownScheduleNames(nil) != nil {
		t.Error("空输入应返回 nil")
	}
	if unknownScheduleNames(map[string]bool{"张三": true}) != nil {
		t.Error("全部有账号时不应返回警告")
	}
}
