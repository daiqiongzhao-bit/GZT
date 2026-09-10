package handlers

import (
	"fmt"
	"sort"
	"time"

	"shiftworkbench/internal/models"
)

// ============ 整月自动排班生成器 ============
//
// 算法总览（严格按约束强度分阶段，前者为后者的硬前提）：
//
//   阶段0  可行性预检：倒班人数 ÷ 班次数 ≥ 每班最少人数，否则直接报错
//   阶段1  铺硬约束：固定班次人员、已锁定需求（休假/上班）、特殊工作日
//   阶段2  排休息：按月出勤目标确定每人应休天数 → 按连续休上限切段铺开
//          （先排休息再填空班次，避免"排完班次找不到连续空档休息"）
//   阶段3  填班次：逐日填补，优先满足每班最少人数，规避连续上班上限
//   阶段4  软约束优化：休假前早班 / 休假后晚班，能改则改
//   阶段5  收尾：enforceLocks 再校验一次，确保没有任何锁定需求被覆盖
//
// 关于【固定班次人员不占用每班最少人数】：
//   阶段3 只把倒班人员计入每班人数；固定班次人员单独排（阶段1），
//   二者互不干扰。校验引擎（规则7）同样只统计倒班人员，
//   因此固定班次人员的存在既不会帮忙凑数，也不会因此产生误报。

// GenResult 生成结果
type GenResult struct {
	Plan       map[string]map[string]string `json:"plan"`
	Violations []Violation                  `json:"violations"`
	Notes      []string                     `json:"notes"`
	Warnings   []string                     `json:"warnings"`
}

// feasible 可行性预检：倒班人数能否满足每班最少人数。
// 固定班次人员不计入可用人力（按业务约定不占用名额）。
func (pi *PlanInfo) feasible() error {
	if pi.Rule.MinPerShift <= 0 {
		return nil
	}
	rotating := 0
	for _, p := range pi.People {
		if !p.IsFixed() {
			rotating++
		}
	}
	// 特殊工作日全员上班，但固定班次人员仍不计入倒班班次人数
	need := pi.Rule.MinPerShift * len(pi.Shifts)
	if rotating < need {
		return fmt.Errorf(
			"当前部门倒班人员 %d 人，%d 个班次每班至少 %d 人需 %d 人，人手不足；"+
				"请增加倒班人员、减少班次或调低每班最少人数（固定班次人员不计入该名额）",
			rotating, len(pi.Shifts), pi.Rule.MinPerShift, need)
	}
	return nil
}

