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

	// 供给下限：出勤人日必须撑得起「每天每班最少人数」，否则无解。
	//
	// 例：31 天的月份，6 个倒班人员，每班最少 2 人 × 2 个班次 = 每天需 4 人，
	// 全月需 124 人日；而 6 人 × 20 天出勤只有 120 人日 —— 差 4 人日，
	// 无论怎么错峰都必有几天凑不够人（实测 12 月 8 天每班人数不足）。
	// 这里把出勤天数自动抬到刚好够用的下限，并在 warnings 里说明，
	// 而不是硬排出一份必然违规的班表。
	if len(rotating) > 0 && len(pi.Shifts) > 0 && pi.Rule.MinPerShift > 0 {
		perDay := pi.Rule.MinPerShift * len(pi.Shifts)
		minWork := (totalDays*perDay + len(rotating) - 1) / len(rotating)
		if minWork > totalDays {
			minWork = totalDays
		}
		if targetWork < minWork {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"本月 %d 天、倒班 %d 人，每天需 %d 人上班（%d 个班次 × 每班最少 %d 人），"+
					"共需 %d 人日；按当前「月出勤 %d 天」只有 %d 人日，无法排开。"+
					"已自动将出勤天数上调为 %d 天。",
				totalDays, len(rotating), perDay, len(pi.Shifts), pi.Rule.MinPerShift,
				totalDays*perDay, targetWork, targetWork*len(rotating), minWork))
			targetWork = minWork
		}
	}
	targetRest := totalDays - targetWork
	if targetRest < 0 {
		targetRest = 0
	}
	maxWork := pi.Rule.MaxWorkStreak
	if maxWork <= 0 {
		maxWork = 6
	}

	// 每日休息人数计数器（跨人员共享）。
	//
	// 为什么需要：仅靠相位错峰，不同人的休息段仍可能压在同一天，
	// 该天在岗人数骤降 → 触发每班最少人数违规。
	// 这里维护「当天已安排多少人休息」，后续人员选休息段位置时
	// 优先挑当天休息人数最少的位置，从而压平在岗人数曲线。
	restPerDay := map[string]int{}
	for ri, p := range rotating {
		pi.assignRestDays(plan, p, days, targetRest, forcedWork, restPerDay, ri, len(rotating))
		// 连续上班超上限时，拆一个双休成单休来打断长班次。
		//
		// 用户原话：「那些连续上7天的你可以把某个双休改成单休啊，
		// 为什么那么死板？」——与其让人连上 7、8 天，
		// 不如牺牲一个双休，把长班次从中间切开。
		pi.breakLongWorkStreaks(plan, p, days, maxWork)
	}

	// —— 阶段3：填班次（规则7 每班最少人数，仅统计倒班人员）——
	if err := pi.fillShifts(plan, rotating, days, res); err != nil {
		return nil, err
	}

	// 压平每日早/中班比例（修月初月末残块导致的 1:3 失衡）
	pi.balanceDailyShiftMix(plan, rotating, days)

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
	return splitRestStreaksMin(total, maxRest, 0, userID, year, month)
}

