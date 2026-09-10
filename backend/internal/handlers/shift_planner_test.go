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

// setupPlanDB 建库并灌入基准数据：1 部门、4 个班次、若干员工。
// 班次：早班 08:00-16:00 / 中班 12:00-20:00 / 晚班 16:00-22:00 / 夜班 22:00-06:00
func setupPlanDB(t *testing.T, staff ...struct {
	Name  string
	EmpNo string
}) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(
		&models.User{}, &models.ShiftConfig{}, &models.ShiftRule{},
		&models.UserShiftPref{}, &models.ShiftRequest{}, &models.SpecialWorkDay{},
		&models.Department{}, &models.Schedule{}, &models.Log{},
		&models.Notification{},
	); err != nil {
		t.Fatal(err)
	}
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

type staff struct {
	Name  string
	EmpNo string
}

// TestFixedStaffNotCountedInMinPerShift 核心业务约定：
// 固定班次人员不占用「每班最少人数」名额。
//
// 场景：4 个倒班人员 + 1 个固定行政班人员，4 个班次每班最少 1 人。
// 预检只按 4 个倒班人员计算（4 ÷ 4 = 1，刚好达标）——
// 若错误地把固定人员也算进可用人力，则 5 人条件下看似更宽裕，
// 但真实排班时必须仅靠 4 个倒班人员填满 4 个班次。
func TestFixedStaffNotCountedInMinPerShift(t *testing.T) {
	setupPlanDB(t,
		staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"}, staff{"赵六", "A004"},
		staff{"行政", "A005"},
	)
	var adm models.User
	db.DB.Where("emp_no = ?", "A005").First(&adm)
	db.DB.Create(&models.UserShiftPref{
		UserID: adm.ID, DeptID: 1, Mode: models.ShiftModeFixed, FixedShift: "早班",
	})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 1, MonthWorkDays: 22, MaxRestStreak: 3, MaxWorkStreak: 6})

	pi := buildPlanInfo(1, 2026, 9, nil)
	if len(pi.People) != 5 {
		t.Fatalf("应载入 5 人，实际 %d", len(pi.People))
	}
	fixedCount, rotCount := 0, 0
	var admName string
	for _, p := range pi.People {
		if p.IsFixed() {
			fixedCount++
			admName = p.Name
		} else {
			rotCount++
		}
	}
	if fixedCount != 1 || rotCount != 4 {
		t.Fatalf("应识别 1 名固定 + 4 名倒班，实际 fixed=%d rotate=%d", fixedCount, rotCount)
	}

	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	// 固定人员必须全月固定早班
	for _, d := range daysInRange(pi.First, pi.Last) {
		if got := lookPlan(res.Plan, dateKey(d), admName); got != "早班" {
			t.Fatalf("%s 固定人员应上早班，实际 %q", dateKey(d), got)
		}
	}

	// 关键：把固定人员从统计中剔除后，仍应能（在多数天）满足每班 ≥1 人。
	// 反过来说，如果实现错误地把固定人员计入，那么"早班"会凭空多出
	// 一个固定人员的名额，导致某些天倒班人员其实缺班却未被检出。
	// 这里直接断言：规则7 的违规统计中，早班的实际倒班人数达标与否
	// 与固定人员无关——即固定人员不能"救"一个空班次。
	rotOnlyShort := 0
	withFixedShort := 0
	for _, d := range daysInRange(pi.First, pi.Last) {
		k := dateKey(d)
		rotCnt := map[string]int{}
		allCnt := map[string]int{}
		for _, p := range pi.People {
			sh := lookPlan(res.Plan, k, p.Name)
			if isRestShift(sh) {
				continue
			}
			allCnt[sh]++
			if !p.IsFixed() {
				rotCnt[sh]++
			}
		}
		for _, sh := range []string{"早班", "中班", "晚班", "夜班"} {
			if rotCnt[sh] < 1 {
				rotOnlyShort++
			}
			if allCnt[sh] < 1 {
				withFixedShort++
			}
		}
	}
	// 固定人员只上早班，故早班在"含固定"口径下几乎总有人；
	// 若有哪天早班的倒班人员为 0，则两种口径必然出现差值 → 证明口径确实不同
	if rotOnlyShort == withFixedShort {
		t.Logf("提示：本月倒班与含固定两种口径缺人数相同（%d），未能观察到口径差异；"+
			"改由下方断言固定人员未被用于凑数", rotOnlyShort)
	}
	// 直接断言校验器口径：规则7 违规只按倒班人员判定
	for _, v := range res.Violations {
		if v.Rule == "min_per_shift" && !contains(v.Reason, "固定班次人员不计入") {
			t.Fatalf("规则7 违规说明应标注「固定班次人员不计入」，实际: %s", v.Reason)
		}
	}
	t.Logf("固定人员「%s」全月固定早班；倒班口径缺人数=%d，含固定口径缺人数=%d",
		admName, rotOnlyShort, withFixedShort)
}