// GeneratePlan 生成整月排班计划。
func (pi *PlanInfo) GeneratePlan() (*GenResult, error) {
	if len(pi.People) == 0 {
		return nil, fmt.Errorf("该部门没有可排班人员（已排除冻结与休假中人员）")
	}
	if len(pi.Shifts) == 0 {
		return nil, fmt.Errorf("该部门没有可排班次，请先在「班次设置」中配置")
	}
	if err := pi.feasible(); err != nil {
		return nil, err
	}

	res := &GenResult{Plan: map[string]map[string]string{}}
	plan := res.Plan
	days := daysInRange(pi.First, pi.Last)
	// forcedWork: 特殊工作日要求上班的名单（date → []姓名），阶段2 不得将其排为休息
	forcedWork := map[string][]string{}

	// 分类人员
	var fixed, rotating []PlanPerson
	for _, p := range pi.People {
		if p.IsFixed() {
			fixed = append(fixed, p)
		} else {
			rotating = append(rotating, p)
		}
	}

	// —— 阶段1a：固定班次人员（规则1）——
	// 固定日上固定班次，非固定日休息。不参与轮转，不占用人数名额。
	for _, p := range fixed {
		for _, d := range days {
			k := dateKey(d)
			if p.fixedOn(d) {
				setPlan(plan, k, p.Name, p.FixedShift)
			} else {
				setPlan(plan, k, p.Name, RestShift)
			}
		}
	}

	// —— 阶段1b：已锁定需求（规则4）——
	// 休假锁定 → 当天休息（后续阶段不得覆盖）
	for _, p := range rotating {
		for _, r := range pi.ReqByUser[p.UserID] {
			if r.Status != models.PrefStatusLocked || r.Type != models.PrefTypeRest {
				continue
			}
			for _, d := range days {
				if requestCovers(r, d) {
					setPlan(plan, dateKey(d), p.Name, RestShift)
				}
			}
		}
	}

	// —— 阶段1c：特殊工作日（规则6）——
	// 全员上班：把当天处于休息的倒班人员记入「已定待排班」集合。
	// 用独立集合而非空串占位，避免与阶段2 的"未定"状态混淆。
	for _, p := range rotating {
		for _, d := range days {
			if !pi.isSpecialWorkDay(d) {
				continue
			}
			k := dateKey(d)
			lockedRest := false
			for _, r := range pi.ReqByUser[p.UserID] {
				if r.Status == models.PrefStatusLocked && r.Type == models.PrefTypeRest && requestCovers(r, d) {
					lockedRest = true
					break
				}
			}
			if lockedRest {
				continue // 员工已锁定休假，需求优先
			}
			if isRestShift(lookPlan(plan, k, p.Name)) {
				forcedWork[k] = append(forcedWork[k], p.Name)
				res.Notes = append(res.Notes,
					fmt.Sprintf("%s 为特殊工作日（%s），%s 已自动安排上班", k, pi.Special[k], p.Name))
			}
		}
	}

	// —— 阶段2：排休息（规则2 + 规则3）——
	// 目标：每人休息天数 = 月天数 - 月出勤目标（不低于连续休上限约束所需）
	totalDays := len(days)
	targetWork := pi.Rule.MonthWorkDays
	if targetWork > totalDays {
		targetWork = totalDays
	}
	targetRest := totalDays - targetWork
	if targetRest < 0 {
		targetRest = 0
	}

	for _, p := range rotating {
		pi.assignRestDays(plan, p, days, targetRest, forcedWork)
	}

	// —— 阶段3：填班次（规则7 每班最少人数，仅统计倒班人员）——
	if err := pi.fillShifts(plan, rotating, days, res); err != nil {
		return nil, err
	}

	// —— 阶段4：软约束优化（规则5）——
	pi.applyAdjacencyRules(plan, rotating, days, res)

	// —— 阶段5：收尾 ——
	// 兜底清理：任何仍未被 resolve 的占位都归为休息，绝不让内部哨兵值外泄到班表。
	for _, d := range days {
		k := dateKey(d)
		for _, p := range pi.People {
			if isPendingShift(lookPlan(plan, k, p.Name)) {
				setPlan(plan, k, p.Name, RestShift)
			}
		}
	}

	// 强制还原锁定需求（规则4 兜底）
	res.Notes = append(res.Notes, pi.enforceLocks(plan)...)

	// 校验
	res.Violations = pi.validatePlan(plan)
	return res, nil
}

