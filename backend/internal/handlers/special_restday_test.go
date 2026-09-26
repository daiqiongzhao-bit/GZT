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

// setupSpecialRestDB 建库并灌入基准数据，额外迁移 SpecialRestDay 表。
func setupSpecialRestDB(t *testing.T, staff ...struct {
	Name  string
	EmpNo string
}) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateAll(d); err != nil {
		t.Fatal(err)
	}
	// 复刻生产：给超管(user 1)种全量数据范围角色，使 Scope.All 成立，避免 RBAC 闸门后 403。
	seedSuperAdmin(t, d)
	db.DB = d

	db.DB.Create(&models.Department{ID: 1, Name: "测试门店"})
	for _, s := range []models.ShiftConfig{
		{DeptID: 1, Name: "早班", StartTime: "08:00", EndTime: "16:00"},
		{DeptID: 1, Name: "中班", StartTime: "12:00", EndTime: "20:00"},
		{DeptID: 1, Name: "晚班", StartTime: "16:00", EndTime: "22:00"},
		{DeptID: 1, Name: "夜班", StartTime: "22:00", EndTime: "06:00"},
	} {
		db.DB.Create(&s)
	}
	for _, s := range staff {
		db.DB.Create(&models.User{
			Name: s.Name, Username: s.EmpNo, EmpNo: s.EmpNo,
			Role: models.RoleExecutor, DeptID: 1,
		})
	}
}

// TestGenerateSpecialRestDay 端到端：特殊休息日当天全员休息，且生成提示、无 special_rest 违规。
func TestGenerateSpecialRestDay(t *testing.T) {
	setupSpecialRestDB(t,
		staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"}, staff{"赵六", "A004"},
		staff{"行政", "A005"},
	)
	var adm models.User
	db.DB.Where("emp_no = ?", "A005").First(&adm)
	db.DB.Create(&models.UserShiftPref{
		UserID: adm.ID, DeptID: 1, Mode: models.ShiftModeFixed, FixedShift: "早班",
	})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 0, MonthWorkDays: 22, MaxRestStreak: 3, MaxWorkStreak: 6})
	restDay := "2026-10-01"
	db.DB.Create(&models.SpecialRestDay{Date: restDay, DeptID: 1, Name: "国庆", AllStaff: true})

	code, out := call(t, "POST", "/schedules/generate",
		map[string]interface{}{"dept_id": 1, "year": 2026, "month": 10},
		superClaims, GenerateSchedule)
	if code != 200 {
		t.Fatalf("生成应成功，实际 %d: %v", code, out)
	}
	plan, ok := out["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 plan: %v", out)
	}
	dayPlan, ok := plan[restDay].(map[string]interface{})
	if !ok {
		t.Fatalf("计划缺少 %s: %v", restDay, plan)
	}
	for name, v := range dayPlan {
		sh, _ := v.(string)
		if sh != RestShift {
			t.Fatalf("%s 在特殊休息日 %s 应为「休息」，实际 %q", name, restDay, sh)
		}
	}

	// 生成应给出提示
	noted := false
	for _, n := range out["notes"].([]interface{}) {
		if s, ok := n.(string); ok && strings.Contains(s, "特殊休息日") {
			noted = true
			break
		}
	}
	if !noted {
		t.Fatalf("生成提示应包含特殊休息日说明，实际 notes=%v", out["notes"])
	}

	// 自动生成不应产生 special_rest 违规
	for _, vv := range out["violations"].([]interface{}) {
		m, _ := vv.(map[string]interface{})
		if m["rule"] == "special_rest" {
			t.Fatalf("自动生成不应出现 special_rest 违规，实际 %v", vv)
		}
	}
}

// TestValidateSpecialRestViolation 校验器：特殊休息日仍排班应报错；休息则无事。
func TestValidateSpecialRestViolation(t *testing.T) {
	restDay := "2026-10-01"
	pi := &PlanInfo{
		DeptID: 1, Year: 2026, Month: 10,
		First: time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local),
		Last:  time.Date(2026, 10, 31, 0, 0, 0, 0, time.Local),
		Rule:  models.ShiftRule{MaxRestStreak: 3, MaxWorkStreak: 6},
		People: []PlanPerson{
			{Name: "张三", UserID: 1, Mode: models.ShiftModeRotate},
			{Name: "李四", UserID: 2, Mode: models.ShiftModeRotate},
		},
		Rest: map[string]string{restDay: "国庆"},
	}

	// 张三被排为上班 → 违规
	planWork := map[string]map[string]string{restDay: {"张三": "早班", "李四": RestShift}}
	vs := pi.validatePlan(planWork)
	found := false
	for _, v := range vs {
		if v.Rule == "special_rest" && v.Person == "张三" {
			found = true
		}
	}
	if !found {
		t.Fatalf("特殊休息日仍排班应报 special_rest 违规，实际 %v", vs)
	}

	// 全员休息 → 无 special_rest 违规
	planRest := map[string]map[string]string{restDay: {"张三": RestShift, "李四": RestShift}}
	vs2 := pi.validatePlan(planRest)
	for _, v := range vs2 {
		if v.Rule == "special_rest" {
			t.Fatalf("全员休息不应报 special_rest 违规，实际 %v", v)
		}
	}
}

// TestEnforceSpecialRest 最终强制阶段：把强制休息名单还原为休息。
func TestEnforceSpecialRest(t *testing.T) {
	restDay := "2026-10-01"
	plan := map[string]map[string]string{restDay: {"张三": "早班", "李四": "中班"}}
	forcedRest := map[string][]string{restDay: {"张三", "李四"}}
	notes := (&PlanInfo{}).enforceSpecialRest(plan, forcedRest)
	if len(notes) != 2 {
		t.Fatalf("应还原 2 人，实际 notes=%v", notes)
	}
	if plan[restDay]["张三"] != RestShift || plan[restDay]["李四"] != RestShift {
		t.Fatalf("强制还原后应为休息，实际 %v", plan[restDay])
	}
}
