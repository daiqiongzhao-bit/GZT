package handlers

import (
	"encoding/json"
	"fmt"
	"testing"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// TestCarryOverPrevMonthRest 规则9 跨月衔接：
// 上月末连续上班已达上限（6 天）→ 本月月初必须安排休息，不能接着连上。
//
// 场景：张三 9 月最后 6 天（9-25 ~ 9-30）连续上班（已发布班表），
// 上限 6 天 → 10 月必须至少休 1 天，且不能在 10-01 接着上。
func TestCarryOverPrevMonthRest(t *testing.T) {
	setupPlanDB(t,
		staff{"张三", "A001"}, staff{"李四", "A002"},
		staff{"王五", "A003"}, staff{"赵六", "A004"},
	)
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: 1, MonthWorkDays: 22,
		MaxRestStreak: 3, MaxWorkStreak: 6, CarryOverPrevMonth: boolPtr(true),
	})

	// 灌入 9 月末已发布班表：张三 9-25~9-30 连上 6 天（均为早班）
	for _, d := range []string{
		"2026-09-25", "2026-09-26", "2026-09-27",
		"2026-09-28", "2026-09-29", "2026-09-30",
	} {
		ppl, _ := json.Marshal([]string{"张三"})
		db.DB.Create(&models.Schedule{Date: d, Shift: "早班", People: string(ppl), DeptID: 1})
	}

	pi := buildPlanInfo(1, 2026, 10, nil)

	// 1) 应正确识别张三的跨月连上班数 = 6
	if cw := pi.CarryWork["张三"]; cw != 6 {
		t.Fatalf("张三 上月末连上班数应为 6，实际 %d（CarryWork=%v）", cw, pi.CarryWork)
	}
	// 上限 6 已满 → hint 应为 1（10 月第 1 个可休日就必须休）
	if h := pi.carryHint["张三"]; h != 1 {
		t.Fatalf("张三 的月初强制休假日应为第 1 天，实际 hint=%d", h)
	}
	// 未连上的人不应有 hint
	if _, ok := pi.carryHint["李四"]; ok {
		t.Fatalf("李四 无跨月连上，不应有 hint")
	}

	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	// 2) 张三 10-01 必须休息（否则跨月连上会到 7 天，超上限 6）
	if !isRestShift(lookPlan(res.Plan, "2026-10-01", "张三")) {
		t.Errorf("张三 上月末已连上 6 天，10-01 应为休息，实际「%s」",
			lookPlan(res.Plan, "2026-10-01", "张三"))
	}

	// 3) 不应出现「跨月连续上班超限」的 error
	for _, v := range res.Violations {
		if v.Rule == "carry_over_work_streak" {
			t.Errorf("出现跨月连上违规: %s %s %s", v.Date, v.Person, v.Reason)
		}
	}
}

// boolPtr 便捷构造 *bool（ShiftRule.CarryOverPrevMonth 为指针类型）。
func boolPtr(v bool) *bool { return &v }

// TestCarryOverDefaultOnWhenNil 规则9 默认开启：
// 未显式配置（nil）时也应读上月、并在月初断休，与「默认开启」的承诺一致。
func TestCarryOverDefaultOnWhenNil(t *testing.T) {
	setupPlanDB(t,
		staff{"张三", "A001"}, staff{"李四", "A002"},
		staff{"王五", "A003"}, staff{"赵六", "A004"},
	)
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: 1, MonthWorkDays: 22,
		MaxRestStreak: 3, MaxWorkStreak: 6, // CarryOverPrevMonth 故意留 nil
	})
	for _, d := range []string{
		"2026-09-25", "2026-09-26", "2026-09-27",
		"2026-09-28", "2026-09-29", "2026-09-30",
	} {
		ppl, _ := json.Marshal([]string{"张三"})
		db.DB.Create(&models.Schedule{Date: d, Shift: "早班", People: string(ppl), DeptID: 1})
	}

	pi := buildPlanInfo(1, 2026, 10, nil)
	if !pi.Rule.CarryOverEnabled() {
		t.Fatalf("未显式配置时应默认开启跨月衔接")
	}
	if cw := pi.CarryWork["张三"]; cw != 6 {
		t.Fatalf("默认开启时应读到张三连上 6 天，实际 %d", cw)
	}
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !isRestShift(lookPlan(res.Plan, "2026-10-01", "张三")) {
		t.Errorf("默认开启时张三 10-01 应为休息，实际「%s」", lookPlan(res.Plan, "2026-10-01", "张三"))
	}
}

// TestCarryOverDisabled 关闭开关后，不应读取上月、也不强制月初休息。
func TestCarryOverDisabled(t *testing.T) {
	setupPlanDB(t,
		staff{"张三", "A001"}, staff{"李四", "A002"},
		staff{"王五", "A003"}, staff{"赵六", "A004"},
	)
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: 1, MonthWorkDays: 22,
		MaxRestStreak: 3, MaxWorkStreak: 6, CarryOverPrevMonth: boolPtr(false),
	})
	for _, d := range []string{
		"2026-09-25", "2026-09-26", "2026-09-27",
		"2026-09-28", "2026-09-29", "2026-09-30",
	} {
		ppl, _ := json.Marshal([]string{"张三"})
		db.DB.Create(&models.Schedule{Date: d, Shift: "早班", People: string(ppl), DeptID: 1})
	}

	pi := buildPlanInfo(1, 2026, 10, nil)
	if len(pi.CarryWork) != 0 {
		t.Fatalf("开关关闭时不应读取上月连上数据，实际 %v", pi.CarryWork)
	}
	if len(pi.carryHint) != 0 {
		t.Fatalf("开关关闭时不应产生强制休假提示，实际 %v", pi.carryHint)
	}
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	for _, v := range res.Violations {
		if v.Rule == "carry_over_work_streak" {
			t.Errorf("开关关闭时不应报跨月违规: %s", v.Reason)
		}
	}
}

// TestCarryOverPartialStreak 上月末只连上 3 天（未达上限 6）：
// 本月可继续上 3 天，第 4 天必须休 → hint=4。
func TestCarryOverPartialStreak(t *testing.T) {
	setupPlanDB(t,
		staff{"张三", "A001"}, staff{"李四", "A002"},
		staff{"王五", "A003"}, staff{"赵六", "A004"},
	)
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: 1, MonthWorkDays: 22,
		MaxRestStreak: 3, MaxWorkStreak: 6, CarryOverPrevMonth: boolPtr(true),
	})
	for _, d := range []string{"2026-09-28", "2026-09-29", "2026-09-30"} {
		ppl, _ := json.Marshal([]string{"张三"})
		db.DB.Create(&models.Schedule{Date: d, Shift: "早班", People: string(ppl), DeptID: 1})
	}

	pi := buildPlanInfo(1, 2026, 10, nil)
	if cw := pi.CarryWork["张三"]; cw != 3 {
		t.Fatalf("张三 连上班数应为 3，实际 %d", cw)
	}
	// 还能再上 3 天（累计到 6），第 4 天必须休
	if h := pi.carryHint["张三"]; h != 4 {
		t.Fatalf("张三 hint 应为 4，实际 %d", h)
	}
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	for _, v := range res.Violations {
		if v.Rule == "carry_over_work_streak" {
			t.Errorf("出现跨月连上违规: %s", v.Reason)
		}
	}
	_ = fmt.Sprint()
}