// splitRestStreaks 把 total 天休息切成若干段，每段长度 <= maxRest。
//
// 设计意图：maxRest 是「连续休息上限」，不是「每次都必须休满」。
// 真实排班中，休息应以单休、双休为主，偶有 3 连休，而不是清一色 3 连休。
//
// 生成策略：
//  1. 先按 maxRest 算出最少段数 minSegs = ceil(total/maxRest)，
//     再决定段数：倾向于把段数放大到 1.2~1.6 倍，让单/双休有机会出现，
//     但不超过 total（否则每段不足 1 天）。
//  2. 每段长度在 [1, maxRest] 内取值，权重偏向 1 和 2（合计约 75%），
//     3 连休占比约 25%——既满足"最高 3 连休"，又不会总是休 3 天。
//  3. 用 userID+year+month 作为种子，保证同一人同一月结果稳定可复现。
func splitRestStreaks(total, maxRest int, userID uint, year, month int) []int {
	if total <= 0 {
		return nil
	}
	if maxRest < 1 {
		maxRest = 1
	}
	if maxRest == 1 || total <= 1 {
		// 上限为 1 时，只能整月单休
		out := make([]int, total)
		for i := range out {
			out[i] = 1
		}
		return out
	}

	// 确定性伪随机（不引入 math/rand，避免全局种子污染测试）
	seed := uint64(userID)*1000003 + uint64(year)*10007 + uint64(month)*211 + 8848897
	next := func() float64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return float64(seed%1000000) / 1000000.0
	}

	// 段数上限：不超过 total（保证每段至少 1 天）
	maxSegs := total
	if maxSegs > 12 {
		maxSegs = 12 // 一个月内休息段数上限，避免过于零碎
	}
	minSegs := (total + maxRest - 1) / maxRest
	if minSegs < 1 {
		minSegs = 1
	}
	if minSegs > maxSegs {
		minSegs = maxSegs
	}

	// 目标段数：在 [minSegs, maxSegs] 中偏向 minSegs 略高一点
	wantSegs := minSegs
	if maxSegs > minSegs {
		extra := maxSegs - minSegs
		// 放大 0~0.5 倍，且至少给 1 段机会（让单/双休能出现）
		add := int(float64(extra) * (0.25 + next()*0.25))
		if add < 1 && extra >= 1 {
			add = 1
		}
		if add > extra {
			add = extra
		}
		wantSegs = minSegs + add
	}

	// 按权重分配每段长度：1→35%, 2→40%, 3→25%（上限为 2 时 1→45%,2→55%）
	segs := make([]int, wantSegs)
	remain := total
	for i := range segs {
		left := len(segs) - i
		// 剩余天数必须在 [left*1, left*maxRest] 内
		lo := 1
		if remain-(left-1)*maxRest > lo {
			lo = remain - (left-1)*maxRest
		}
		hi := maxRest
		if remain-(left-1)*1 < hi {
			hi = remain - (left - 1)
		}
		if hi > remain {
			hi = remain
		}
		if lo > hi {
			lo = hi
		}

		r := next()
		var pick int
		if maxRest >= 3 {
			switch {
			case r < 0.35:
				pick = 1
			case r < 0.75:
				pick = 2
			default:
				pick = 3
			}
		} else { // maxRest == 2
			if r < 0.45 {
				pick = 1
			} else {
				pick = 2
			}
		}
		if pick < lo {
			pick = lo
		}
		if pick > hi {
			pick = hi
		}
		segs[i] = pick
		remain -= pick
	}
	// 兜底：若仍有剩余（理论上不会），追加到末段以前的可行位置
	for remain > 0 {
		done := false
		for i := range segs {
			if remain == 0 {
				break
			}
			if segs[i] < maxRest {
				segs[i]++
				remain--
				done = true
			}
		}
		if !done {
			break
		}
	}
	return segs
}

// splitDrift 为某人的休息段起点生成一组确定性偏移量（错峰）。
//
// 目的：避免同部门所有人都在同一天休息。偏移量按 userID 派生，
// 因此同一人同一月结果稳定；不同人之间互不相同。
// 偏移幅度限制在单段平均间距的 ±1/3 以内，防止把休息段挤到一起。
func splitDrift(userID uint, year, month, segCount, freeLen int) []int {
	drift := make([]int, segCount)
	if segCount == 0 {
		return drift
	}
	avgGap := freeLen / segCount
	if avgGap < 2 {
		return drift // 空间太紧，不扰动，优先保证能放下
	}
	ampl := avgGap / 3
	if ampl < 1 {
		ampl = 1
	}

	seed := uint64(userID)*2654435761 + uint64(year)*40503 + uint64(month)*97 + 12345
	next := func() int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % uint64(2*ampl+1))
	}
	for i := range drift {
		// 首段不扰动（保持月初锚点），其余段在 ±ampl 内浮动
		if i == 0 {
			drift[i] = 0
			continue
		}
		drift[i] = next() - ampl
	}
	return drift
}

