package handlers

import (
	"testing"
)

// TestSplitRestStreaksDoubleFirst 验证休息段「尽量双连休」：
//
// 需求来源：用户要求「在满足人数的情况下，尽量双连休」。
// 双休(2) 应成为主体，3 连休作为上限内的调配，单休(1) 尽量少。
func TestSplitRestStreaksDoubleFirst(t *testing.T) {
	total := 8
	maxRest := 3

	lenCount := map[int]int{}
	totalSegs := 0
	for uid := uint(1); uid <= 30; uid++ {
		segs := splitRestStreaks(total, maxRest, uid, 2026, 11)
		sum := 0
		for _, n := range segs {
			if n < 1 || n > maxRest {
				t.Fatalf("uid=%d 段长 %d 超出 [1,%d]：%v", uid, n, maxRest, segs)
			}
			lenCount[n]++
			sum += n
		}
		if sum != total {
			t.Fatalf("uid=%d 段长之和 %d != %d：%v", uid, sum, total, segs)
		}
		totalSegs += len(segs)
	}

	p1 := float64(lenCount[1]) / float64(totalSegs) * 100
	p2 := float64(lenCount[2]) / float64(totalSegs) * 100
	p3 := float64(lenCount[3]) / float64(totalSegs) * 100
	t.Logf("30 个种子段长分布：单休 %d(%.0f%%) 双休 %d(%.0f%%) 3连休 %d(%.0f%%)",
		lenCount[1], p1, lenCount[2], p2, lenCount[3], p3)

	// 双休必须是主体
	if p2 < 50 {
		t.Errorf("双休占比 %.0f%% 应为主流（>=50%%）：%v", p2, lenCount)
	}
	// 单休应尽量少
	if p1 > 25 {
		t.Errorf("单休占比 %.0f%% 过高（应 <=25%%）：%v", p1, lenCount)
	}
	// 上限内允许出现 3 连休作调配
	if lenCount[3] == 0 {
		t.Errorf("应有少量 3 连休用于调配（上限内）：%v", lenCount)
	}
}

// TestSplitRestStreaksAllDoubleWhenEven 偶数天且上限>=2 时，
// 8 天休息应能排出「全双休」结构（4 段 × 2 天）。
func TestSplitRestStreaksAllDoubleWhenEven(t *testing.T) {
	found := false
	for uid := uint(1); uid <= 30; uid++ {
		segs := splitRestStreaks(8, 3, uid, 2026, 11)
		allTwo := true
		for _, n := range segs {
			if n != 2 {
				allTwo = false
				break
			}
		}
		if allTwo {
			found = true
			t.Logf("uid=%d 排出全双休：%v", uid, segs)
			break
		}
	}
	if !found {
		t.Error("8 天休息在 30 个种子中应至少出现一次「全双休」(4×2)")
	}
}

// TestSplitRestStreaksDeterministic 同一(人,月)多次调用结果必须一致，
// 否则「生成预览 → 应用」两步之间结果会漂移。
func TestSplitRestStreaksDeterministic(t *testing.T) {
	for uid := uint(1); uid <= 5; uid++ {
		a := splitRestStreaks(8, 3, uid, 2026, 11)
		b := splitRestStreaks(8, 3, uid, 2026, 11)
		if len(a) != len(b) {
			t.Fatalf("uid=%d 两次段数不同：%v vs %v", uid, a, b)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("uid=%d 两次结果不同：%v vs %v", uid, a, b)
			}
		}
	}
}

// TestSplitRestStreaksDoubleDominant 多个用户的休息段都应「以双休为主」，
// 而不是各人风格差异过大导致部分人全是单休。
func TestSplitRestStreaksDoubleDominant(t *testing.T) {
	bad := 0
	for uid := uint(1); uid <= 20; uid++ {
		segs := splitRestStreaks(8, 3, uid, 2026, 11)
		two, one := 0, 0
		for _, n := range segs {
			if n == 2 {
				two++
			}
			if n == 1 {
				one++
			}
		}
		// 双休段数必须不少于单休段数
		if two < one {
			bad++
			t.Logf("uid=%d 双休 %d 段 < 单休 %d 段：%v", uid, two, one, segs)
		}
	}
	if bad > 0 {
		t.Errorf("%d 个用户的双休段少于单休段，未做到「尽量双连休」", bad)
	}
}

// TestSplitRestStreaksEdgeCases 边界：上限 1、总数 0、总数小于上限。
func TestSplitRestStreaksEdgeCases(t *testing.T) {
	if got := splitRestStreaks(0, 3, 1, 2026, 11); got != nil {
		t.Errorf("total=0 应返回 nil，实际 %v", got)
	}

	// 上限 1 → 全部单休
	segs := splitRestStreaks(5, 1, 1, 2026, 11)
	if len(segs) != 5 {
		t.Fatalf("上限 1 应切 5 段，实际 %d：%v", len(segs), segs)
	}
	for _, n := range segs {
		if n != 1 {
			t.Errorf("上限 1 时每段应为 1，实际 %d", n)
			break
		}
	}

	// 总数 2、上限 3 → 一段 2 天（不超过上限）
	segs = splitRestStreaks(2, 3, 1, 2026, 11)
	sum := 0
	for _, n := range segs {
		if n > 3 {
			t.Errorf("段长 %d 超上限", n)
		}
		sum += n
	}
	if sum != 2 {
		t.Errorf("总数应为 2，实际 %d：%v", sum, segs)
	}

	// 上限 2 时不应出现 3
	for uid := uint(1); uid <= 20; uid++ {
		for _, n := range splitRestStreaks(9, 2, uid, 2026, 11) {
			if n > 2 {
				t.Fatalf("上限 2 却出现段长 %d", n)
			}
		}
	}
}

