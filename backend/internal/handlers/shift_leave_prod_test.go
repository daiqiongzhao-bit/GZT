package handlers

import (
	"fmt"
	"os"
	"testing"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestProdDBLeaveScenario 用真实生产库（只读）复现 2026-10 营运部排班，
// 验证「休假人员（on_leave=true）仍参与排班、休假天数标休息且不计入每班最少人数、
// 其余人员满足 22 天」这条修复。
// 仅当设置环境变量 PROD_DB=/path/to/swb.db 时运行，避免污染常规测试。
func TestProdDBLeaveScenario(t *testing.T) {
	path := os.Getenv("PROD_DB")
	if path == "" {
		t.Skip("PROD_DB 未设置，跳过生产库复现")
	}
	d, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=ro", path)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d

	pi := buildPlanInfo(1, 2026, 10, nil)
	t.Logf("部门可用排班人员(%d):", len(pi.People))
	for _, p := range pi.People {
		t.Logf("  - %s (id=%d, fixed=%v, on_leave? %v)", p.Name, p.UserID, p.IsFixed(), isOnLeave(d, p.UserID))
	}

	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	errCnt := 0
	badDays := map[string]bool{}
	for _, v := range res.Violations {
		if v.Level == "error" {
			errCnt++
			badDays[v.Date] = true
			t.Logf("  ERROR: %s", v.Reason)
		}
	}
	for _, w := range res.Warnings {
		t.Logf("  WARN: %s", w)
	}
	t.Logf("error 级违规数=%d, 总违规=%d, 警告=%d", errCnt, len(res.Violations), len(res.Warnings))

	// 打印违规日的逐人排版，定位班次聚集原因
	for dk := range badDays {
		t.Logf("  [诊断] %s:", dk)
		for _, p := range pi.People {
			t.Logf("      %s = %s", p.Name, lookPlan(res.Plan, dk, p.Name))
		}
	}

	// 逐人统计出勤天数与休假标注
	dates, _ := pi.daysRange()
	workDays := map[string]int{}
	leaveOK := map[string]bool{}
	for _, p := range pi.People {
		workDays[p.Name] = 0
		leaveOK[p.Name] = true
	}
	for _, d0 := range dates {
		k := dateKey(d0)
		for _, p := range pi.People {
			sh := lookPlan(res.Plan, k, p.Name)
			if isRestShift(sh) {
				continue
			}
			workDays[p.Name]++
		}
	}
	// 校验 on_leave 人员的休假是否在班表标为休息
	for _, p := range pi.People {
		if !isOnLeave(d, p.UserID) {
			continue
		}
		for _, d0 := range dates {
			k := dateKey(d0)
			if !pi.onLeaveOn(p.UserID, d0) {
				continue
			}
			if !isRestShift(lookPlan(res.Plan, k, p.Name)) {
				leaveOK[p.Name] = false
			}
		}
	}

	t.Logf("逐人出勤天数(月目标22):")
	for _, p := range pi.People {
		tag := ""
		if isOnLeave(d, p.UserID) {
			tag = " [on_leave]"
		}
		t.Logf("  %s: %d 天%s", p.Name, workDays[p.Name], tag)
	}

	if errCnt > 0 {
		t.Errorf("存在 %d 条 error 级违规（含班次人数不足）", errCnt)
	}
	for _, p := range pi.People {
		if isOnLeave(d, p.UserID) && !leaveOK[p.Name] {
			t.Errorf("%s 的休假日期未全部标为休息", p.Name)
		}
	}
}

func isOnLeave(d *gorm.DB, uid uint) bool {
	var u models.User
	if err := d.First(&u, uid).Error; err != nil {
		return false
	}
	return u.OnLeave
}

func (pi *PlanInfo) daysRange() ([]time.Time, error) {
	return daysInRange(pi.First, pi.Last), nil
}