// splitRestStreaksMin 同 splitRestStreaks，但额外接受 minWantSegs：
// 调用方根据「连续上班上限」算出的最少段数，用于从源头避免长班次，
// 从而不必事后拆散双休（事后拆散会把班表打得过碎）。
func splitRestStreaksMin(total, maxRest, minWantSegs int, userID uint, year, month int) []int {
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

	// 双休基准段数：向上取整。
	//
	// 为什么不再抖动段数：段数一旦变少，均分后每段就会 >2 天，
	// 直接产生 3 连休——用户明确要求「能不三休就不要排三休」。
	// 人与人的差异改由「哪几段是单休」体现（见下方打散），
	// 既能错峰，又不会冒出三休。
	wantSegs := (total + 1) / 2
	if wantSegs < minSegs {
		wantSegs = minSegs
	}
	// 为避免「连续上班超过上限」，段数还得够多。
	// 宁可多排几个单休，也不要让人连上 7、8 天
	// ——用户原话：「可以把某个双休改成单休啊，为什么那么死板？」
	if wantSegs < minWantSegs {
		wantSegs = minWantSegs
	}
	if wantSegs > maxSegs {
		wantSegs = maxSegs
	}
	if wantSegs < 1 {
		wantSegs = 1
	}

	// ---- 2) 均分：base 天 + 其中 extra 段各多 1 天 ----
	//
	// wantSegs >= ceil(total/2) 时 base <= 2，
	// 于是每段只可能是 1 或 2 天 —— 天然不出现 3 连休。
	segs := make([]int, wantSegs)
	base := total / wantSegs
	extra := total % wantSegs
	if base < 1 {
		base = 1
		extra = 0
	}
	for i := range segs {
		segs[i] = base
	}

	// extra 个「+1」落在哪几段：用种子打散，让不同人节奏有差异
	if extra > 0 {
		idx := make([]int, wantSegs)
		for i := range idx {
			idx[i] = i
		}
		for i := wantSegs - 1; i > 0; i-- {
			j := int(next()*float64(i+1)) % (i + 1)
			idx[i], idx[j] = idx[j], idx[i]
		}
		for k := 0; k < extra && k < wantSegs; k++ {
			segs[idx[k]]++
		}
	}

	// 兜底：若 maxRest 偏小导致某段超上限，把多出的天数挪给最短的段
	for i := range segs {
		for segs[i] > maxRest {
			segs[i]--
			minIdx := 0
			for j := range segs {
				if segs[j] < segs[minIdx] {
					minIdx = j
				}
			}
			segs[minIdx]++
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
	return restSegmentIndicesLoad(freeLen, segLens, userID, year, month, nil, 0, 0, 0)
}

// restSegmentIndicesLoad 是 restSegmentIndices 的完整版，额外接受每日负载，
// 以及本人在倒班队列中的序号 phaseIdx 与队列总人数 phaseTotal。
//
// 相位为什么必须由「人数」决定（v0.20.0 的核心改动）：
//
//	早期版本用 hash(uid) % segCount 做整段旋转，再叠一个 [0,room] 的小偏移。
//	问题是这两者的取值域都远小于人数：5 段 → 相位只有 5 档，room=2 → 偏移只有 3 档。
//	6 个人挤进这么少的档位，必然大量撞车——表现为「全员几乎同一天休息」，
//	当天在岗人数骤降触发每班人数违规（线上实测 21/30 天不足）。
//
//	正确做法是让 N 个人的相位【均匀铺满整个槽长】：
//	  第 u 个人的段起点 = 槽起点 + u * slotLen / N
//	这样 N 个人的休息段首尾相接、每天恰好覆盖固定人数，
//	在岗人数曲线是平的（6 人 10 休 / 30 天 → 每天恒定 2 人休、4 人上班）。
//	相邻两人的相位差 1 天，正好形成「休休中中早早」的轮转带。
func restSegmentIndicesLoad(freeLen int, segLens []int, userID uint, year, month int, load []int, maxWork int, phaseIdx, phaseTotal int) []int {
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

	// —— 全局统一偏移模型（v0.20.0）——
	//
	// 同一个人的所有休息段共用【同一个偏移量 off】。这一点是硬要求：
	//   段 i 的末尾   = slotStart[i] + off + n_i - 1
	//   段 i+1 的起点 = slotStart[i] + slotLen[i] + off
	//   两者间隔 = slotLen[i] - n_i，与 off 无关 → 恒 >= 1，天然隔离。
	// 若每段各用各的偏移，相邻两段可能首尾相接粘成一条，
	// 突破连续休息上限（实测出现过 [11 12 13 14] 的 4 连休）。
	useSpread := phaseTotal > 0 && phaseIdx >= 0 && phaseIdx < phaseTotal
	wrap := useSpread // 环形回绕仅用于精确铺开

	// 偏移上限：保证每段起点都还在序列内。
	//   wrap 时末段允许溢出（溢出部分回绕到月初），故放宽到 slotLen-1；
	//   不 wrap 时必须完整容纳，故取 slotLen[i]-n_i 的最小值。
	offMax := slotLen[0] - 1
	for i, n := range segLens {
		lim := slotLen[i] - 1
		if !wrap {
			lim = slotLen[i] - n
		}
		if lim < offMax {
			offMax = lim
		}
	}
	if offMax < 0 {
		offMax = 0
	}

	// 理想偏移：把 N 个人均匀铺在 [0, offMax] 上，相邻人差 1 天，
	// 正好形成「休休中中早早」首尾相接的轮转带。
	ideal := 0
	if useSpread {
		ideal = phaseIdx * (offMax + 1) / phaseTotal
	} else {
		// 兜底（调用方未传人数）：沿用旧的种子偏移
		offs := splitSlotOffsets(userID, year, month, segCount, baseSlot)
		if len(offs) > 0 {
			ideal = offs[0] % (offMax + 1)
		}
	}
	if ideal > offMax {
		ideal = offMax
	}
	if ideal < 0 {
		ideal = 0
	}

	hasLoad := len(load) >= freeLen

	// 在 [ideal-1, ideal, ideal+1] 里挑「覆盖日期负载之和」最小的偏移。
	//
	// 注意：精确铺开（useSpread）时【不做微调】。
	// ideal 本身已经是「N 个人均匀铺满 [0,offMax]」的最优解，
	// 任何偏移都会与相邻人的相位撞车——实测 6 人排班中出现
	// 两人同取 off=5、off=1 无人占用的情况，每天休息人数随之在 1~3 波动。
	// 错峰的价值远大于局部负载均衡，故此时直接用 ideal。
	best := ideal
	if !useSpread {
		bestScore := -1
		for _, cand := range []int{ideal, ideal - 1, ideal + 1} {
			if cand < 0 || cand > offMax {
				continue
			}
			score := 0
			for i, n := range segLens {
				s0 := slotStart[i] + cand
				for k := 0; k < n; k++ {
					pos := s0 + k
					if wrap && pos >= freeLen {
						pos -= freeLen
					}
					if hasLoad && pos >= 0 && pos < len(load) {
						score += load[pos]
					}
				}
			}
			if !hasLoad {
				score = abs(cand - ideal) // 无负载时退回「离理想偏移最近」
			}
			if bestScore < 0 || score < bestScore {
				bestScore = score
				best = cand
			}
		}
	}

	// 落位：越界部分环形回绕到月初（形成跨月双休，避免白丢休息天数）。
	seen := map[int]bool{}
	for i, n := range segLens {
		s0 := slotStart[i] + best
		for k := 0; k < n; k++ {
			pos := s0 + k
			if wrap && pos >= freeLen {
				pos -= freeLen
			}
			if pos < 0 || pos >= freeLen {
				continue
			}
			seen[pos] = true
		}
	}

	sorted := make([]int, 0, len(seen))
	for i := range seen {
		sorted = append(sorted, i)
	}
	sort.Ints(sorted)
	return sorted
}

// abs 返回整数绝对值。
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// breakLongWorkStreaks 打断超过 maxWork 的连续上班段。
//
// 背景（用户原话）：
//
//	「那些连续上7天的你可以把某个双休改成单休啊，为什么那么死板？」
//
// 思路：找一段过长的连续上班，在它的中间挑一天改为休息；
// 为了保持「本月总休息天数」不变，再从某个双休里拿掉一天改回上班。
// 净效果：一个双休被拆成两个单休，长班次被切成两段——
// 总休息天数没变，但节奏明显更健康。
//
// 只在必要时动手（确实存在超长班次时才改），不打扰本就合规的排班。
func (pi *PlanInfo) breakLongWorkStreaks(plan map[string]map[string]string, p PlanPerson, days []time.Time, maxWork int) {
	if maxWork <= 0 || len(days) == 0 {
		return
	}
	// 最多循环几轮，防御性上限（正常 1~2 轮即收敛）
	for round := 0; round < 8; round++ {
		// 1) 找出最长的一段连续上班
		bestStart, bestLen := -1, 0
		curStart, curLen := -1, 0
		for i, d := range days {
			if isRestShift(lookPlan(plan, dateKey(d), p.Name)) {
				curStart, curLen = -1, 0
				continue
			}
			if curLen == 0 {
				curStart = i
			}
			curLen++
			if curLen > bestLen {
				bestLen, bestStart = curLen, curStart
			}
		}
		if bestLen <= maxWork {
			return // 没有超长班次，收工
		}

		// 2) 在这段中间挑一天改为休息（切在正中间，两段尽量均衡）
		cut := bestStart + bestLen/2

		// 3) 找一个「双休」拆出一天补回来，保持总休息天数不变
		donor := -1
		for i, d := range days {
			// 跳过即将变成休息的那天及其相邻日，避免把新休息段又破坏掉
			if i >= cut-1 && i <= cut+1 {
				continue
			}
			j := i + 1
			if j >= len(days) {
				break
			}
			if isRestShift(lookPlan(plan, dateKey(d), p.Name)) &&
				isRestShift(lookPlan(plan, dateKey(days[j]), p.Name)) {
				// 连续两天都休息 = 双休，把其中一天改回上班
				donor = i
				break
			}
		}
		if donor < 0 {
			return // 没有可拆的双休，放弃（保持现状，后续校验会如实报告）
		}

		setPlan(plan, dateKey(days[cut]), p.Name, RestShift)
		setPlan(plan, dateKey(days[donor]), p.Name, PendingShift)
	}
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
func (pi *PlanInfo) assignRestDays(plan map[string]map[string]string, p PlanPerson, days []time.Time, targetRest int, forcedWork map[string][]string, restPerDay map[string]int, phaseIdx, phaseTotal int) {
	maxRest := pi.Rule.MaxRestStreak
	if maxRest <= 0 {
		maxRest = 3
	}
	// 注意：targetRest<=0（出勤天数被抬到满月）时不能提前 return——
	// 下方还要为每一天写 PendingShift 占位，否则 fillShifts 无从下手，
	// 会排出一份全空的班表。

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

	// 连续上班上限与本月工作日数，用于反推休息段数下限
	maxWork := pi.Rule.MaxWorkStreak
	if maxWork <= 0 {
		maxWork = 6
	}
	workDays := len(days) - targetRest
	if workDays < 0 {
		workDays = 0
	}

	need := targetRest - len(rest)
	if need > 0 {
		// 切成不超过 maxRest 的段。
		//
		// 关键点：maxRest 是【上限】而非【固定值】。若每次都用满上限，
		// 8 天休息会被机械切成 [3,3,2]，全员一模一样，既不真实也不人性化。
		// 这里改为：以双休(2)为主、必要时用单休(1)调配，
		// 且用确定性种子打散，保证同一(人,月)每次生成结果一致（可复现）。
		//
		// 段数下限由「连续上班上限」反推：
		//   工作块数 ≈ 休息段数 - 1（若月初月末都在休），
		//   要让块长 ≤ maxWork，段数就得 ≥ ⌈工作日/maxWork⌉ + 1。
		// 这样从源头就排不出 7、8 天的长班次，
		// 不必事后拆散双休（事后拆会把班表打得过碎）。
		minWantSegs := 0
		if maxWork > 0 {
			// 工作块长度 ≈ 槽长 - 段长 = 月天数/段数 - 段长。
			// 令其 ≤ maxWork 反解出段数下限：
			//   段数 ≥ 月天数 / (maxWork + 段长)
			// 段长按双休 2 天估算（本函数以双休为主）。
			//
			// 旧公式用「工作日数/maxWork + 1」会高估段数：
			// 28 天 8 休本该 4 段（槽长 7、块长 5 天，未超限），
			// 却被抬成 5 段（槽长 5），导致可用的错峰档位只剩 5 个 < 6 人，
			// 两人撞同一相位 → 该天 3 人同时休息、上班只剩 3 人。
			minWantSegs = (len(days) + maxWork + 2 - 1) / (maxWork + 2)
			if minWantSegs < 1 {
				minWantSegs = 1
			}
		}
		segLens := splitRestStreaksMin(need, maxRest, minWantSegs, p.UserID, pi.Year, pi.Month)

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
				for _, idx := range restSegmentIndicesLoad(len(free), segLens, p.UserID, pi.Year, pi.Month, load, maxWork, phaseIdx, phaseTotal) {
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
			// 负载感知微调：错开不同人的中/早分界，压平每日各班人数
			seg = pi.pickBalancedSegments(len(b.idxs), seg, b.idxs, load, order)
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
		if quota[i] <= 0 {
			// 该班次配额已用完 → 本块不再排它。
			// 典型场景：块只剩 1 天，而中班天数已达标 → 排早班更均衡。
			want = 0
		} else if remainQ > 0 {
			want = (quota[i]*remain + remainQ/2) / remainQ
		}
		// 块长 ≥2 时首段至少 1 天：保证形态是「休 中… 早… 休」，
		// 与规则5（休假后第一天晚班）一致；块长 1 时自由选择。
		minTake := 0
		if i == 0 && L >= 2 {
			minTake = 1
		}
		maxTake := remain - (n - 1 - i)
		if maxTake < minTake {
			maxTake = minTake
		}
		if want < minTake {
			want = minTake
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

// pickBalancedSegments 在基础切分附近微调，压平每日各班次人数。
//
// 为什么需要：若只按「每人均衡」切分，所有人的块会趋于同步——
// 月初大家都在块头（全中班）、月末都在块尾（全早班），
// 于是出现「某天早班 0 人、某天中班 0 人」这种极端分布，
// 直接触发「每班最少人数」违规。
//
// 做法：在基础切分点 ±2 范围内搜索，选「当天该班次已有人数」最少的方案；
// 同时对偏离基础值加二次惩罚，避免为了错峰而破坏班次均衡。
func (pi *PlanInfo) pickBalancedSegments(L int, base []int, idxs []int, load []map[string]int, order []string) []int {
	if len(base) < 2 || L <= 0 {
		return base
	}
	best, bestScore := base, -1
	for delta := -2; delta <= 2; delta++ {
		cand := make([]int, len(base))
		copy(cand, base)
		cand[0] = base[0] + delta
		if cand[0] < 0 || cand[0] > L {
			continue
		}
		remain := L - cand[0]
		if len(cand) == 2 {
			cand[1] = remain
		} else {
			m := len(cand) - 1
			for i := 1; i < len(cand); i++ {
				cand[i] = remain / m
				if i == len(cand)-1 {
					cand[i] = remain - (remain/m)*(m-1)
				}
			}
		}
		// 该方案下，块内每天的「同班次已有人数」之和，越小越均衡
		score := 0
		pos := 0
		for si, cnt := range cand {
			for c := 0; c < cnt && pos < len(idxs); c++ {
				dayIdx := idxs[pos]
				if si < len(order) {
					score += load[dayIdx][order[si]]
				}
				pos++
			}
		}
		// 偏离惩罚：优先小调整，避免破坏每人班次均衡
		score += delta * delta * 2
		if bestScore < 0 || score < bestScore {
			bestScore = score
			best = cand
		}
	}
	return best
}

// applyAdjacencyRules 软约束优化（规则5）：
// 休假前一天尽量排早班、休假后第一天尽量排晚班。
// 只在「不破坏每班最少人数」的前提下做交换，避免按下葫芦浮起瓢。
// balanceDailyShiftMix 压平「每日早班/中班人数」的比例。
//
// 为什么需要：休息日已按人数均匀铺开（每天上班人数恒定），
// 但块内切分点是按【块长比例】定的，而月初/月末存在跨月残块
// （上月延续导致长度 1~3 天），这些残块的切分点与全局轮转错位，
// 于是出现某天「1 早 3 中」这类失衡（11 月实测 3/30 天）。
//
// 修法：只移动【块内的切分点】，绝不单点翻转。
//
//	块形态恒为 [中×a, 早×b]：
//	  · 把最后一个中班日改成早班 → [中×(a-1), 早×(b+1)]
//	  · 把第一个早班日改成中班 → [中×(a+1), 早×(b-1)]
//	两种操作都保持块内单向（休→中→早→休），不会倒班。
func (pi *PlanInfo) balanceDailyShiftMix(plan map[string]map[string]string, rotating []PlanPerson, days []time.Time) {
	morning, evening := pi.Morning, pi.Evening
	if morning == "" || evening == "" || len(days) == 0 {
		return
	}
	names := make([]string, 0, len(rotating))
	for _, p := range rotating {
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		return
	}

	for i, d := range days {
		k := dateKey(d)
		prevK, nextK := "", ""
		if i > 0 {
			prevK = dateKey(days[i-1])
		}
		if i+1 < len(days) {
			nextK = dateKey(days[i+1])
		}

		// 最多修 |差值| 次；每轮只动 1 人，改完立即重算
		for round := 0; round < len(names); round++ {
			mc, ec := 0, 0
			for _, n := range names {
				switch lookPlan(plan, k, n) {
				case morning:
					mc++
				case evening:
					ec++
				}
			}
			total := mc + ec
			if total < 2 {
				break
			}
			// 目标：早中尽量各半（早班取较少的一份，与班次推进顺序无关）
			wantM := total / 2
			if mc == wantM {
				break
			}

			moved := false
			if mc < wantM {
				// 早班不够：把一个「中班日」改成早班。
				// 必须是其工作块的【最后一个中班日】，否则会造出 中早中 的倒班。
				for _, n := range names {
					if lookPlan(plan, k, n) != evening {
						continue
					}
					if nextK != "" && lookPlan(plan, nextK, n) == evening {
						continue // 后面还是中班 → 不是最后一个
					}
					setPlan(plan, k, n, morning)
					moved = true
					break
				}
			} else {
				// 早班过多：把一个「早班日」改成中班。
				// 必须是其工作块的【第一个早班日】，否则会造出 早中早 的倒班。
				for _, n := range names {
					if lookPlan(plan, k, n) != morning {
						continue
					}
					if prevK != "" && lookPlan(plan, prevK, n) == morning {
						continue // 前面还是早班 → 不是第一个
					}
					setPlan(plan, k, n, evening)
					moved = true
					break
				}
			}
			if !moved {
				break // 无人可动，放弃这一天
			}
		}
	}
}

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