// TestSplitSlotOffsetsBounded 槽内偏移必须在 ±slot/2 内，
// 否则休息段会越出本槽、破坏错峰效果。
func TestSplitSlotOffsetsBounded(t *testing.T) {
	slot := 5
	segCount := 4
	ampl := slot / 2
	if ampl < 1 {
		ampl = 1
	}
	for uid := uint(1); uid <= 50; uid++ {
		d := splitSlotOffsets(uid, 2026, 11, segCount, slot)
		if len(d) != segCount {
			t.Fatalf("偏移长度 %d != %d", len(d), segCount)
		}
		for i, v := range d {
			if v > ampl || v < -ampl {
				t.Errorf("uid=%d 第 %d 段偏移 %d 超出 ±%d", uid, i, v, ampl)
			}
		}
	}

	// slot <= 1 时应全为 0（没有错峰空间）
	for _, v := range splitSlotOffsets(1, 2026, 11, 4, 1) {
		if v != 0 {
			t.Errorf("slot=1 时偏移应为 0，实际 %d", v)
		}
	}
}

// TestSplitSlotOffsetsVariesByUser 不同用户的槽内偏移应当不同，
// 这是「错峰休息、避免同一天集体休假」的关键。
func TestSplitSlotOffsetsVariesByUser(t *testing.T) {
	seen := map[string]int{}
	for uid := uint(1); uid <= 10; uid++ {
		d := splitSlotOffsets(uid, 2026, 11, 4, 5)
		key := ""
		for _, v := range d {
			key += string(rune('a' + v + 5))
		}
		seen[key]++
	}
	if len(seen) < 3 {
		t.Errorf("10 个用户只产生 %d 种偏移模式，错峰效果不足：%v",
			len(seen), seen)
	}
}

// TestRestSegmentIndicesNoOverlap 验证休息段互不重叠、且不超出序列范围。
//
// 这是 v0.18.3 修掉的一个真实缺陷：原先用 start = len(free) - n 兜底，
// 会把多个段挤到序列末尾连成一片，造出超过 maxRest 的超长连休
// （线上实测出现过「3连休 + 5连休」，而规则上限是 3）。
func TestRestSegmentIndicesNoOverlap(t *testing.T) {
	for uid := uint(1); uid <= 40; uid++ {
		for _, segLens := range [][]int{{2, 2, 2, 2}, {2, 3, 3}, {3, 3, 2}, {2, 2, 3, 1}, {1, 1, 2, 2, 2}} {
			freeLen := 19
			idxs := restSegmentIndices(freeLen, segLens, uid, 2026, 9)

			want := 0
			for _, n := range segLens {
				want += n
			}
			if len(idxs) != want {
				t.Fatalf("uid=%d segLens=%v：应产生 %d 个休息日，实际 %d",
					uid, segLens, want, len(idxs))
			}
			seen := map[int]bool{}
			for _, p := range idxs {
				if p < 0 || p >= freeLen {
					t.Fatalf("uid=%d segLens=%v：下标 %d 越界 [0,%d)",
						uid, segLens, p, freeLen)
				}
				if seen[p] {
					t.Fatalf("uid=%d segLens=%v：下标 %d 重复（休息段重叠）",
						uid, segLens, p)
				}
				seen[p] = true
			}
		}
	}
}

// TestRestSegmentIndicesNoOverStreak 验证最重要的不变量：
// 落点合并后，任何连续休息长度都不得超过 maxRest。
//
// 这正是线上「员工009 出现 5 连休」的回归防线。
func TestRestSegmentIndicesNoOverStreak(t *testing.T) {
	maxRest := 3
	for uid := uint(1); uid <= 60; uid++ {
		segLens := splitRestStreaks(8, maxRest, uid, 2026, 9)
		freeLen := 19
		idxs := restSegmentIndices(freeLen, segLens, uid, 2026, 9)

		occupied := map[int]bool{}
		for _, p := range idxs {
			occupied[p] = true
		}
		// 扫描整条序列，统计最长连续休息
		streak, longest := 0, 0
		for i := 0; i < freeLen; i++ {
			if occupied[i] {
				streak++
				if streak > longest {
					longest = streak
				}
			} else {
				streak = 0
			}
		}
		if longest > maxRest {
			t.Fatalf("uid=%d segLens=%v：出现 %d 连休，超过上限 %d（下标 %v）",
				uid, segLens, longest, maxRest, idxs)
		}
	}
}

// TestRestSegmentIndicesDeterministic 同一(人,月)必须可复现，
// 否则「重新生成」会得到不同结果，用户无法信任。
func TestRestSegmentIndicesDeterministic(t *testing.T) {
	a := restSegmentIndices(19, []int{2, 2, 2, 2}, 7, 2026, 9)
	b := restSegmentIndices(19, []int{2, 2, 2, 2}, 7, 2026, 9)
	if len(a) != len(b) {
		t.Fatalf("长度不一致：%d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("第 %d 位不一致：%d vs %d", i, a[i], b[i])
		}
	}
}

// TestRestSegmentIndicesSpreadAcrossUsers 验证错峰有效：
// 不同用户的休息日不应雷同。
func TestRestSegmentIndicesSpreadAcrossUsers(t *testing.T) {
	segLens := []int{2, 2, 2, 2}
	patterns := map[string]int{}
	for uid := uint(12); uid <= 17; uid++ {
		idxs := restSegmentIndices(26, segLens, uid, 2026, 9)
		key := ""
		for _, p := range idxs {
			key += string(rune('A' + p))
		}
		patterns[key]++
	}
	if len(patterns) < 4 {
		t.Errorf("6 名员工只产生 %d 种休息模式，错峰不足：%v",
			len(patterns), patterns)
	}
}
