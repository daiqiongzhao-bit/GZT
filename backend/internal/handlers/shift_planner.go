package handlers

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// ============ 排班计划引擎（v0.18.0）============
//
// 计划表结构：plan[YYYY-MM-DD][姓名] = 班次名
//   - 班次名取本部门 ShiftConfig 定义的班次（规则8）
//   - RestShift（"休息"）表示休息；某人在某日无记录时按休息处理
//
// 8 条规则的落点：
//   规则1 固定班次人员不参与倒班     → loadPlanPeople 标记 Mode=fixed
//   规则2 最高连续休 / 连续上        → validatePlan 检查 + 生成器规避
//   规则3 月度出勤天数               → validatePlan 汇总 + 生成器目标
//   规则4 员工需求锁定不可改         → enforceLocks 强制还原
//   规则5 休假前早班 / 休假后晚班    → validatePlan 相邻检查
//   规则6 特殊工作日全员上班         → 生成器硬性铺满
//   规则7 每班次最少人数             → validatePlan 按天按班次检查
//   规则8 可设置有哪些班次           → displayShifts 取部门配置

// RestShift 休息占位班次名。
const RestShift = "休息"

// PendingShift 待定占位：表示「该日已确定要上班，但班次尚未分配」。
// 它既不是休息，也不是可展示的班次；生成器在 fillShifts 中会把它替换掉。
// 引入显式占位是为了消除「空串」的二义性——空串既可能表示"无记录(休息)"，
// 也可能表示"待填班次"，会让特殊工作日拉起的人员被误判为休息。
const PendingShift = "\x00pending"

// parseHM 解析 "09:00" → 分钟数；非法返回 -1。
func parseHM(s string) int {
	s = strings.TrimSpace(s)
	if len(s) != 5 || s[2] != ':' {
		return -1
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return -1
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return -1
	}
	return h*60 + m
}

// dateKey 格式化 YYYY-MM-DD
func dateKey(t time.Time) string { return t.Format("2006-01-02") }