// mustWorkOn 判断某倒班人员在某天是否「必须上班」：
// 特殊工作日（规则6）或 已锁定的上班需求（规则4）。
func (pi *PlanInfo) mustWorkOn(p PlanPerson, day time.Time) bool {
	if pi.isSpecialWorkDay(day) {
		return true
	}
	for _, r := range pi.ReqByUser[p.UserID] {
		if r.Type == models.PrefTypeWork && r.Status == models.PrefStatusLocked && requestCovers(r, day) {
			return true
		}
	}
	return false
}

// assignRestDays 为某人铺开休息日（规则2 连续休上限 + 规则3 月出勤目标）。
//
// 策略：把应休天数切成若干段，每段长度不超过 maxRest，
// 段与段之间至少间隔 1 个工作日；同时避开已锁定的上班需求。
// 休息段的起始日尽量均匀分布，避免全挤在月初/月末。
func (pi *PlanInfo) assignRestDays(plan map[string]map[string]string, p PlanPerson, days []time.Time, targetRest int, forcedWork map[string][]string) {
	maxRest := pi.Rule.MaxRestStreak
	if maxRest <= 0 {
		maxRest = 3
	}
	if targetRest <= 0 {
		return
	}

	// 已确定休息 / 已确定上班（更早阶段决定，本函数不得推翻）
	rest := map[string]bool{}
	blocked := map[string]bool{}

	// 1) 尊重更早阶段（锁定需求）已写入的计划。
	//    注意：必须用「键是否存在」判断，不能用 lookPlan 的返回值——
	//    无记录与休息都返回空串，混为一谈会让每一天都被当成休息。
	for _, d := range days {
		k := dateKey(d)
		m, ok := plan[k]
		if !ok {
			continue
		}
		cur, exists := m[p.Name]
		if !exists {
			continue
		}
		if isRestShift(cur) {
			rest[k] = true
		} else if !isPendingShift(cur) {
			blocked[k] = true // 已指定具体班次 → 保持不动
		}
	}
	// 2) 锁定上班需求 → 不可休息
	for _, r := range pi.ReqByUser[p.UserID] {
		if r.Type != models.PrefTypeWork || r.Status != models.PrefStatusLocked {
			continue
		}
		for _, d := range days {
			if requestCovers(r, d) {
				blocked[dateKey(d)] = true
			}
		}
	}
	// 3) 特殊工作日强制上班（规则6）→ 不可休息
	for k, names := range forcedWork {
		for _, n := range names {
			if n == p.Name {
				blocked[k] = true
			}
		}
	}

	need := targetRest - len(rest)
	if need > 0 {
		// 切成不超过 maxRest 的段。
		//
		// 关键点：maxRest 是【上限】而非【固定值】。若每次都用满上限，
		// 8 天休息会被机械切成 [3,3,2]，全员一模一样，既不真实也不人性化。
		// 这里改为混合长度：以单休(1)/双休(2)为主、偶尔出现 3 连休，
		// 且用确定性种子打散，保证同一(人,月)每次生成结果一致（可复现）。
		segLens := splitRestStreaks(need, maxRest, p.UserID, pi.Year, pi.Month)

		// 可自由安排的天（未被更早阶段占用）
		var free []time.Time
		for _, d := range days {
			k := dateKey(d)
			if rest[k] || blocked[k] {
				continue
			}
			free = append(free, d)
		}

		if len(free) > 0 {
			// 段间保留 1 天上班日，避免休息段连成一片突破连续休上限
			needSlots := 0
			for i, n := range segLens {
				needSlots += n
				if i < len(segLens)-1 {
					needSlots++
				}
			}
			if needSlots <= len(free) {
				// 均匀铺开 + 每人错位扰动。
				//
				// 若不扰动，所有人的休息段起点都落在相同的等分位置，
				// 结果就是"全员同一天休息"——既不真实，也会让当天在岗
				// 人数骤降触发每班人数违规。这里给每个人一个稳定的
				// 起始偏移，把休息日错峰打散。
				segCount := len(segLens)
				gap := float64(len(free)) / float64(segCount)

				// 确定性漂移：同一人同一月固定，不同人之间分布不同
				drift := splitDrift(p.UserID, pi.Year, pi.Month, segCount, len(free))

				cursor := 0
				for i, n := range segLens {
					start := int(float64(i)*gap) + drift[i]
					if start < cursor {
						start = cursor
					}
					if start+n > len(free) {
						start = len(free) - n
					}
					if start < cursor {
						start = cursor
					}
					for j := 0; j < n && start+j < len(free); j++ {
						rest[dateKey(free[start+j])] = true
					}
					cursor = start + n + 1
				}
			} else {
				// 空间不足：尽力铺（后续校验会报告违规）
				si := 0
				for _, n := range segLens {
					for j := 0; j < n && si < len(free); j++ {
						rest[dateKey(free[si])] = true
						si++
					}
					si++
				}
			}
		}
	}

	// 落库：休息 → RestShift；其余 → PendingShift 占位，待 fillShifts 填班次。
	// 必须写显式占位（而非空串）：空串与"无记录"无法区分，会让后续阶段
	// 把「已定要上班」误当成休息。
	for _, d := range days {
		k := dateKey(d)
		cur := lookPlan(plan, k, p.Name)
		// blocked 优先：锁定上班需求 / 特殊工作日（规则6）要求必须上班，
		// 即使休息段算法把它算进休息区间，也不得覆盖。
		if blocked[k] {
			if isRestShift(cur) {
				setPlan(plan, k, p.Name, PendingShift)
			}
			continue
		}
		if rest[k] {
			setPlan(plan, k, p.Name, RestShift)
			continue
		}
		if isRestShift(cur) {
			setPlan(plan, k, p.Name, PendingShift)
		}
	}
}

