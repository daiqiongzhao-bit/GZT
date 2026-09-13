package handlers

import (
	"encoding/json"
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

// loadPlanPeople 载入参与排班的员工（排除冻结 / 无姓名）。
// 注意：处于「休假」状态（on_leave）的人员【不再被整体排除】——
// 产假、婚假等只把其休假日期标记为休息（见阶段1b），这些天不计入
// 「每班最少人数」名额，其余日子仍正常排班，从而满足其他人每月 22 天班。
// 仅当某人整月没有任何「已锁定的休假需求」却仍被标记 on_leave 时，
// 才视为「完全停职/不可用」而排除（例如长期停职人员）。
// userIDs 非空时按指定人员取；否则按 deptID 及其子孙部门取。
func loadPlanPeople(deptID uint, userIDs []uint, byUser map[uint][]models.ShiftRequest, first, last time.Time) []PlanPerson {
	var users []models.User
	q := db.DB.Where("frozen = ?", false)
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
		// 完全停职/不可用：整月没有任何已锁定休假需求却标记了 on_leave，
		// 说明不是「带薪假」而是长期离岗，整月不参与排班。
		if u.OnLeave && !hasLockedLeaveInRange(byUser, u.ID, first, last) {
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

// hasLockedLeaveInRange 判断某人本月是否存在「已锁定的休假需求」覆盖 [first,last]
// 中的任意一天（产假/婚假等）。用于区分「带薪假（仍参与排班）」与
// 「长期停职（整月排除）」两种 on_leave 语义。
func hasLockedLeaveInRange(byUser map[uint][]models.ShiftRequest, uid uint, first, last time.Time) bool {
	for _, r := range byUser[uid] {
		if r.Type != models.PrefTypeRest || r.Status != models.PrefStatusLocked {
			continue
		}
		for d := dayOf(first); !d.After(last); d = d.AddDate(0, 0, 1) {
			if requestCovers(r, d) {
				return true
			}
		}
	}
	return false
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

// loadSpecialRestDays 载入区间内该部门（含全局 dept_id=0）的特殊休息日。
func loadSpecialRestDays(deptID uint, from, to string) map[string]string {
	var list []models.SpecialRestDay
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
	Rest     map[string]string // 特殊休息日 date→事由
	ReqByUser map[uint][]models.ShiftRequest
	// CarryWork：姓名 → 截至上月末的「连续上班天数」。
	// 由上月已发布班表（schedules）回溯得出，用于规则9 跨月衔接：
	// 若 carryWork >= MaxWorkStreak，则本月月初必须安排休息。
	// 仅当 Rule.CarryOverPrevMonth 开启时才有值（否则为空 map）。
	CarryWork map[string]int
	// carryHint：姓名 → 本月「第几天起必须已休过」（1-based）。
	// 例如 carryWork=5、上限=6 时，则该人本月最多再连上 1 天，
	// 即第 2 天必须休息 → hint[姓名]=2。
	carryHint map[string]int
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

	people := loadPlanPeople(deptID, userIDs, byUser, first, last)
	carryWork := map[string]int{}
	carryHint := map[string]int{}
	if rule.CarryOverEnabled() {
		carryWork = loadCarryOverWork(deptID, year, month, rule.MaxWorkStreak, people)
		assignCarryHints(carryWork, carryHint, rule.MaxWorkStreak, people)
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
		People:    people,
		Special:   loadSpecialDays(deptID, dateKey(first), dateKey(last)),
		Rest:     loadSpecialRestDays(deptID, dateKey(first), dateKey(last)),
		ReqByUser: byUser,
		CarryWork: carryWork,
		carryHint: carryHint,
	}
}

// assignCarryHints 由「跨月连上班数」推导每人的月初强制休假日：carryHint[姓名] = 第几天须休。
//
// 基础换算：还能再连上 (maxWork - carryWork) 天，再下一天就超限，故 hint = 余量 + 1。
//
// 【负载均衡】只做基础换算是不够的。现实数据（线上 2026-10 副本实测）里，
// 上月末往往是「整组人同进同出」——因为排班在月初统一铺休息、月末统一收，
// 一个月末的连上班数 × 参与人数 = 40~70 人日，而一天最多只能安排
// (倒班人数 - 每班最少人数 × 班次数) 人休息（4 人 4 班每班 2 人时，一天只能休 0 人！）。
// 若按基础换算把所有到限的人都顶在月初同一两天，会算出「一天要休 5~6 人」的
// 数学上不可能满足的班表，反而比不衔接更差。
//
// 因此这里把「必须休」摊到月初的一段窗口里，逐日排空：
//
//	· 到限的人（余量 0）优先往前排，否则他第一天就会被记违规；
//	· 每天最多安排 dailyCap 人，dailyCap 保守取「倒班人数 / 6」（最坏情况每人
//	  连上上限 6 天，一天安排倒班人数的 1/6 休息恰好匹配周转速度）；
//	  每班最少人数 > 0 时再按「一天至多能休几人」收紧。
//
// 这样：人数充裕时等价于「月初尽早休」，人数紧张时自然退化，
// 在数学上无解的情况下把冲突从「一天挤 6 人」摊成「几天内轮休」。
func assignCarryHints(carryWork, carryHint map[string]int, maxWorkStreak int, people []PlanPerson) {
	if maxWorkStreak <= 0 || len(people) == 0 {
		return
	}
	// 按「余量」升序：余量越小（越早到限）越先安排。
	type item struct {
		name  string
		usage int // 本月还能再连上的天数
	}
	items := make([]item, 0, len(carryWork))
	for _, p := range people {
		cw, ok := carryWork[p.Name]
		if !ok || cw <= 0 {
			continue
		}
		usage := maxWorkStreak - cw
		if usage < 0 {
			usage = 0
		}
		items = append(items, item{name: p.Name, usage: usage})
	}
	if len(items) == 0 {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].usage != items[j].usage {
			return items[i].usage < items[j].usage
		}
		return items[i].name < items[j].name
	})

	// 单日最多安排多少人「跨月断休」
	dailyCap := len(people) / 6
	if dailyCap < 1 {
		dailyCap = 1
	}
	if len(people) > 0 {
		// 每人最多连上 maxWorkStreak 天，则每天需要 1/maxWorkStreak 的人休息；
		// 取 6 与 maxWorkStreak 的较小者更保守（月内连上上限越小，周转越快）。
		cap2 := len(people) / maxWorkStreak
		if cap2 < 1 {
			cap2 = 1
		}
		if cap2 < dailyCap {
			dailyCap = cap2
		}
	}

	usedPerDay := map[int]int{}
	for _, it := range items {
		// 从「第 usage+1 天」开始找一个还有名额的窗口日；
		// 越晚则离到限越近，越应优先，故仍按 usage 升序分配、逐日填满后再顺延。
		day := it.usage + 1
		for usedPerDay[day] >= dailyCap {
			day++
		}
		usedPerDay[day]++
		carryHint[it.name] = day
	}
}

// loadCarryOverWork 读取【上月已发布班表】，回溯每人截至上月末的连续上班天数。
//
// 数据来源：schedules 表（已发布班表）。取上月最后 lookback 天用于回溯，
// lookback 取 maxWorkStreak+1（够判定是否已达上限），至少 10 天。
// 未发布过上月班表 → 视为 0（无衔接），不报错、不影响生成。
func loadCarryOverWork(deptID uint, year, month, maxWorkStreak int, people []PlanPerson) map[string]int {
	out := map[string]int{}
	if maxWorkStreak <= 0 {
		return out
	}
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	prevLast := first.AddDate(0, 0, -1) // 上月最后一天
	lookback := maxWorkStreak + 1
	if lookback < 10 {
		lookback = 10
	}
	prevFrom := prevLast.AddDate(0, 0, -(lookback - 1))

	var rows []models.Schedule
	q := db.DB.Where("date >= ? AND date <= ?", dateKey(prevFrom), dateKey(prevLast))
	if deptID > 0 {
		q = q.Where("dept_id IN ?", descendantDeptIDs(deptID))
	}
	q.Find(&rows)
	if len(rows) == 0 {
		return out // 上月未发布，无衔接
	}

	// 每人每天的班次（含休息标记）
	// schedules 只记录「上班」的班次；未出现的日期视为休息。
	dayShift := map[string]map[string]string{} // name → date → shift
	for _, r := range rows {
		var names []string
		if err := json.Unmarshal([]byte(r.People), &names); err != nil {
			continue
		}
		for _, nm := range names {
			if dayShift[nm] == nil {
				dayShift[nm] = map[string]string{}
			}
			dayShift[nm][r.Date] = r.Shift
		}
	}

	for _, p := range people {
		m := dayShift[p.Name]
		if m == nil {
			continue
		}
		cnt := 0
		// 从月末往前数连续「有班次」的天数
		for d := prevLast; !d.Before(prevFrom); d = d.AddDate(0, 0, -1) {
			if _, ok := m[dateKey(d)]; ok {
				cnt++
			} else {
				break // 遇到休息即中断
			}
		}
		if cnt > 0 {
			out[p.Name] = cnt
		}
	}
	return out
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

func (pi *PlanInfo) isSpecialRestDay(day time.Time) bool {
	_, ok := pi.Rest[dateKey(day)]
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
