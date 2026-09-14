package handlers

import (
	"fmt"
	"testing"

	"github.com/gin-gonic/gin"

	"shiftworkbench/internal/data"
)

// TestHolidayNotForced 回归保护：法定节假日只作「提示」，绝不进入生成逻辑、不强制休息。
// 用户硬约束：节假日联动只能在班表/排班「体现」，不能强制员工必休或必上。
// 2026-02 春节（9 天连休）、2026-10 国庆（7 天连休）是最严苛的长假期场景，
// 生成器应产出与平常等价的合法班表：每班人数不跌破下限、不出现三连休。
func TestHolidayNotForced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ym := range [][2]int{{2026, 2}, {2026, 10}} {
		for _, n := range []int{8, 12} {
			n, ym := n, ym
			t.Run(fmt.Sprintf("N%d_%d%02d", n, ym[0], ym[1]), func(t *testing.T) {
				c := scaleCase{N: n, Work: 20, MinPer: 2, Shifts: twoShift()}
				setupScaleDB(t, c)
				out := genScale(t, ym[0], ym[1])
				st := inspect(t, out, c)
				need := c.MinPer * len(c.Shifts)
				slack := float64(c.N*c.Work)/float64(st.Days) - float64(need)
				if st.BadDays > 0 && slack >= 1.2 {
					t.Errorf("%d-%02d 节假日月份出现 %d 天班次人数不足", ym[0], ym[1], st.BadDays)
				}
				if st.TriRest > 0 {
					t.Errorf("%d-%02d 节假日月份出现 %d 次三连休（节假日不应强制休息）", ym[0], ym[1], st.TriRest)
				}
			})
		}
	}
}

// TestHolidayData 验证内置节假日数据源（2026 劳动节）准确：rest 含 05-01~05-05，
// work 含调休补班 05-09，普通工作日不在其中。
func TestHolidayData(t *testing.T) {
	rest, work := data.HolidaysInRange("2026-05-01", "2026-05-31")
	for _, d := range []string{"2026-05-01", "2026-05-02", "2026-05-03", "2026-05-04", "2026-05-05"} {
		if _, ok := rest[d]; !ok {
			t.Fatalf("劳动节 %s 应标记为法定休息日 rest", d)
		}
	}
	if _, ok := work["2026-05-09"]; !ok {
		t.Fatalf("劳动节调休补班 2026-05-09 应标记为 work")
	}
	for _, d := range []string{"2026-05-10", "2026-05-20"} {
		if _, ok := rest[d]; ok {
			t.Fatalf("%s 不是节假日，不应出现在 rest", d)
		}
		if _, ok := work[d]; ok {
			t.Fatalf("%s 不是补班日，不应出现在 work", d)
		}
	}
}