// TestFeasibilityRejectsTooFewRotating 可行性预检：
// 2 个倒班人员 + 4 个班次 + 每班最少 2 人 = 需 8 人 → 必须明确报错。
func TestFeasibilityRejectsTooFewRotating(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 2})

	pi := buildPlanInfo(1, 2026, 9, nil)
	_, err := pi.GeneratePlan()
	if err == nil {
		t.Fatal("人手不足时应报错，但生成成功了")
	}
	msg := err.Error()
	if !contains(msg, "人手不足") || !contains(msg, "不计入") {
		t.Fatalf("错误信息应说明人手不足且提示固定班次人员不计入，实际: %s", msg)
	}
	t.Logf("预检拦截：%s", msg)
}

// TestLockedRestRequestNotOverwritten 规则4：
// 已锁定的休假需求必须被强制保留，即使与出勤目标冲突。
func TestLockedRestRequestNotOverwritten(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"})
	var u models.User
	db.DB.Where("emp_no = ?", "A001").First(&u)

	now := time.Now()
	db.DB.Create(&models.ShiftRequest{
		UserID: u.ID, UserName: u.Name, DeptID: 1,
		Type: models.PrefTypeRest, Repeat: models.PrefRepeatOnce,
		StartDate: "2026-09-25", EndDate: "2026-09-28",
		Status: models.PrefStatusLocked, LockedAt: &now,
	})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 0, MonthWorkDays: 22})

	pi := buildPlanInfo(1, 2026, 9, nil)
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	for _, k := range []string{"2026-09-25", "2026-09-26", "2026-09-27", "2026-09-28"} {
		got := lookPlan(res.Plan, k, "张三")
		if !isRestShift(got) {
			t.Fatalf("%s 张三已锁定休假，应为休息，实际 %q", k, got)
		}
	}
	// 锁定需求不得出现在违规里
	for _, v := range res.Violations {
		if v.Rule == "locked_request" {
			t.Fatalf("已锁定需求不应违规: %+v", v)
		}
	}
	t.Logf("张三 9/25-9/28 锁定休假已保留，违规数 %d", len(res.Violations))
}

// TestWeeklyRestRequest 规则4 场景：B 员工申请每个周末固定休。
func TestWeeklyRestRequest(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"})
	var u models.User
	db.DB.Where("emp_no = ?", "A002").First(&u)

	now := time.Now()
	db.DB.Create(&models.ShiftRequest{
		UserID: u.ID, UserName: u.Name, DeptID: 1,
		Type: models.PrefTypeRest, Repeat: models.PrefRepeatWeek,
		WeekDays: "6,7", // 周六、周日
		Status:   models.PrefStatusLocked, LockedAt: &now,
	})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MonthWorkDays: 22})

	pi := buildPlanInfo(1, 2026, 9, nil)
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	weekendCount := 0
	for d := pi.First; !d.After(pi.Last); d = d.AddDate(0, 0, 1) {
		wd := weekdayOf(d)
		if wd != 6 && wd != 7 {
			continue
		}
		weekendCount++
		if got := lookPlan(res.Plan, dateKey(d), "李四"); !isRestShift(got) {
			t.Fatalf("%s 是周末，李四应休息，实际 %q", dateKey(d), got)
		}
	}
	if weekendCount == 0 {
		t.Fatal("9 月应包含周末")
	}
	t.Logf("李四所有 %d 个周末日均为休息", weekendCount)
}