// dayOf 归一化到当天零点（保留时区）
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// parseDateKey 解析 YYYY-MM-DD；空串/非法返回 false。
func parseDateKey(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 10 {
		s = s[:10]
	}
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// monthRange 返回某年月的首日与末日。
func monthRange(year, month int) (time.Time, time.Time) {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	return first, first.AddDate(0, 1, -1)
}

// daysInRange 列出 [from,to] 的每一天。
func daysInRange(from, to time.Time) []time.Time {
	var out []time.Time
	for d := dayOf(from); !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

// weekdayOf 返回 1=周一 … 7=周日
func weekdayOf(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return wd
}

// parseWeekDays 解析 "1,3,5" → []int{1,3,5}；空串返回 nil（语义为「每天」）。
func parseWeekDays(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '、'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(part, "%d", &n); err != nil {
			continue
		}
		if n < 1 || n > 7 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// displayShifts 部门可选班次（规则8）：取本部门 ShiftConfig，按开始时间升序。
// 无配置时回退默认四班，保证新部门开箱可用。
func displayShifts(deptID uint) []string {
	var cfgs []models.ShiftConfig
	db.DB.Where("dept_id = ?", deptID).Find(&cfgs)
	if len(cfgs) == 0 {
		return []string{"早班", "中班", "晚班", "夜班"}
	}
	sort.SliceStable(cfgs, func(i, j int) bool {
		a, b := parseHM(cfgs[i].StartTime), parseHM(cfgs[j].StartTime)
		if a < 0 {
			a = 24 * 60
		}
		if b < 0 {
			b = 24 * 60
		}
		return a < b
	})
	out := make([]string, 0, len(cfgs))
	seen := map[string]bool{}
	for _, c := range cfgs {
		n := strings.TrimSpace(c.Name)
		if n == "" || seen[n] {
			continue
		}
		// 「全员」「休息」不是可排的实体班次
		if n == RestShift || n == "全员" {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// pickMorningEvening 挑出「早班」「晚班」代表班次：
// 优先规则里显式配置的名称，否则按名称特征推断，最后按首尾兜底。
func pickMorningEvening(rule models.ShiftRule, shifts []string) (string, string) {
	morning := strings.TrimSpace(rule.MorningShiftName)
	evening := strings.TrimSpace(rule.EveningShiftName)
	if morning == "" {
		for _, s := range shifts {
			if strings.Contains(s, "早") {
				morning = s
				break
			}
		}
	}
	if evening == "" {
		for _, s := range shifts {
			if strings.Contains(s, "晚") || strings.Contains(s, "夜") {
				evening = s
				break
			}
		}
	}
	if morning == "" && len(shifts) > 0 {
		morning = shifts[0]
	}
	if evening == "" && len(shifts) > 0 {
		evening = shifts[len(shifts)-1]
	}
	return morning, evening
}

// ruleForDept 取部门规则配置；不存在时返回带默认值的对象（不落库）。
func ruleForDept(deptID uint) models.ShiftRule {
	var r models.ShiftRule
	if err := db.DB.Where("dept_id = ?", deptID).First(&r).Error; err == nil {
		if r.MaxRestStreak <= 0 {
			r.MaxRestStreak = 3
		}
		if r.MaxWorkStreak <= 0 {
			r.MaxWorkStreak = 6
		}
		if r.MonthWorkDays <= 0 {
			r.MonthWorkDays = 22
		}
		return r
	}
	return models.ShiftRule{
		DeptID:               deptID,
		MaxRestStreak:        3,
		MaxWorkStreak:        6,
		MonthWorkDays:        22,
		AllowExceedMonthDays: true,
	}
}

// PlanPerson 参与排班的员工。
//
// 字段带 json tag：本结构会随 /schedules/generate 直接返回给前端，
// 前端据此渲染「固定班次」标记与锁死逻辑。IsFixed 是方法，不属于
// JSON 字段，因此额外导出 IsFixed 布尔字段供前端直接使用。
type PlanPerson struct {
	UserID      uint   `json:"user_id"`
	Name        string `json:"name"`
	EmpNo       string `json:"emp_no"`
	Mode        string `json:"mode"`        // rotate 倒班 / fixed 固定班次
	FixedShift  string `json:"fixed_shift"` // Mode=fixed 时的固定班次名
	FixedDays   []int  `json:"fixed_days"`  // 固定班次的生效星期；nil = 每天
	IsFixedFlag bool   `json:"is_fixed"`    // 派生字段：是否固定班次人员
}

// IsFixed 是否固定班次人员（规则1）。固定班次人员不参与倒班轮转，
// 且按业务约定【不占用每班最少人数名额】。
func (p PlanPerson) IsFixed() bool { return p.Mode == models.ShiftModeFixed && p.FixedShift != "" }

// fixedOn 判断固定班次人员在某天是否上其固定班次。
func (p PlanPerson) fixedOn(day time.Time) bool {
	if !p.IsFixed() {
		return false
	}
	if len(p.FixedDays) == 0 {
		return true
	}
	for _, d := range p.FixedDays {
		if d == weekdayOf(day) {
			return true
		}
	}
	return false
}

// loadPlanPeople 载入参与排班的员工（排除冻结 / 休假中 / 无姓名）。
// userIDs 非空时按指定人员取；否则按 deptID 及其子孙部门取。
func loadPlanPeople(deptID uint, userIDs []uint) []PlanPerson {
	var users []models.User
	q := db.DB.Where("frozen = ? AND on_leave = ?", false, false)
	if len(userIDs) > 0 {
		q = q.Where("id IN ?", userIDs)
	} else if deptID > 0 {
		q = q.Where("dept_id IN ?", descendantDeptIDs(deptID))
	}
	q.Order("id asc").Find(&users)
	if len(users) == 0 {
		return nil
	}

	ids := make([]uint, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	var prefs []models.UserShiftPref
	db.DB.Where("user_id IN ?", ids).Find(&prefs)
	pm := map[uint]models.UserShiftPref{}
	for _, p := range prefs {
		pm[p.UserID] = p
	}

	out := make([]PlanPerson, 0, len(users))
	for _, u := range users {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			continue
		}
		p := PlanPerson{UserID: u.ID, Name: name, EmpNo: u.EmpNo, Mode: models.ShiftModeRotate}
		if pref, ok := pm[u.ID]; ok && pref.Mode == models.ShiftModeFixed {
			if fs := strings.TrimSpace(pref.FixedShift); fs != "" {
				p.Mode = models.ShiftModeFixed
				p.FixedShift = fs
				p.FixedDays = parseWeekDays(pref.FixedWeekDays)
			}
		}
		p.IsFixedFlag = p.IsFixed() // 派生标记：前端据此渲染锁死逻辑
		out = append(out, p)
	}
	return out
}

// ---- 员工需求（规则4）----

// requestCovers 判断需求是否覆盖某天。
func requestCovers(r models.ShiftRequest, day time.Time) bool {
	if r.Repeat == models.PrefRepeatWeek {
		days := parseWeekDays(r.WeekDays)
		if len(days) == 0 {
			return false
		}
		wd := weekdayOf(day)
		for _, d := range days {
			if d == wd {
				return true
			}
		}
		return false
	}
	// once：按区间（同一天时首尾相等）
	st, ok := parseDateKey(r.StartDate)
	if !ok {
		return false
	}
	en, ok2 := parseDateKey(r.EndDate)
	if !ok2 {
		en = st
	}
	if en.Before(st) {
		st, en = en, st
	}
	d := dayOf(day)
	return !d.Before(st) && !d.After(en)
}

// loadSpecialDays 载入区间内该部门（含全局 dept_id=0）的特殊工作日。
func loadSpecialDays(deptID uint, from, to string) map[string]string {
	var list []models.SpecialWorkDay
	q := db.DB.Where("date >= ? AND date <= ?", from, to)
	if deptID > 0 {
		q = q.Where("dept_id = ? OR dept_id = 0", deptID)
	}
	q.Find(&list)
	out := map[string]string{}
	for _, s := range list {
		if !s.AllStaff {
			continue
		}
		if _, exists := out[s.Date]; !exists {
			out[s.Date] = s.Name
		}
	}
	return out
}

// PlanInfo 一次性装配的生成 / 校验上下文。
type PlanInfo struct {
	DeptID    uint
	Year      int
	Month     int
	First     time.Time
	Last      time.Time
	Rule      models.ShiftRule
	Shifts    []string // 可排班次池（规则8）
	Morning   string   // 「早班」代表
	Evening   string   // 「晚班」代表
	People    []PlanPerson
	Special   map[string]string // 特殊工作日 date→事由
	ReqByUser map[uint][]models.ShiftRequest
}

// buildPlanInfo 装配某部门某月的排班上下文。
func buildPlanInfo(deptID uint, year, month int, userIDs []uint) *PlanInfo {
	first, last := monthRange(year, month)
	rule := ruleForDept(deptID)
	shifts := displayShifts(deptID)
	morning, evening := pickMorningEvening(rule, shifts)

	var reqs []models.ShiftRequest
	rq := db.DB.Order("user_id asc, id asc")
	if deptID > 0 {
		rq = rq.Where("dept_id IN ?", descendantDeptIDs(deptID))
	}
	rq.Find(&reqs)
	byUser := map[uint][]models.ShiftRequest{}
	for _, r := range reqs {
		byUser[r.UserID] = append(byUser[r.UserID], r)
	}

	return &PlanInfo{
		DeptID:    deptID,
		Year:      year,
		Month:     month,
		First:     first,
		Last:      last,
		Rule:      rule,
		Shifts:    shifts,
		Morning:   morning,
		Evening:   evening,
		People:    loadPlanPeople(deptID, userIDs),
		Special:   loadSpecialDays(deptID, dateKey(first), dateKey(last)),
		ReqByUser: byUser,
	}
}

// requestOn 返回某员工某天的生效需求（休假优先于上班）。
func (pi *PlanInfo) requestOn(userID uint, day time.Time) (models.ShiftRequest, string, bool) {
	var work *models.ShiftRequest
	for _, r := range pi.ReqByUser[userID] {
		if !requestCovers(r, day) {
			continue
		}
		if r.Type == models.PrefTypeRest {
			return r, models.PrefTypeRest, true
		}
		rr := r
		work = &rr
	}
	if work != nil {
		return *work, models.PrefTypeWork, true
	}
	return models.ShiftRequest{}, "", false
}

// isSpecialWorkDay 当天是否特殊工作日（规则6）。
func (pi *PlanInfo) isSpecialWorkDay(day time.Time) bool {
	_, ok := pi.Special[dateKey(day)]
	return ok
}

// isRestShift 判断班次名是否表示休息。
// 注意：PendingShift（待填班次）**不算**休息——它是「已定要上班」的占位。
func isRestShift(shift string) bool {
	s := strings.TrimSpace(shift)
	return s == "" || s == RestShift
}

// isPendingShift 判断是否为「待填班次」占位。
func isPendingShift(shift string) bool {
	return strings.TrimSpace(shift) == PendingShift
}

// lookPlan 读取计划表中某人某天的班次；无记录返回空串（视为休息）。
func lookPlan(plan map[string]map[string]string, date, name string) string {
	if m, ok := plan[date]; ok {
		return strings.TrimSpace(m[name])
	}
	return ""
}

// setPlan 写入计划表。
func setPlan(plan map[string]map[string]string, date, name, shift string) {
	if plan[date] == nil {
		plan[date] = map[string]string{}
	}
	plan[date][name] = shift
}