// fillShifts 为所有已占位（非休息）的倒班人员填入具体班次。
// 规则7 只统计倒班人员；固定班次人员不参与本函数。
func (pi *PlanInfo) fillShifts(plan map[string]map[string]string, rotating []PlanPerson, days []time.Time, res *GenResult) error {
	// 跨天轮转游标：每个员工一个"下一个班次"的偏移，实现轮流倒班
	next := map[uint]int{}
	for i, p := range rotating {
		next[p.UserID] = i % len(pi.Shifts)
	}

	for _, d := range days {
		k := dateKey(d)
		// 当天需要上班的倒班人员。
		// PendingShift 表示「已定要上班、班次待填」；只有 RestShift / 无记录才是休息。
		var working []PlanPerson
		for _, p := range rotating {
			cur := lookPlan(plan, k, p.Name)
			if isRestShift(cur) {
				continue
			}
			working = append(working, p)
		}
		if len(working) == 0 {
			continue
		}

		// 当天各班次已有人数（锁定上班需求可能已指定具体班次）
		count := map[string]int{}
		assigned := map[uint]string{}
		for _, p := range working {
			sh := lookPlan(plan, k, p.Name)
			// 仅统计「已是真实班次」的人；PendingShift 是待填占位，不能计入
			if sh == "" || isPendingShift(sh) {
				continue
			}
			count[sh]++
			assigned[p.UserID] = sh
		}

		// 待分配的人：优先分给人数最少的班次，保证「每班最少人数」
		var pending []PlanPerson
		for _, p := range working {
			if _, ok := assigned[p.UserID]; !ok {
				pending = append(pending, p)
			}
		}
		// 按「当前人数最少的班次」优先级依次分配
		for _, p := range pending {
			// 候选班次按当天已排人数升序、且考虑该员工的下一个轮转班次
			best := pi.pickShiftFor(plan, p, k, d, count, next)
			count[best]++
			assigned[p.UserID] = best
			setPlan(plan, k, p.Name, best)
			// 该员工下一天从下一个班次开始，形成轮转
			next[p.UserID] = (indexOf(pi.Shifts, best) + 1) % len(pi.Shifts)
		}
	}
	return nil
}