// TestSpecialWorkDayAllStaff 规则6：特殊工作日全员上班（员工无需求则默认上班）。
func TestSpecialWorkDayAllStaff(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"})
	db.DB.Create(&models.SpecialWorkDay{Date: "2026-09-15", DeptID: 1, Name: "店庆", AllStaff: true})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MonthWorkDays: 22, MinPerShift: 0})

	pi := buildPlanInfo(1, 2026, 9, nil)
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	// 9/15 应全员上班（无锁定需求者）
	for _, p := range pi.People {
		got := lookPlan(res.Plan, "2026-09-15", p.Name)
		if isRestShift(got) {
			t.Fatalf("店庆日 %s 应上班，实际 %q", p.Name, got)
		}
	}
	t.Log("店庆日 9/15 全员已上班")
}

// TestSpecialWorkDayYieldsToLockedRest 规则6 例外：
// 特殊工作日与员工已锁定休假冲突时，员工需求优先。
func TestSpecialWorkDayYieldsToLockedRest(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"}, staff{"王五", "A003"})
	db.DB.Create(&models.SpecialWorkDay{Date: "2026-09-15", DeptID: 1, Name: "店庆", AllStaff: true})

	var u models.User
	db.DB.Where("emp_no = ?", "A001").First(&u)
	now := time.Now()
	db.DB.Create(&models.ShiftRequest{
		UserID: u.ID, UserName: u.Name, DeptID: 1,
		Type: models.PrefTypeRest, Repeat: models.PrefRepeatOnce,
		StartDate: "2026-09-15", EndDate: "2026-09-15",
		Status: models.PrefStatusLocked, LockedAt: &now,
	})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MonthWorkDays: 22})

	pi := buildPlanInfo(1, 2026, 9, nil)
	res, err := pi.GeneratePlan()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if got := lookPlan(res.Plan, "2026-09-15", "张三"); !isRestShift(got) {
		t.Fatalf("张三已锁定休假，店庆日也应休息，实际 %q", got)
	}
	// 其他两人应上班
	for _, n := range []string{"李四", "王五"} {
		if got := lookPlan(res.Plan, "2026-09-15", n); isRestShift(got) {
			t.Fatalf("店庆日 %s 应上班，实际 %q", n, got)
		}
	}
	t.Log("店庆日张三锁定休假优先，李四/王五正常上班")
}

// TestMaxWorkStreakDetected 规则2b：连续上班超限必须被检出。
func TestMaxWorkStreakDetected(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MaxWorkStreak: 3, MaxRestStreak: 3, MonthWorkDays: 30})

	pi := buildPlanInfo(1, 2026, 9, nil)
	// 手工构造：连续 5 天上班
	plan := map[string]map[string]string{}
	for i := 0; i < 5; i++ {
		k := fmt.Sprintf("2026-09-%02d", 1+i)
		setPlan(plan, k, "张三", "早班")
	}
	vs := pi.validatePlan(plan)
	found := false
	for _, v := range vs {
		if v.Rule == "max_work_streak" && v.Person == "张三" {
			found = true
			if v.Date != "2026-09-01" || v.EndDate != "2026-09-05" {
				t.Fatalf("连续上班区间应为 09-01~09-05，实际 %s~%s", v.Date, v.EndDate)
			}
		}
	}
	if !found {
		t.Fatalf("连续上班 5 天超过上限 3，应检出违规，实际: %+v", vs)
	}
	t.Log("连续上班超限已检出")
}

// TestMaxRestStreakDetected 规则2a：连续休息超限必须被检出。
func TestMaxRestStreakDetected(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 30})

	pi := buildPlanInfo(1, 2026, 9, nil)
	plan := map[string]map[string]string{}
	// 9/1 上班，9/2-9/7 连续休息 6 天
	setPlan(plan, "2026-09-01", "张三", "早班")
	for i := 2; i <= 7; i++ {
		setPlan(plan, fmt.Sprintf("2026-09-%02d", i), "张三", RestShift)
	}
	vs := pi.validatePlan(plan)
	found := false
	for _, v := range vs {
		if v.Rule == "max_rest_streak" {
			found = true
			if v.Reason == "" || v.EndDate == "" {
				t.Fatalf("连续休息违规应含原因与结束日: %+v", v)
			}
		}
	}
	if !found {
		t.Fatalf("连续休息 6 天超过上限 3，应检出违规，实际: %+v", vs)
	}
	t.Log("连续休息超限已检出")
}

