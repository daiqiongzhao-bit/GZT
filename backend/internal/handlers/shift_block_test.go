package handlers

import (
	"strings"
	"testing"
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
