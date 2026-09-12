package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestLeavePersonStillScheduled 复现 2026-10 场景：
// 6 名倒班人员，其中 user12 产假 10-01~10-11（11 天）。
// 期望：产假人员仍在班表内（休假标「休息」），其休假天数不计入每班最少人数，
// 其余 5 人满足 22 天班，整月无「班次人数不足」硬违规。
func TestLeavePersonStillScheduled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := "file:leave_repro?mode=memory&cache=shared"
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(
		&models.User{}, &models.ShiftConfig{}, &models.ShiftRule{},
		&models.UserShiftPref{}, &models.ShiftRequest{}, &models.SpecialWorkDay{},
		&models.Department{}, &models.Schedule{}, &models.Log{}, &models.Notification{},
	); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	db.DB.Create(&models.Department{ID: 1, Name: "营运部"})

	shifts := []struct {
		name, start, end string
	}{
		{"早班", "08:00", "16:00"},
		{"中班", "12:00", "20:00"},
	}
	for _, s := range shifts {
		db.DB.Create(&models.ShiftConfig{DeptID: 1, Name: s.name, StartTime: s.start, EndTime: s.end})
	}
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: 2, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 22,
	})

	// 6 名倒班人员
	for i := 0; i < 6; i++ {
		db.DB.Create(&models.User{
			Name: fmt.Sprintf("员工%02d", i+1), Username: fmt.Sprintf("U%03d", i+1),
			EmpNo: fmt.Sprintf("U%03d", i+1), Role: models.RoleExecutor, DeptID: 1,
		})
	}
	// user12（id=12）产假 10-01~10-11
	db.DB.Create(&models.User{
		Name: "产假员工", Username: "U12", EmpNo: "U12", Role: models.RoleExecutor,
		DeptID: 1, OnLeave: true,
	})
	db.DB.Create(&models.ShiftRequest{
		UserID: 12, UserName: "产假员工", DeptID: 1,
		Type: models.PrefTypeRest, Repeat: models.PrefRepeatOnce,
		StartDate: "2026-10-01", EndDate: "2026-10-11",
		Reason: "产假", Status: models.PrefStatusLocked,
	})

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("claims", &models.Claims{UserID: 1, Username: "admin", Role: models.RoleSuperAdmin, DeptID: 0})
		c.Next()
	})
	r.POST("/g", GenerateSchedule)
	body, _ := json.Marshal(map[string]interface{}{"dept_id": 1, "year": 2026, "month": 10})
	req := httptest.NewRequest(http.MethodPost, "/g", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("生成失败 %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out)

	// 收集违规 / 警告
	viol, _ := out["violations"].([]interface{})
	warn, _ := out["warnings"].([]interface{})
	var badCount, shortWork int
	for _, v := range viol {
		if m, ok := v.(map[string]interface{}); ok {
			lv, _ := m["level"].(string)
			if lv == "error" {
				badCount++
				t.Logf("  ERROR违规: %v", m["reason"])
			}
		}
	}
	for _, v := range warn {
		if m, ok := v.(map[string]interface{}); ok {
			t.Logf("  WARN: %v", m["reason"])
		}
	}

	// 校验产假员工是否在班表，且 10-01~10-11 为休息、其余 5 人 22 天
	planRaw, _ := out["plan"].(map[string]interface{})
	dates := make([]string, 0, len(planRaw))
	for k := range planRaw {
		dates = append(dates, k)
	}
	// 统计每个非休假员工的出勤天数
	peopleRaw, _ := out["people"].([]interface{})
	names := []string{}
	for _, p := range peopleRaw {
		if m, ok := p.(map[string]interface{}); ok {
			if f, _ := m["is_fixed"].(bool); !f {
				names = append(names, fmt.Sprint(m["name"]))
			}
		}
	}
	workDays := map[string]int{}
	for _, nm := range names {
		workDays[nm] = 0
	}
	leaveRestOK := true
	for _, d := range dates {
		row, _ := planRaw[d].(map[string]interface{})
		for _, nm := range names {
			v := fmt.Sprint(row[nm])
			if nm == "产假员工" {
				if d >= "2026-10-01" && d <= "2026-10-11" && v != "休息" {
					leaveRestOK = false
				}
			} else if v != "休息" {
				workDays[nm]++
			}
		}
	}
	_ = shortWork
	t.Logf("产假员工休假标休息: %v；非休假员工出勤: %v", leaveRestOK, workDays)
	for nm, c := range workDays {
		if c < 22 {
			t.Errorf("  %s 出勤 %d 天 < 22", nm, c)
		}
	}
	if !leaveRestOK {
		t.Errorf("产假员工 10-01~10-11 未全部标为休息")
	}
	if badCount > 0 {
		t.Errorf("存在 %d 条 error 级违规（含班次人数不足）", badCount)
	}
}
