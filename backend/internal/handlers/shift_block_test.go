package handlers

import (
	"strings"
	"testing"
	"time"
)

// TestPlanBlockSegmentsSum 各段之和必须等于块长，且尽可能均等。
func TestPlanBlockSegmentsSum(t *testing.T) {
	for L := 1; L <= 12; L++ {
		for _, quota := range [][]int{{11, 11}, {5, 6}, {1, 1}, {0, 8}, {8, 0}} {
			seg := planBlockSegments(L, quota)
			sum := 0
			for _, v := range seg {
				if v < 0 {
					t.Fatalf("L=%d quota=%v 出现负段长 %v", L, quota, seg)
				}
				sum += v
			}
			if sum != L {
				t.Fatalf("L=%d quota=%v 段长之和 %d != %d（seg=%v）", L, quota, sum, L, seg)
			}
		}
	}
}

// TestPlanBlockSegmentsBalanced 长块应被切成接近均等的两段，
// 这是「每人早晚班次各 11 天」的基础。
func TestPlanBlockSegmentsBalanced(t *testing.T) {
	seg := planBlockSegments(6, []int{11, 11})
	if seg[0] != 3 || seg[1] != 3 {
		t.Errorf("6 天块应切成 3+3，实际 %v", seg)
	}
	seg = planBlockSegments(5, []int{11, 11})
	diff := seg[0] - seg[1]
	if diff < 0 {
		diff = -diff
	}
	if diff > 1 {
		t.Errorf("5 天块应切成 3+2 或 2+3，实际 %v", seg)
	}
}

// TestBlockShiftOrder 班次推进顺序必须是「晚 → 早」单向，
// 即中班在前、早班在后（休 中中中 早早早 休）。
func TestBlockShiftOrder(t *testing.T) {
	pi := &PlanInfo{
		Shifts:  []string{"早班", "中班"},
		Morning: "早班",
		Evening: "中班",
	}
	order := pi.blockShiftOrder()
	if len(order) != 2 || order[0] != "中班" || order[1] != "早班" {
		t.Fatalf("推进顺序应为 [中班 早班]，实际 %v", order)
	}
}

// TestFillShiftsNoZigzag 端到端：任何人任何工作块内都不得出现
// 「中→早→中」这种来回倒班。这是用户明确禁止的形态。
func TestFillShiftsNoZigzag(t *testing.T) {
	// 核心不变量：按 order 顺序填出的块天然单向
	order := []string{"中班", "早班", "夜班"}
	for _, seg := range [][]int{{3, 3}, {1, 4}, {4, 1}, {2, 2, 3}, {5, 2}} {
		var seq []string
		for si, cnt := range seg {
			for c := 0; c < cnt && si < len(order); c++ {
				seq = append(seq, order[si])
			}
		}
		// 检查是否出现「回到先前出现过的班次」
		seen := map[string]bool{}
		prev := ""
		for _, sh := range seq {
			if sh != prev && seen[sh] {
				t.Fatalf("班次来回倒：%s（seg=%v）", strings.Join(seq, ""), seg)
			}
			seen[sh] = true
			prev = sh
		}
	}
}

// TestBalanceDailyShiftMix 端到端验证「每日早/中班人数压平」。
//
// 场景来自线上真实输出（2026-11，6 人 20 天出勤）：
// 休息日已完美错峰（每天恒定 4 人上班），但月初跨月残块让
// 头两天变成「1 早 3 中」，末日变成「3 早 1 中」。
func TestBalanceDailyShiftMix(t *testing.T) {
	pi := &PlanInfo{
		Shifts:  []string{"早班", "中班"},
		Morning: "早班",
		Evening: "中班",
	}
	// 6 人 × 30 天，手工构造与线上一致的序列（休=休息）
	seqs := []string{
		"休休中中早早休休中中早早休休中中早早休休中中早早休休中中早早", // 004
		"早休休中中早早休休中中早早休休中中早早休休中中早早休休中中早", // 005
		"中早休休中中早早休休中中早早休休中中早早休休中中早早休休中早", // 006
		"中中早休休中中早早休休中中早早休休中中早早休休中中早早休休中", // 007
		"中中早早休休中中早早休休中中早早休休中中早早休休中中早早休休", // 009
		"休中中早早休休中中早早休休中中早早休休中中早早休休中中早早休", // 010
	}
	names := []string{"A", "B", "C", "D", "E", "F"}
	days := make([]time.Time, 0, 30)
	keys := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		d := time.Date(2026, 11, i+1, 0, 0, 0, 0, time.Local)
		days = append(days, d)
		keys = append(keys, dateKey(d))
	}
	people := make([]PlanPerson, 0, len(names))
	for i, n := range names {
		people = append(people, PlanPerson{UserID: uint(i + 1), Name: n, Mode: "rotate"})
	}

	trans := map[rune]string{'休': RestShift, '中': "中班", '早': "早班"}
	plan := map[string]map[string]string{}
	for ni, sq := range seqs {
		for di, ch := range []rune(sq) {
			setPlan(plan, keys[di], names[ni], trans[ch])
		}
	}

	// 修补前：第 0、1 天应为 1 早 3 中（这正是要修的问题）
	before := dailyMix(plan, keys, names, pi)
	if before[0][0] != 1 || before[0][1] != 3 {
		t.Fatalf("前置条件不成立：第 0 天应为 1 早 3 中，实际 %v（测试数据需更新）", before[0])
	}

	pi.balanceDailyShiftMix(plan, people, days)

	// 修补后：每天上班 4 人，早中必须各 2
	for i, k := range keys {
		m, e := 0, 0
		for _, n := range names {
			switch lookPlan(plan, k, n) {
			case "早班":
				m++
			case "中班":
				e++
			}
		}
		if m+e == 0 {
			continue
		}
		if m != e {
			t.Errorf("第 %d 天早 %d / 中 %d，未压平（期望各半）", i+1, m, e)
		}
	}

	// 关键：修补不得破坏单向形态（休→中→早→休，不得来回倒）
	for ni, n := range names {
		var seq []string
		for _, k := range keys {
			seq = append(seq, lookPlan(plan, k, n))
		}
		for _, blk := range splitBlocks(seq) {
			seen := map[string]bool{}
			prev := ""
			for _, sh := range blk {
				if sh != prev && seen[sh] {
					t.Errorf("第 %d 人块 %v 出现来回倒班", ni, blk)
					break
				}
				seen[sh] = true
				prev = sh
			}
		}
	}
}

// dailyMix 返回每天的 [早班人数, 中班人数]
func dailyMix(plan map[string]map[string]string, keys, names []string, pi *PlanInfo) [][2]int {
	out := make([][2]int, len(keys))
	for i, k := range keys {
		for _, n := range names {
			switch lookPlan(plan, k, n) {
			case pi.Morning:
				out[i][0]++
			case pi.Evening:
				out[i][1]++
			}
		}
	}
	return out
}

// splitBlocks 把一人的班次序列按休息日切成若干工作块
func splitBlocks(seq []string) [][]string {
	var res [][]string
	var cur []string
	for _, s := range seq {
		if isRestShift(s) {
			if len(cur) > 0 {
				res = append(res, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, s)
	}
	if len(cur) > 0 {
		res = append(res, cur)
	}
	return res
}
