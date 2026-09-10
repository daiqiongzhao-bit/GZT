package handlers

import (
	"testing"
)

// TestSplitRestStreaksMixed 验证休息段是「混合长度」而非清一色 3 连休。
//
// 需求来源：用户明确要求「最高 3 连休，不是每次都是 3 连休，
// 也可以单休，也可以双休」。maxRest 是上限，不是固定值。
func TestSplitRestStreaksMixed(t *testing.T) {
	total := 8
	maxRest := 3

	// 统计多种子下的分布
	lenCount := map[int]int{}
	totalSegs := 0
	for uid := uint(1); uid <= 20; uid++ {
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

	t.Logf("20 个种子下段长分布：%v（共 %d 段）", lenCount, totalSegs)

	// 必须出现单休
	if lenCount[1] == 0 {
		t.Errorf("应出现单休(1 连休)，实际没有：%v", lenCount)
	}
	// 必须出现双休
	if lenCount[2] == 0 {
		t.Errorf("应出现双休(2 连休)，实际没有：%v", lenCount)
	}
	// 必须出现 3 连休（上限被用到）
	if lenCount[3] == 0 {
		t.Errorf("应出现 3 连休，实际没有：%v", lenCount)
	}
	// 单休+双休 应占多数（体现「不是每次都是 3 连休」）
	soft := lenCount[1] + lenCount[2]
	if soft*2 < totalSegs {
		t.Errorf("单休+双休 占比 %d/%d 过低，仍偏向长连休", soft, totalSegs)
	}
	// 3 连休不应成为绝对主流
	if lenCount[3]*2 >= totalSegs {
		t.Errorf("3 连休 占比 %d/%d 过高，仍偏向长连休", lenCount[3], totalSegs)
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

// TestSplitRestStreaksDifferentUsers 不同人的休息段切分应当有差异，
// 避免「全员同一模式」。
func TestSplitRestStreaksDifferentUsers(t *testing.T) {
	patterns := map[string]int{}
	for uid := uint(1); uid <= 10; uid++ {
		segs := splitRestStreaks(8, 3, uid, 2026, 11)
		key := ""
		for _, n := range segs {
			key += string(rune('0' + n))
		}
		patterns[key]++
	}
	if len(patterns) < 2 {
		t.Errorf("10 个用户只产生了 %d 种休息模式，缺乏多样性：%v",
			len(patterns), patterns)
	}
	t.Logf("10 个用户产生 %d 种模式：%v", len(patterns), patterns)
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

// TestSplitDriftBounded 错峰偏移必须在 ±ampl 内且首段为 0，
// 否则会把休息段推到一起或推出月外。
func TestSplitDriftBounded(t *testing.T) {
	freeLen := 22
	segCount := 4
	ampl := freeLen / segCount / 3
	if ampl < 1 {
		ampl = 1
	}
	for uid := uint(1); uid <= 50; uid++ {
		d := splitDrift(uid, 2026, 11, segCount, freeLen)
		if len(d) != segCount {
			t.Fatalf("偏移长度 %d != %d", len(d), segCount)
		}
		if d[0] != 0 {
			t.Errorf("uid=%d 首段偏移应为 0，实际 %d", uid, d[0])
		}
		for i, v := range d {
			if v > ampl || v < -ampl {
				t.Errorf("uid=%d 第 %d 段偏移 %d 超出 ±%d", uid, i, v, ampl)
			}
		}
	}
}

// TestSplitDriftVariesByUser 不同用户的偏移应当不同（真正起到错峰作用）。
func TestSplitDriftVariesByUser(t *testing.T) {
	seen := map[string]int{}
	for uid := uint(1); uid <= 10; uid++ {
		d := splitDrift(uid, 2026, 11, 4, 22)
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
