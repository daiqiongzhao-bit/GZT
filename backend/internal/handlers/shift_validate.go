package handlers

import (
	"fmt"
	"time"

	"shiftworkbench/internal/models"
)

// ============ 规则校验引擎 ============

// Violation 违规明细。
type Violation struct {
	Date    string `json:"date"`               // 起始日期
	EndDate string `json:"end_date,omitempty"` // 连续类违规的结束日期
	Person  string `json:"person"`             // 员工姓名
	Rule    string `json:"rule"`               // 规则标识
	Label   string `json:"label"`              // 规则中文名
	Reason  string `json:"reason"`             // 具体原因
	Level   string `json:"level"`              // error 违规 / warn 提醒
}

// Streak 连续段。
type Streak struct {
	Start   time.Time
	End     time.Time
	N       int
	Partial bool // 段起点延续自区间之前：跨度无法完整观测，判上限时跳过
}

// collectStreaks 统计区间内满足 pred 的连续段。
// prevOK 用于判断段起点前一天是否也满足（是 → 该段延续自区间外，标记 Partial）。
func collectStreaks(from, to time.Time, pred func(time.Time) bool, prevOK func(time.Time) bool) []Streak {
	var out []Streak
	var cur *Streak
	for d := dayOf(from); !d.After(to); d = d.AddDate(0, 0, 1) {
		if pred(d) {
			if cur == nil {
				// Partial 表示该段「延续自区间之前」：由调用方的 prevOK
				// 依据真实观测（含区间外日期）判定，而非猜测。
				cur = &Streak{Start: d, End: d, N: 1, Partial: prevOK(d.AddDate(0, 0, -1))}
			} else {
				cur.End = d
				cur.N++
			}
		} else if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// validatePlan 校验整月计划，返回违规明细。
//
// 关键业务约定：**固定班次人员不占用每班最少人数名额**——
// 规则7 只统计「倒班人员」的到岗数，固定班次人员即使当天在岗也不计入。
func (pi *PlanInfo) validatePlan(plan map[string]map[string]string) []Violation {
	var vs []Violation
	from, to := pi.First, pi.Last
	maxRest, maxWork := pi.Rule.MaxRestStreak, pi.Rule.MaxWorkStreak

	// 按人建立索引加速
	type pIdx struct {
		name  string
		fixed bool // 固定班次人员：不参与倒班，连续上/休上限对其不适用
		rest  map[string]bool
		work  map[string]string
	}
	idxes := make([]pIdx, 0, len(pi.People))
	nameToUID := map[string]uint{}
	for _, p := range pi.People {
		ix := pIdx{name: p.Name, fixed: p.IsFixed(), rest: map[string]bool{}, work: map[string]string{}}
		for _, d := range daysInRange(from, to) {
			k := dateKey(d)
			sh := lookPlan(plan, k, p.Name)
			if isRestShift(sh) {
				ix.rest[k] = true
			} else {
				ix.work[k] = sh
			}
		}
		idxes = append(idxes, ix)
		nameToUID[p.Name] = p.UserID
	}

	for _, ix := range idxes {
		// 谓词必须把区间外的日期判为"不满足"：否则 map 未命中会被当成
		// 「非休息=上班」，使跨月边界被误判为 Partial（段延续自区间外）而漏报。
		// isRest 对区间外的日期也给出真实判断：计划表没有记录即视为休息。
		// 这样「跨月延续的休息段」能被正确标记为 Partial（跳过），
		// 而「月初开始、上月最后一天在上班」的上班段则不会被误标。
		isRest := func(d time.Time) bool {
			k := dateKey(d)
			if k < dateKey(from) || k > dateKey(to) {
				return true // 区间外无记录 → 按休息处理
			}
			return ix.rest[k]
		}
		isWork := func(d time.Time) bool {
			k := dateKey(d)
			if k < dateKey(from) || k > dateKey(to) {
				return false
			}
			return !ix.rest[k]
		}

		// —— 规则2a：最高连续休息天数 ——
		// 固定班次人员不参与倒班（规则1），其作息由固定班次决定，
		// 连续上/休上限不适用；否则会因"每天固定上班"被误报超限。
		if maxRest > 0 && !ix.fixed {
			for _, s := range collectStreaks(from, to, isRest, isRest) {
				if s.Partial || s.N <= maxRest {
					continue
				}
				vs = append(vs, Violation{
					Date: dateKey(s.Start), EndDate: dateKey(s.End), Person: ix.name,
					Rule: "max_rest_streak", Label: "连续休息超限", Level: "error",
					Reason: fmt.Sprintf("连续休息 %d 天，超过上限 %d 天", s.N, maxRest),
				})
			}
		}

		// —— 规则2b：最高连续上班天数 ——
		if maxWork > 0 && !ix.fixed {
			for _, s := range collectStreaks(from, to, isWork, isWork) {
				if s.Partial || s.N <= maxWork {
					continue
				}
				vs = append(vs, Violation{
					Date: dateKey(s.Start), EndDate: dateKey(s.End), Person: ix.name,
					Rule: "max_work_streak", Label: "连续上班超限", Level: "error",
					Reason: fmt.Sprintf("连续上班 %d 天，超过上限 %d 天", s.N, maxWork),
				})
			}
		}

		// —— 规则5：休假前早班 / 休假后晚班 ——
		// 仅当部门配了早/晚代表班次且二者不同才有意义
		if pi.Morning != "" && pi.Evening != "" && pi.Morning != pi.Evening && !ix.fixed {
			for _, d := range daysInRange(from, to) {
				k := dateKey(d)
				if !ix.rest[k] {
					continue
				}
				// 休假开始日的前一天若上班 → 应为早班
				if pi.Rule.RequireMorningBeforeRest {
					prev := d.AddDate(0, 0, -1)
					pk := dateKey(prev)
					if !prev.Before(dayOf(from)) && !ix.rest[pk] {
						if ix.work[pk] != pi.Morning {
							vs = append(vs, Violation{
								Date: pk, Person: ix.name,
								Rule: "morning_before_rest", Label: "休假前须早班", Level: "warn",
								Reason: fmt.Sprintf("次日（%s）起休假，%s 应排「%s」，实际为「%s」",
									k, pk, pi.Morning, ix.work[pk]),
							})
						}
					}
				}
				// 休假结束次日若上班 → 应为晚班
				if pi.Rule.RequireEveningAfterRest {
					next := d.AddDate(0, 0, 1)
					nk := dateKey(next)
					if !next.After(to) && !ix.rest[nk] {
						if ix.work[nk] != pi.Evening {
							vs = append(vs, Violation{
								Date: nk, Person: ix.name,
								Rule: "evening_after_rest", Label: "休假后须晚班", Level: "warn",
								Reason: fmt.Sprintf("前一日（%s）为休假，%s 应排「%s」，实际为「%s」",
									k, nk, pi.Evening, ix.work[nk]),
							})
						}
					}
				}
			}
		}

		// —— 规则3：月度出勤天数 ——
		// 同理，固定班次人员的出勤天数由其固定班次与生效星期决定，不按月目标考核。
		// 产假/婚假等已锁定休假不计入出勤目标：该人只需在「可用天数」内排满，
		// 否则会对其误报「出勤天数不足」（休了 11 天产假却要求上满 22 天本就不合理）。
		if pi.Rule.MonthWorkDays > 0 && !ix.fixed {
			cnt := 0
			for _, d := range daysInRange(from, to) {
				if !ix.rest[dateKey(d)] {
					cnt++
				}
			}
			leaveDays := 0
			if uid, ok := nameToUID[ix.name]; ok {
				leaveDays = pi.leaveDaysInMonth(uid)
			}
			effTarget := pi.Rule.MonthWorkDays - leaveDays
			if effTarget < 0 {
				effTarget = 0
			}
			if cnt < effTarget {
				vs = append(vs, Violation{
					Date: dateKey(from), EndDate: dateKey(to), Person: ix.name,
					Rule: "month_work_days", Label: "出勤天数不足", Level: "warn",
					Reason: fmt.Sprintf("本月排班 %d 天，少于目标 %d 天（已扣除 %d 天产假/婚假等休假）", cnt, effTarget, leaveDays),
				})
			} else if cnt > pi.Rule.MonthWorkDays && !pi.Rule.AllowExceedMonthDays {
				vs = append(vs, Violation{
					Date: dateKey(from), EndDate: dateKey(to), Person: ix.name,
					Rule: "month_work_days", Label: "出勤天数超额", Level: "warn",
					Reason: fmt.Sprintf("本月排班 %d 天，超过目标 %d 天", cnt, pi.Rule.MonthWorkDays),
				})
			}
		}
	}

	// —— 规则7：每班次最少人数（固定班次人员不计入）——
	if pi.Rule.MinPerShift > 0 {
		rotating := map[string]bool{}
		for _, p := range pi.People {
			if !p.IsFixed() {
				rotating[p.Name] = true
			}
		}
		for _, d := range daysInRange(from, to) {
			k := dateKey(d)
			// 只统计倒班人员在每个班次的到岗数
			count := map[string]int{}
			for name := range rotating {
				sh := lookPlan(plan, k, name)
				if isRestShift(sh) {
					continue
				}
				count[sh]++
			}
			for _, sh := range pi.Shifts {
				if count[sh] < pi.Rule.MinPerShift {
					vs = append(vs, Violation{
						Date: k, Person: fmt.Sprintf("%s 班次", sh),
						Rule: "min_per_shift", Label: "班次人数不足", Level: "error",
						Reason: fmt.Sprintf("%s「%s」仅 %d 人（倒班人员），少于最少 %d 人（固定班次人员不计入）",
							k, sh, count[sh], pi.Rule.MinPerShift),
					})
				}
			}
		}
	}

	// —— 规则6：特殊工作日未安排（仅提示）——
	if len(pi.Special) > 0 {
		for _, d := range daysInRange(from, to) {
			k := dateKey(d)
			reason, ok := pi.Special[k]
			if !ok {
				continue
			}
			for _, p := range pi.People {
				if isRestShift(lookPlan(plan, k, p.Name)) {
					vs = append(vs, Violation{
						Date: k, Person: p.Name,
						Rule: "special_workday", Label: "特殊工作日未上班", Level: "warn",
						Reason: fmt.Sprintf("%s 为特殊工作日（%s）要求全员上班，但未安排班次", k, reason),
					})
				}
			}
		}
	}

	// —— 规则4：锁定需求被违反 ——
	for _, p := range pi.People {
		for _, r := range pi.ReqByUser[p.UserID] {
			if r.Type != models.PrefTypeRest {
				continue
			}
			for _, d := range daysInRange(from, to) {
				if !requestCovers(r, d) {
					continue
				}
				k := dateKey(d)
				if !isRestShift(lookPlan(plan, k, p.Name)) {
					vs = append(vs, Violation{
						Date: k, Person: p.Name,
						Rule: "locked_request", Label: "违反已锁定需求", Level: "error",
						Reason: fmt.Sprintf("%s 已提交并锁定休假需求，但仍被排班", k),
					})
				}
			}
		}
	}

	// 排序：日期 → 人员 → 规则，便于阅读
	sortViolations(vs)
	return vs
}

// sortViolations 稳定排序违规明细。
func sortViolations(vs []Violation) {
	// 简单插入排序即可（违规数量级很小），避免引入额外依赖
	for i := 1; i < len(vs); i++ {
		cur := vs[i]
		j := i - 1
		for j >= 0 && lessViolation(cur, vs[j]) {
			vs[j+1] = vs[j]
			j--
		}
		vs[j+1] = cur
	}
}

func lessViolation(a, b Violation) bool {
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	if a.Person != b.Person {
		return a.Person < b.Person
	}
	return a.Rule < b.Rule
}

// enforceLocks 强制还原所有已锁定需求（规则4）：
// 已锁定休假 → 当天置为休息；已锁定上班 → 保留其班次不覆盖。
// 返回被强制还原的明细，供界面提示。任何写入路径都必须经过本函数。
func (pi *PlanInfo) enforceLocks(plan map[string]map[string]string) []string {
	var notes []string
	for _, p := range pi.People {
		for _, r := range pi.ReqByUser[p.UserID] {
			if r.Status != models.PrefStatusLocked {
				continue
			}
			for _, d := range daysInRange(pi.First, pi.Last) {
				if !requestCovers(r, d) {
					continue
				}
				k := dateKey(d)
				if r.Type == models.PrefTypeRest {
					if !isRestShift(lookPlan(plan, k, p.Name)) {
						notes = append(notes, fmt.Sprintf("%s %s 已锁定休假，已还原为休息", k, p.Name))
					}
					setPlan(plan, k, p.Name, RestShift)
				}
			}
		}
	}
	return notes
}
