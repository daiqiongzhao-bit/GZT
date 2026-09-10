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

	// 每日休息人数计数器（跨人员共享）。
	//
	// 为什么需要：仅靠相位错峰，不同人的休息段仍可能压在同一天，
	// 该天在岗人数骤降 → 触发每班最少人数违规。
	// 这里维护「当天已安排多少人休息」，后续人员选休息段位置时
	// 优先挑当天休息人数最少的位置，从而压平在岗人数曲线。
	restPerDay := map[string]int{}
	for _, p := range rotating {
		pi.assignRestDays(plan, p, days, targetRest, forcedWork, restPerDay)
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
// 设计意图：
//   - maxRest 是「连续休息上限」，不是「每次都必须休满」；
//   - 休息应【尽量成双】，即优先排 2 连休（双休），这是最舒服的节奏；
//   - 在双休铺不满的情况下，用 3 连休（不超上限）和单休做调配。
//
// 生成策略（双休优先）：
//  1. 先按「每段 2 天」估算段数 wantSegs = round(total/2)，
//     再夹到 [ceil(total/maxRest), total] 的可行区间内。
//     这样 total=8 时 wantSegs=4，天然就是 2+2+2+2 的双休结构。
//  2. 每段初始为 2 天，剩余天数按权重补给各段：
//     优先补成 3 连休（概率高），少量补成 1 天（单休）。
//     当 maxRest<3 时无法补 3，退化为纯双休。
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

	// ---- 1) 段数：以「每段 2 天」为基准 ----
	minSegs := (total + maxRest - 1) / maxRest // 少于这个段数，某段会超上限
	if minSegs < 1 {
		minSegs = 1
	}
	maxSegs := total / 1 // 每段至少 1 天
	if maxSegs < 1 {
		maxSegs = 1
	}
	// 避免一个月内段数过多（过于零碎）
	if maxSegs > 12 {
		maxSegs = 12
	}
	if minSegs > maxSegs {
		minSegs = maxSegs
	}

	// 双休基准段数：向上取整，余数用单休消化（避免出现半段）
	wantSegs := (total + 1) / 2
	// 小幅抖动 ±0 或 ±1，让不同人节奏略有差异
	if next() < 0.3 && wantSegs+1 <= maxSegs {
		wantSegs++
	} else if next() < 0.3 && wantSegs-1 >= minSegs {
		wantSegs--
	}
	if wantSegs < minSegs {
		wantSegs = minSegs
	}
	if wantSegs > maxSegs {
		wantSegs = maxSegs
	}
	if wantSegs < 1 {
		wantSegs = 1
	}

	// ---- 2) 每段初始 2 天（上限为 2 时直接成立），再补齐差额 ----
	segs := make([]int, wantSegs)
	base := 2
	if maxRest < base {
		base = maxRest
	}
	sum := 0
	for i := range segs {
		segs[i] = base
		sum += base
	}

	// 差额：正数=还需加休，负数=需减休
	diff := total - sum
	// 可加的空间（每段最多到 maxRest）
	for diff > 0 {
		moved := false
		for i := range segs {
			if diff == 0 {
				break
			}
			if segs[i] < maxRest {
				segs[i]++
				diff--
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	// 需减：优先把 2 天的段减成 1 天（即出现单休），且不产生 0
	for diff < 0 {
		moved := false
		for i := range segs {
			if diff == 0 {
				break
			}
			if segs[i] > 1 {
				segs[i]--
				diff++
				moved = true
			}
		}
		if !moved {
			break
		}
	}

	return segs
}

// splitSlotOffsets 为某人的每个槽位生成「槽内偏移量」。
//
// 每个休息段被约束在长度为 slot 的独立槽位内，
// 偏移量决定它在槽内的具体位置。偏移幅度限制在槽长的一半以内，
// 既能错开不同人的休息日，又不会越槽影响相邻段。
//
// 偏移量按【槽位下标】派生（而非段序号），这样当相位把段旋转到
// 别的槽位时，偏移量会跟着槽位走，不会出现「A 槽的偏移被用到 B 槽」。
//
// 按 userID 派生，同一人同一月结果稳定；不同人之间分布不同。
func splitSlotOffsets(userID uint, year, month, segCount, slot int) []int {
	offs := make([]int, segCount)
	if segCount == 0 || slot <= 1 {
		return offs
	}
	// 允许的偏移上限：槽长的一半，至少 1
	ampl := slot / 2
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
	for i := range offs {
		offs[i] = next() - ampl
	}
	return offs
}

// restSegmentIndices 计算某人各休息段在 free 序列中的具体下标。
//
// 这是「等分槽位 + 环状相位错峰」的核心，抽成纯函数便于单测。
//
// 做法：
//  1. 把 free 序列切成 segCount 个「槽」，每段占一个槽。槽长取均值，
//     余数分给靠前的槽（否则末尾槽可能装不下段）。
//  2. 段在槽内浮动，但浮动空间受限：不仅要满足「槽长 - 段长」，
//     还要在段之后预留 1 天「隔离日」，否则本槽的段漂到槽尾、
//     下一槽的段漂到槽头时，两段会贴在一起连成一片，
//     造出超过 maxRest 的超长连休（线上实测出现过 5 连休）。
//  3. 每个人的槽序列整体旋转一个相位（按 userID/年/月派生），
//     使不同人的休息段在月内互相错开，避免同一天集体休息。
//  4. 负载感知（load）：在槽内可浮动范围内，优先选择
//     「当日已休息人数最少」的位置，进一步压平在岗人数曲线。
//     这是解决「某天在岗人数骤降导致每班人数违规」的关键。
//
// freeLen: 可自由安排的「天」的数量
// segLens: 各休息段的长度
// load:    长度应 >= freeLen 的每日负载数组（可为 nil，表示不做负载感知）
func restSegmentIndices(freeLen int, segLens []int, userID uint, year, month int) []int {
	return restSegmentIndicesLoad(freeLen, segLens, userID, year, month, nil)
}

// restSegmentIndicesLoad 是 restSegmentIndices 的完整版，额外接受每日负载。
func restSegmentIndicesLoad(freeLen int, segLens []int, userID uint, year, month int, load []int) []int {
	if freeLen <= 0 || len(segLens) == 0 {
		return nil
	}
	segCount := len(segLens)

	baseSlot := freeLen / segCount
	extra := freeLen % segCount
	if baseSlot < 1 {
		baseSlot = 1
		extra = 0
	}

	// 各槽的 [起点, 长度]，前 extra 个槽各多 1 天
	slotStart := make([]int, segCount)
	slotLen := make([]int, segCount)
	cursor := 0
	for i := 0; i < segCount; i++ {
		slotStart[i] = cursor
		slotLen[i] = baseSlot
		if i < extra {
			slotLen[i]++
		}
		cursor += slotLen[i]
	}

	// 相位：按用户错开整段位置，范围 0 ~ segCount-1。
	//
	// 不能用 (userID+year+month)%segCount：连续 uid 会得到连续相位，
	// 当人多于槽数时相位会重复，导致「几个人同一天开始休息」，
	// 当天在岗人数骤降。这里用乘法散列 + 黄金比，让相邻 uid 的
	// 相位分散到不同槽位。
	phase := int((uint64(userID)*2654435761 + uint64(year)*40503 + uint64(month)*97) % uint64(segCount))
	if phase < 0 {
		phase = 0
	}
	// 槽内偏移：按槽位下标派生（不是段序号），随槽走
	offs := splitSlotOffsets(userID, year, month, segCount, baseSlot)

	hasLoad := len(load) >= freeLen

	var idxs []int
	for i, n := range segLens {
		si := (i + phase) % segCount
		// 段可浮动空间 = 槽长 - 段长 - 1 天隔离日。
		// 槽长不足以容纳「段 + 隔离日」时退化为不允许浮动（room=0），
		// 此时靠相位错峰仍能实现人与人之间的分散。
		room := slotLen[si] - n - 1
		if room < 0 {
			room = 0
		}

		// 候选起点：以种子偏移为中心，向两侧扩展，覆盖整个可浮动范围。
		// 这样既能保留确定性（同一人同一月稳定），又能在候选里挑负载最低的。
		bestStart := slotStart[si]
		bestScore := -1
		for d := 0; d <= room; d++ {
			for _, cand := range []int{offs[i] + d, offs[i] - d} {
				if cand < 0 || cand > room {
					continue
				}
				start := slotStart[si] + cand
				if start+n > freeLen || start < 0 {
					continue
				}
				// 打分：该段覆盖日期的负载之和，越小越好
				score := 0
				if hasLoad {
					for k := 0; k < n; k++ {
						score += load[start+k]
					}
				} else {
					score = abs(cand - offs[i]) // 无负载时退回「离种子偏移最近」
				}
				if bestScore < 0 || score < bestScore {
					bestScore = score
					bestStart = start
				}
			}
			// 有负载时无需穷举全部候选，找到零负载即可停止
			if hasLoad && bestScore == 0 {
				break
			}
		}

		for k := 0; k < n; k++ {
			if p := bestStart + k; p >= 0 && p < freeLen {
				idxs = append(idxs, p)
			}
		}
	}
	sort.Ints(idxs)
	return idxs
}

// abs 返回整数绝对值。
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
// restPerDay 为跨人员共享的「每日休息人数」计数器，用于压平在岗人数曲线。
func (pi *PlanInfo) assignRestDays(plan map[string]map[string]string, p PlanPerson, days []time.Time, targetRest int, forcedWork map[string][]string, restPerDay map[string]int) {
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
				// 「等分槽位 + 环状错峰」放置。
				//
				// 为什么不用简单的等分起点：
				//   若所有人起点都取 i*gap，多个人的休息段会落在同一天，
				//   导致该天在岗人数骤降触发 min_per_shift 违规；
				//   而相邻几天又人满为患——人力被浪费，且连上上限也易破。
				//
				// 做法见 restSegmentIndicesLoad 的注释。
				// 负载数组：free[j] 当天已有多少人休息，用于择优压平在岗曲线。
				load := make([]int, len(free))
				for j, d := range free {
					load[j] = restPerDay[dateKey(d)]
				}
				for _, idx := range restSegmentIndicesLoad(len(free), segLens, p.UserID, pi.Year, pi.Month, load) {
					if idx < 0 || idx >= len(free) {
						continue
					}
					k := dateKey(free[idx])
					rest[k] = true
					restPerDay[k]++
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
//
// 【班次形态约束】（用户明确要求）
// 每个「工作块」（两次休息之间的连续上班日）内，班次只能单向推进一次，
// 严禁来回倒班。合法形态：
//
//	休 中中中 早早早 休
//	休 中 早早早 休
//	休 中中中中 早 休
//
// 非法形态：中 早 中 早（来回倒班）。
//
// 【均衡约束】每人各班次天数尽量平均（如 22 个工作日 → 11 早 + 11 中）。
func (pi *PlanInfo) fillShifts(plan map[string]map[string]string, rotating []PlanPerson, days []time.Time, res *GenResult) error {
	order := pi.blockShiftOrder()
	if len(order) == 0 || len(rotating) == 0 {
		return nil
	}
	n := len(order)

	// 每日各班次人数负载；锁定需求指定的班次先计入，保证后续错峰能参考
	load := make([]map[string]int, len(days))
	for i := range days {
		load[i] = map[string]int{}
		for _, sh := range pi.Shifts {
			load[i][sh] = 0
		}
	}

	// 收集每人的工作块：连续的上班日，被休息日或已锁定班次日分隔。
	//
	// 为什么按「块」而不是逐日排：班次形态是块级约束
	// （块内只能单向切换一次），逐日贪心必然排出来回倒班。
	type blockT struct {
		uid  uint
		name string
		idxs []int // days 中的下标
	}
	var blocks []blockT
	for _, p := range rotating {
		cur := []int{}
		flush := func() {
			if len(cur) > 0 {
				blocks = append(blocks, blockT{p.UserID, p.Name, append([]int{}, cur...)})
				cur = cur[:0]
			}
		}
		for i, d := range days {
			v := lookPlan(plan, dateKey(d), p.Name)
			if isRestShift(v) {
				flush()
				continue
			}
			// 已锁定具体班次（锁定需求）→ 保持不动，计入负载，并打断工作块
			if v != "" && !isPendingShift(v) {
				load[i][v]++
				flush()
				continue
			}
			cur = append(cur, i)
		}
		flush()
	}

	byUID := map[uint][]blockT{}
	for _, b := range blocks {
		byUID[b.uid] = append(byUID[b.uid], b)
	}

	for _, p := range rotating {
		bs := byUID[p.UserID]
		if len(bs) == 0 {
			continue
		}
		// 该人本月总工作日
		total := 0
		for _, b := range bs {
			total += len(b.idxs)
		}
		// 各班次目标配额：尽量均分（22 天 → 11 + 11）
		quota := make([]int, n)
		base := total / n
		extra := total % n
		for i := range quota {
			quota[i] = base
			if i < extra {
				quota[i]++
			}
		}
		// 逐块切分并填写；quota 随填写递减，实现全局均衡
		for _, b := range bs {
			seg := planBlockSegments(len(b.idxs), quota)
			pos := 0
			for si, cnt := range seg {
				for c := 0; c < cnt && pos < len(b.idxs); c++ {
					dayIdx := b.idxs[pos]
					sh := order[si]
					setPlan(plan, dateKey(days[dayIdx]), b.name, sh)
					load[dayIdx][sh]++
					if quota[si] > 0 {
						quota[si]--
					}
					pos++
				}
			}
		}
	}
	return nil
}

// blockShiftOrder 返回工作块内的班次推进顺序（单向、不可逆）。
//
// 休息后第一天上「晚」的班，逐步提前，休假前上「早」的班：
//
//	[中班, 早班] → 块形态「中中中 早早早」
//
// 这与规则5（休假前早班、休假后晚班）天然一致。
func (pi *PlanInfo) blockShiftOrder() []string {
	if len(pi.Shifts) <= 2 && pi.Evening != "" && pi.Morning != "" && pi.Evening != pi.Morning {
		return []string{pi.Evening, pi.Morning}
	}
	// 多班次：按开始时间逆序（最晚 → 最早）单向推进
	out := make([]string, len(pi.Shifts))
	for i, s := range pi.Shifts {
		out[len(pi.Shifts)-1-i] = s
	}
	return out
}

// planBlockSegments 把长度为 L 的工作块按顺序切成 n 段，各段之和 = L。
//
// 段长参考 quota（该人各班次「还应排多少天」）按比例分配，
// 因此逐块消耗 quota 后，全天下来各班次天数自然趋于平均。
// 每段至少 1 天（天数不足时靠后的段可为 0）。
func planBlockSegments(L int, quota []int) []int {
	n := len(quota)
	seg := make([]int, n)
	if L <= 0 || n == 0 {
		return seg
	}
	remain := L
	remainQ := 0
	for _, q := range quota {
		if q > 0 {
			remainQ += q
		}
	}
	for i := 0; i < n; i++ {
		if i == n-1 {
			seg[i] = remain
			break
		}
		want := remain / (n - i)
		if remainQ > 0 && quota[i] > 0 {
			want = (quota[i]*remain + remainQ/2) / remainQ
		}
		// 给后面的段至少留 1 天
		maxTake := remain - (n - 1 - i)
		if maxTake < 1 {
			maxTake = 1
		}
		if want < 1 {
			want = 1
		}
		if want > maxTake {
			want = maxTake
		}
		seg[i] = want
		remain -= want
		if quota[i] > 0 {
			remainQ -= quota[i]
			if remainQ < 0 {
				remainQ = 0
			}
		}
	}
	return seg
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