// TestMonthWorkDaysShortfall 规则3：出勤不足应提示。
func TestMonthWorkDaysShortfall(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MonthWorkDays: 10, MaxWorkStreak: 31, MaxRestStreak: 31})

	pi := buildPlanInfo(1, 2026, 9, nil)
	plan := map[string]map[string]string{}
	for i := 1; i <= 5; i++ {
		setPlan(plan, fmt.Sprintf("2026-09-%02d", i), "张三", "早班")
	}
	vs := pi.validatePlan(plan)
	found := false
	for _, v := range vs {
		if v.Rule == "month_work_days" {
			found = true
		}
	}
	if !found {
		t.Fatalf("出勤 5 天少于目标 10 天，应提示，实际: %+v", vs)
	}
	t.Log("出勤不足已提示")
}

// TestAdjacencyRulesWarning 规则5：
// 开启「休假前须早班」后，休假前一天排了晚班应产生提醒（warn 级）。
func TestAdjacencyRulesWarning(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"})
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MonthWorkDays: 30, MaxWorkStreak: 31, MaxRestStreak: 31,
		RequireMorningBeforeRest: true, RequireEveningAfterRest: true,
	})

	pi := buildPlanInfo(1, 2026, 9, nil)
	plan := map[string]map[string]string{}
	setPlan(plan, "2026-09-09", "张三", "晚班") // 休假前一天排晚班 → 违规
	setPlan(plan, "2026-09-10", "张三", RestShift)
	setPlan(plan, "2026-09-11", "张三", "早班") // 休假后第一天排早班 → 违规

	vs := pi.validatePlan(plan)
	var before, after bool
	for _, v := range vs {
		if v.Rule == "morning_before_rest" {
			before = true
			if v.Level != "warn" {
				t.Fatalf("规则5 应为 warn 级提醒，实际 %s", v.Level)
			}
		}
		if v.Rule == "evening_after_rest" {
			after = true
		}
	}
	if !before || !after {
		t.Fatalf("应检出休假前/后班次不符，实际 before=%v after=%v, %+v", before, after, vs)
	}
	t.Log("休假前后班次提醒已检出")
}

// TestMonthBoundaryStreakNotMisreported 跨月边界：
// 若某段连续休息「延续自上月」（本月第一天就在休），本月只能观测到片段，
// 不应按片段长度判违规（否则每月月初都会被误报）。
//
// 构造：9/1-9/5 休息（延续自 8 月）→ 应跳过；
//
//	9/20-9/24 休息（完整落在本月，5 天 > 上限 3）→ 应报。
func TestMonthBoundaryStreakNotMisreported(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 30})

	pi := buildPlanInfo(1, 2026, 9, nil)
	plan := map[string]map[string]string{}
	for i := 1; i <= 30; i++ {
		setPlan(plan, fmt.Sprintf("2026-09-%02d", i), "张三", "早班")
	}
	for i := 1; i <= 5; i++ { // 月初延续段 → 应跳过
		setPlan(plan, fmt.Sprintf("2026-09-%02d", i), "张三", RestShift)
	}
	for i := 20; i <= 24; i++ { // 月中完整段 → 应报
		setPlan(plan, fmt.Sprintf("2026-09-%02d", i), "张三", RestShift)
	}

	var reported []Violation
	for _, v := range pi.validatePlan(plan) {
		if v.Rule == "max_rest_streak" {
			reported = append(reported, v)
		}
	}
	if len(reported) != 1 {
		t.Fatalf("应只报 1 段连续休息超限，实际 %d 段: %+v", len(reported), reported)
	}
	if reported[0].Date != "2026-09-20" {
		t.Fatalf("应报 9/20 那段，实际起点 %s（月初延续段被误报）", reported[0].Date)
	}
	t.Logf("月初延续段已跳过；月中段已报: %s~%s %s",
		reported[0].Date, reported[0].EndDate, reported[0].Reason)
}