// pickShiftFor 为某员工在某天选择班次：
// 优先填补当天人数最少的班次（满足规则7），其次考虑轮转连续性。
func (pi *PlanInfo) pickShiftFor(plan map[string]map[string]string, p PlanPerson, k string, d time.Time, count map[string]int, next map[uint]int) string {
	// 候选：全部班次，按 (当天人数, 是否等于轮转期望) 排序
	type cand struct {
		shift string
		cnt   int
		pref  int // 0 = 正是轮转期望，1 = 其他
	}
	cands := make([]cand, 0, len(pi.Shifts))
	want := pi.Shifts[next[p.UserID]%len(pi.Shifts)]
	for _, sh := range pi.Shifts {
		c := cand{shift: sh, cnt: count[sh]}
		if sh != want {
			c.pref = 1
		}
		cands = append(cands, c)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].cnt != cands[j].cnt {
			return cands[i].cnt < cands[j].cnt
		}
		return cands[i].pref < cands[j].pref
	})
	return cands[0].shift
}

// indexOf 返回班次在池中的下标；不存在返回 0。
func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return 0
}

// applyAdjacencyRules 软约束优化（规则5）：
// 休假前一天尽量排早班、休假后第一天尽量排晚班。
// 只在「不破坏每班最少人数」的前提下做交换，避免按下葫芦浮起瓢。
func (pi *PlanInfo) applyAdjacencyRules(plan map[string]map[string]string, rotating []PlanPerson, days []time.Time, res *GenResult) {
	if pi.Morning == "" || pi.Evening == "" || pi.Morning == pi.Evening {
		return
	}
	wantBefore := pi.Rule.RequireMorningBeforeRest
	wantAfter := pi.Rule.RequireEveningAfterRest
	if !wantBefore && !wantAfter {
		return
	}

	nameSet := map[string]PlanPerson{}
	for _, p := range rotating {
		nameSet[p.Name] = p
	}

	for _, d := range days {
		k := dateKey(d)

		// 统计当天各班次人数（仅倒班人员）
		count := map[string]int{}
		byShift := map[string][]string{}
		for _, p := range rotating {
			sh := lookPlan(plan, k, p.Name)
			if isRestShift(sh) {
				continue
			}
			count[sh]++
			byShift[sh] = append(byShift[sh], p.Name)
		}
		if len(count) == 0 {
			continue
		}

		// 需要调整的人：次日休假 → 今天应早班；昨日休假 → 今天应晚班
		target := map[string]string{} // name → 期望班次
		for _, p := range rotating {
			cur := lookPlan(plan, k, p.Name)
			if isRestShift(cur) {
				continue
			}
			if wantBefore {
				nk := dateKey(d.AddDate(0, 0, 1))
				if !d.AddDate(0, 0, 1).After(pi.Last) && isRestShift(lookPlan(plan, nk, p.Name)) {
					target[p.Name] = pi.Morning
				}
			}
			if wantAfter {
				pk := dateKey(d.AddDate(0, 0, -1))
				if !d.AddDate(0, 0, -1).Before(pi.First) && isRestShift(lookPlan(plan, pk, p.Name)) {
					target[p.Name] = pi.Evening
				}
			}
		}

		// 逐个尝试：直接改（若原班次人数充裕）
		for name, want := range target {
			cur := lookPlan(plan, k, name)
			if cur == want {
				continue
			}
			// 原班次改后仍不少于下限 → 直接改
			if pi.Rule.MinPerShift <= 0 || count[cur]-1 >= pi.Rule.MinPerShift {
				count[cur]--
				count[want]++
				setPlan(plan, k, name, want)
				continue
			}
			// 否则尝试与目标班次的人对调
			swapped := false
			for _, other := range byShift[want] {
				otherCur := lookPlan(plan, k, other)
				if otherCur == want && (pi.Rule.MinPerShift <= 0 || count[otherCur]-1 >= pi.Rule.MinPerShift) && other != name {
					// other 改到 cur，name 改到 want
					setPlan(plan, k, other, cur)
					setPlan(plan, k, name, want)
					swapped = true
					break
				}
			}
			if !swapped {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("%s %s 无法调整为「%s」（会低于每班最少 %d 人），已在违规明细中提示",
						k, name, want, pi.Rule.MinPerShift))
			}
		}
	}
}