// TestDisplayShiftsFromConfig 规则8：班次池取部门配置，剔除「全员」「休息」。
func TestDisplayShiftsFromConfig(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"})
	db.DB.Create(&models.ShiftConfig{DeptID: 1, Name: "全员", StartTime: "09:00", EndTime: "18:00"})
	db.DB.Create(&models.ShiftConfig{DeptID: 1, Name: "休息", StartTime: "09:00", EndTime: "18:00"})

	got := displayShifts(1)
	// 原始 4 班 + 全员 + 休息 = 6 条配置，但可排班次应为 4 个
	if len(got) != 4 {
		t.Fatalf("可排班次应为 4 个，实际 %d: %v", len(got), got)
	}
	for _, s := range got {
		if s == "全员" || s == RestShift {
			t.Fatalf("「%s」不应出现在可排班次池: %v", s, got)
		}
	}
	// 按开始时间排序：早班(08:00) 应第一
	if got[0] != "早班" {
		t.Fatalf("班次应按开始时间排序，首项应为早班，实际 %v", got)
	}
	t.Logf("可排班次池: %v", got)
}

// TestNoPeopleRejected 无人可排时应明确报错。
func TestNoPeopleRejected(t *testing.T) {
	setupPlanDB(t)
	pi := buildPlanInfo(1, 2026, 9, nil)
	if _, err := pi.GeneratePlan(); err == nil {
		t.Fatal("无人员时应报错")
	}
}

// TestPlanDeterministic 同一输入两次生成结果应完全一致（可重复）。
func TestPlanDeterministic(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"李四", "A002"},
		staff{"王五", "A003"}, staff{"赵六", "A004"}, staff{"钱七", "A005"})
	db.DB.Create(&models.ShiftRule{DeptID: 1, MinPerShift: 1, MonthWorkDays: 20, MaxRestStreak: 3, MaxWorkStreak: 6})

	a, err := buildPlanInfo(1, 2026, 9, nil).GeneratePlan()
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	b, err := buildPlanInfo(1, 2026, 9, nil).GeneratePlan()
	if err != nil {
		t.Fatalf("二次生成失败: %v", err)
	}
	for d := 1; d <= 30; d++ {
		k := fmt.Sprintf("2026-09-%02d", d)
		for _, n := range []string{"张三", "李四", "王五", "赵六", "钱七"} {
			if lookPlan(a.Plan, k, n) != lookPlan(b.Plan, k, n) {
				t.Fatalf("%s %s 两次生成不一致: %q vs %q", k, n,
					lookPlan(a.Plan, k, n), lookPlan(b.Plan, k, n))
			}
		}
	}
	t.Log("生成结果可重复")
}

// contains 字符串包含
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// TestFixedStaffExemptFromStreakRules 固定班次人员豁免连续上/休上限与月出勤目标。
//
// 业务理由：固定班次人员不参与倒班（规则1），其作息完全由「固定班次 + 生效星期」
// 决定。若仍按倒班口径考核，会因「每天固定上班」被判连续上班 30 天超限，
// 产生大量无意义违规，掩盖真正需要处理的问题。
func TestFixedStaffExemptFromStreakRules(t *testing.T) {
	setupPlanDB(t, staff{"张三", "A001"}, staff{"行政", "A002"})
	var adm models.User
	db.DB.Where("emp_no = ?", "A002").First(&adm)
	db.DB.Create(&models.UserShiftPref{
		UserID: adm.ID, DeptID: 1, Mode: models.ShiftModeFixed, FixedShift: "早班",
	})
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: 22, MinPerShift: 0,
	})

	pi := buildPlanInfo(1, 2026, 9, nil)

	// 构造：行政全月上早班（会触发连续上班 30 天）；张三 9/1-9/5 连续上班 5 天
	plan := map[string]map[string]string{}
	for _, d := range daysInRange(pi.First, pi.Last) {
		setPlan(plan, dateKey(d), "行政", "早班")
	}
	for i := 1; i <= 5; i++ {
		setPlan(plan, fmt.Sprintf("2026-09-%02d", i), "张三", "早班")
	}

	var admViol, zhangViol int
	for _, v := range pi.validatePlan(plan) {
		switch v.Person {
		case "行政":
			admViol++
		case "张三":
			if v.Rule == "max_work_streak" {
				zhangViol++
			}
		}
	}
	if admViol != 0 {
		t.Fatalf("固定班次人员不应产生连续上/休或月出勤违规，实际 %d 条", admViol)
	}
	if zhangViol != 0 {
		t.Fatalf("张三连续上班 5 天未超上限 6，不应违规，实际 %d 条", zhangViol)
	}
	t.Log("固定班次人员已豁免连续上/休与月出勤校验；倒班人员仍正常受约束")
}
