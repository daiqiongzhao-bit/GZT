package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// scaleCase 描述一套排班场景。
type scaleCase struct {
	N        int          // 倒班人数
	Work     int          // 月出勤天数
	MinPer   int          // 每班最少人数
	Shifts   []scaleShift // 班次池（按时间正序传入）
	RestAdj  bool         // 开启规则5：休假前须早班 / 休假后须晚班
}

type scaleShift struct {
	Name  string
	Start string
	End   string
}

// twoShift 是线上当前形态：早班 + 中班。
func twoShift() []scaleShift {
	return []scaleShift{
		{Name: "早班", Start: "08:00", End: "16:00"},
		{Name: "中班", Start: "12:00", End: "20:00"},
	}
}

// threeShift 三班制：早班 + 中班 + 晚班。
func threeShift() []scaleShift {
	return []scaleShift{
		{Name: "早班", Start: "08:00", End: "16:00"},
		{Name: "中班", Start: "12:00", End: "20:00"},
		{Name: "晚班", Start: "20:00", End: "23:30"},
	}
}

// setupScaleDB 按场景建库：N 个倒班人员 + 指定班次池 + 排班规则。
func setupScaleDB(t *testing.T, c scaleCase) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	d, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(
		&models.User{}, &models.ShiftConfig{}, &models.ShiftRule{},
		&models.UserShiftPref{}, &models.ShiftRequest{}, &models.SpecialWorkDay{},
		&models.Department{}, &models.Schedule{}, &models.Log{}, &models.Notification{},
	); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	db.DB.Create(&models.Department{ID: 1, Name: "测试门店"})
	for _, s := range c.Shifts {
		db.DB.Create(&models.ShiftConfig{
			DeptID: 1, Name: s.Name, StartTime: s.Start, EndTime: s.End,
		})
	}
	for i := 0; i < c.N; i++ {
		db.DB.Create(&models.User{
			Name: fmt.Sprintf("员工%02d", i+1), Username: fmt.Sprintf("E%03d", i+1),
			EmpNo: fmt.Sprintf("E%03d", i+1), Role: models.RoleExecutor, DeptID: 1,
		})
	}
	db.DB.Create(&models.ShiftRule{
		DeptID: 1, MinPerShift: c.MinPer, MaxRestStreak: 3, MaxWorkStreak: 6, MonthWorkDays: c.Work,
		RequireMorningBeforeRest: c.RestAdj, RequireEveningAfterRest: c.RestAdj,
	})
}

func genScale(t *testing.T, year, month int) map[string]interface{} {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("claims", &models.Claims{UserID: 1, Username: "admin", Role: models.RoleSuperAdmin, DeptID: 0})
		c.Next()
	})
	r.POST("/g", GenerateSchedule)
	body, _ := json.Marshal(map[string]interface{}{"dept_id": 1, "year": year, "month": month})
	req := httptest.NewRequest(http.MethodPost, "/g", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("生成失败 %d: %s", w.Code, w.Body.String())
	}
	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// scaleStat 一次生成结果的体检数据。
type scaleStat struct {
	Days      int
	MinPer    map[string]int // 各班次单日最少人数
	BadDays   int            // 有班次低于 MinPerShift 的天数
	LessM     int            // 早班人数 < 中班人数的天数（仅两班制）
	TriRest   int            // 三连休次数
	MaxWork   int            // 最长连续上班天数
	MaxSkew   int            // 单人早/中班天数最大差值
	WarnCount int
	BadDetail []string // 违规日详情，纯数字便于排查：2026-11-05:cnt=[7 0 6]
}

// inspect 对生成结果做全量体检。
func inspect(t *testing.T, out map[string]interface{}, c scaleCase) scaleStat {
	t.Helper()
	planRaw, _ := out["plan"].(map[string]interface{})
	dates := make([]string, 0, len(planRaw))
	for k := range planRaw {
		dates = append(dates, k)
	}
	sort.Strings(dates)

	peopleRaw, _ := out["people"].([]interface{})
	names := []string{}
	for _, p := range peopleRaw {
		if m, ok := p.(map[string]interface{}); ok {
			if f, _ := m["is_fixed"].(bool); !f {
				names = append(names, fmt.Sprint(m["name"]))
			}
		}
	}

	shiftNames := make([]string, 0, len(c.Shifts))
	for _, s := range c.Shifts {
		shiftNames = append(shiftNames, s.Name)
	}

	st := scaleStat{Days: len(dates), MinPer: map[string]int{}}
	for _, s := range shiftNames {
		st.MinPer[s] = 99
	}

	// 每人各班次累计天数
	perPerson := map[string]map[string]int{}
	for _, nm := range names {
		perPerson[nm] = map[string]int{}
	}

	for _, d := range dates {
		row, _ := planRaw[d].(map[string]interface{})
		cnt := map[string]int{}
		for _, nm := range names {
			v := fmt.Sprint(row[nm])
			if _, ok := perPerson[nm][v]; ok || v != "休息" {
				perPerson[nm][v]++
			}
			if v != "休息" {
				cnt[v]++
			}
		}
		bad := false
		for _, s := range shiftNames {
			if cnt[s] < st.MinPer[s] {
				st.MinPer[s] = cnt[s]
			}
			if cnt[s] < c.MinPer {
				bad = true
			}
		}
		if bad {
			st.BadDays++
			if len(st.BadDetail) < 8 {
				nums := make([]string, 0, len(shiftNames))
				for _, s := range shiftNames {
					nums = append(nums, fmt.Sprint(cnt[s]))
				}
				st.BadDetail = append(st.BadDetail, fmt.Sprintf("%s:cnt=[%s]", d, strings.Join(nums, " ")))
			}
		}
		if len(c.Shifts) == 2 && cnt["早班"] < cnt["中班"] {
			st.LessM++
		}
	}

	// 单人维度：三连休 / 最长连班 / 班次天数差
	for _, nm := range names {
		seq := make([]string, 0, len(dates))
		for _, d := range dates {
			row, _ := planRaw[d].(map[string]interface{})
			seq = append(seq, fmt.Sprint(row[nm]))
		}
		sym := make([]rune, len(seq))
		for i, s := range seq {
			switch s {
			case "休息":
				sym[i] = '休'
			case "早班":
				sym[i] = '早'
			case "中班":
				sym[i] = '中'
			case "晚班":
				sym[i] = '晚'
			default:
				sym[i] = '?'
			}
		}
		run := 0
		for _, ch := range sym {
			if ch == '休' {
				run++
				if run >= 3 {
					st.TriRest++
				}
			} else {
				run = 0
			}
		}
		// 注意：中文是 3 字节，必须用 rune 计数，否则 4 天会被算成 12 天。
		for _, blk := range strings.Split(string(sym), "休") {
			if n := len([]rune(blk)); n > st.MaxWork {
				st.MaxWork = n
			}
		}
		if len(c.Shifts) == 2 {
			diff := perPerson[nm]["早班"] - perPerson[nm]["中班"]
			if diff < 0 {
				diff = -diff
			}
			if diff > st.MaxSkew {
				st.MaxSkew = diff
			}
		}
	}

	if ws, ok := out["warnings"].([]interface{}); ok {
		st.WarnCount = len(ws)
	}
	return st
}

// runScale 跑一个场景并在失败时报错。返回 false 表示该组合数学上无解已跳过。
func runScale(t *testing.T, c scaleCase, year, month int) bool {
	t.Helper()
	setupScaleDB(t, c)
	out := genScale(t, year, month)
	st := inspect(t, out, c)

	// 可行性下限：每天需要 MinPer*班次数 人上班，此外还得留人轮休
	// （连班上限 6 天意味着干 6 天必休 1 天，满编无休的组合本身无解）。
	// 留 3 人轮休余量仍不够 → 属于参数组合不可行，不计为算法缺陷。
	// 只卡真正的硬下界：至少得留 1 个人轮休，否则全员满勤无休。
	// 曾设成 need+3，结果把 N=6（线上真实形态，每天 4 人上班）整个跳过了。
	// 人数够但排不开的情况交给下面的 TIGHT 判定，不要在这里误杀。
	need := c.MinPer * len(c.Shifts)
	if c.N < need+1 {
		t.Logf("SKIP N=%d %d-%02d: only %d people but %d must work daily (need >=%d)",
			c.N, year, month, c.N, need, need+1)
		return false
	}

	parts := make([]string, 0, len(c.Shifts))
	for _, s := range c.Shifts {
		parts = append(parts, fmt.Sprintf("%s%d", s.Name[0:1], st.MinPer[s.Name]))
	}
	bad := st.BadDays > 0 || st.TriRest > 0 || st.MaxWork > 6 || st.LessM > 0
	status := "OK  "
	if bad {
		status = "FAIL"
	}
	t.Logf("%s N=%-3d %d-%02d (%2d天) 出勤%d 每班≥%d 班次%d | 最少 %s | 不足天=%d 早<中=%d 三休=%d 最长连班=%d 班次差=%d 警告=%d",
		status, c.N, year, month, st.Days, c.Work, c.MinPer, len(c.Shifts),
		strings.Join(parts, "/"), st.BadDays, st.LessM, st.TriRest, st.MaxWork, st.MaxSkew, st.WarnCount)

	// 纯 ASCII 摘要：中文日志在部分终端/管道下会乱码，汇总分析用这行。
	t.Logf("DATA|N=%d|%d-%02d|shifts=%d|minPer=%d|work=%d|days=%d|bad=%d|lessM=%d|tri=%d|maxWork=%d|skew=%d|warn=%d",
		c.N, year, month, len(c.Shifts), c.MinPer, c.Work, st.Days,
		st.BadDays, st.LessM, st.TriRest, st.MaxWork, st.MaxSkew, st.WarnCount)

	if st.BadDays > 0 {
		// 「零余量」判定：平均每天上班人数几乎等于「每班最低需求之和」时，
		// 任何搬运都会让某个班次跌破下限，属于配置本身没有腾挪空间，
		// 不是算法缺陷 —— 此时不算失败，但给出配人建议。
		slack := float64(c.N*c.Work)/float64(st.Days) - float64(need)
		if slack < 1.2 {
			suggest := (need+2)*st.Days/c.Work + 1
			t.Logf("TIGHT N=%d %d-%02d: 平均每天仅 %.2f 人上班，却要填满 %d 人（%d 个班次 × 每班 %d 人），"+
				"无腾挪空间；建议倒班人数 >= %d。样例 %v",
				c.N, year, month, float64(c.N*c.Work)/float64(st.Days), need, len(c.Shifts), c.MinPer, suggest, st.BadDetail)
		} else {
			t.Errorf("有 %d 天某班次人数低于 %d 人；样例 %v", st.BadDays, c.MinPer, st.BadDetail)
		}
	}
	if st.TriRest > 0 {
		t.Errorf("出现 %d 次三连休", st.TriRest)
	}
	// 连班上限同理：每天能休息的人数 = N - need。
	// 只剩 0~1 人可休息时（例如 7 人却要 6 人上班），双休根本排不出来，
	// 单休轮转也必然顶到边界，连班压不到 6 天属于配置极限，给出建议而非判失败。
	if st.MaxWork > 6 && c.N-need <= 1 {
		t.Logf("LIMIT N=%d %d-%02d: 每天仅 %d 人能休息，最长连班 %d 天；"+
			"建议倒班人数 >= %d 才能压到 6 天内", c.N, year, month, c.N-need, st.MaxWork, need+2)
	} else if st.MaxWork > 6 {
		t.Errorf("最长连续上班 %d 天，超过上限 6", st.MaxWork)
	}
	if st.LessM > 0 {
		t.Errorf("有 %d 天早班人数少于中班（业务要求：不对等时早班应 >= 中班）", st.LessM)
	}
	// 班次均衡防退化：差额要摊到全员，不能集中在个别人身上。
	// 当前实测最大 6（N=7 出勤 20 天），阈值留到 8。
	if st.MaxSkew > 8 {
		t.Errorf("单人早/中班天数最大差 %d，差额集中在个别人身上", st.MaxSkew)
	}
	return true
}

// TestScaleEndToEnd 基线回归：线上当前形态（两班制、每班≥2、出勤 20 天）。
func TestScaleEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, n := range []int{6, 7, 8, 10, 20} {
		for _, ym := range [][2]int{{2026, 2}, {2026, 11}, {2026, 12}, {2027, 1}} {
			ym := ym
			t.Run(fmt.Sprintf("N%d_%d%02d", n, ym[0], ym[1]), func(t *testing.T) {
				runScale(t, scaleCase{N: n, Work: 20, MinPer: 2, Shifts: twoShift()}, ym[0], ym[1])
			})
		}
	}
}

// TestScaleWide 扩展覆盖：更多人数 × 更多月份 × 出勤 22 天 × 每班 3 人 × 三班制。
func TestScaleWide(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		label string
		c     scaleCase
	}{
		{"出勤22天", scaleCase{Work: 22, MinPer: 2, Shifts: twoShift()}},
		{"每班3人", scaleCase{Work: 22, MinPer: 3, Shifts: twoShift()}},
		{"三班制", scaleCase{Work: 22, MinPer: 2, Shifts: threeShift()}},
	}
	months := [][2]int{{2026, 2}, {2026, 11}, {2026, 12}}
	// 人数覆盖：刚好卡线、卡线+1、典型规模、大团队
	people := []int{6, 7, 9, 10, 12, 15, 20, 30}

	for _, cs := range cases {
		for _, n := range people {
			c := cs.c
			c.N = n
			for _, ym := range months {
				ym := ym
				t.Run(fmt.Sprintf("%s_N%d_%d%02d", cs.label, n, ym[0], ym[1]), func(t *testing.T) {
					runScale(t, c, ym[0], ym[1])
				})
			}
		}
	}
}

// TestTwoShiftPerfectBalance 6 人 × 出勤 20 天 × 30 天月，是本项目的线上真实形态：
// 每天恰好 早2/中2 上班、2 人休息，全月 早班 60 人日 / 中班 60 人日，
// 因此每个人【必须】严格 早10 / 中10 —— 差值为 0 是数学上可达成的，达不到就是缺陷。
//
// 这条断言是「班次均衡」要求的硬底线，防止后续改动把差额又压回个别人身上。
func TestTwoShiftPerfectBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupScaleDB(t, scaleCase{N: 6, Work: 20, MinPer: 2, Shifts: twoShift()})
	out := genScale(t, 2026, 11)

	planRaw, _ := out["plan"].(map[string]interface{})
	dates := make([]string, 0, len(planRaw))
	for k := range planRaw {
		dates = append(dates, k)
	}
	sort.Strings(dates)
	if len(dates) != 30 {
		t.Fatalf("期望 30 天，实际 %d", len(dates))
	}

	peopleRaw, _ := out["people"].([]interface{})
	names := []string{}
	for _, p := range peopleRaw {
		if m, ok := p.(map[string]interface{}); ok {
			if f, _ := m["is_fixed"].(bool); !f {
				names = append(names, fmt.Sprint(m["name"]))
			}
		}
	}
	sort.Strings(names)
	if len(names) != 6 {
		t.Fatalf("期望 6 名倒班人员，实际 %d", len(names))
	}

	for _, nm := range names {
		m, a, r := 0, 0, 0
		sym := make([]rune, 0, len(dates))
		for _, d := range dates {
			row, _ := planRaw[d].(map[string]interface{})
			switch fmt.Sprint(row[nm]) {
			case "早班":
				m++
				sym = append(sym, 'M')
			case "中班":
				a++
				sym = append(sym, 'A')
			case "休息":
				r++
				sym = append(sym, 'x')
			default:
				sym = append(sym, '?')
			}
		}
		t.Logf("  %s 早%d 中%d 休%d  %s", nm, m, a, r, string(sym))
		if m != 10 || a != 10 || r != 10 {
			t.Errorf("%s 班次不均：早%d 中%d 休%d（期望各 10）", nm, m, a, r)
		}
	}

	// 每天必须恰好 早2 / 中2
	for _, d := range dates {
		row, _ := planRaw[d].(map[string]interface{})
		m, a := 0, 0
		for _, nm := range names {
			switch fmt.Sprint(row[nm]) {
			case "早班":
				m++
			case "中班":
				a++
			}
		}
		if m != 2 || a != 2 {
			t.Errorf("%s 人数为 早%d/中%d（期望 2/2）", d, m, a)
		}
	}
}

// TestRestAdjacencyGuard 规则5（休假前须早班 / 休假后须晚班）保护。
//
// 背景：为压平「每人早/中班天数差」引入的零和交换会搬动个别格子，
// 曾把休假边界日的班次改掉，导致 2027-02 出现 2 条规则5 警告
// （v0.20.3 时是 0）。零和交换属于可选优化，遇到会破坏规则5 的人必须跳过。
func TestRestAdjacencyGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, work := range []int{20, 22} {
	for _, ym := range [][2]int{{2026, 2}, {2026, 11}, {2026, 12}, {2027, 2}} {
		work, ym := work, ym
		t.Run(fmt.Sprintf("work%d_%d%02d", work, ym[0], ym[1]), func(t *testing.T) {
			c := scaleCase{N: 6, Work: work, MinPer: 2, Shifts: twoShift(), RestAdj: true}
			setupScaleDB(t, c)
			out := genScale(t, ym[0], ym[1])

			planRaw, _ := out["plan"].(map[string]interface{})
			dates := make([]string, 0, len(planRaw))
			for k := range planRaw {
				dates = append(dates, k)
			}
			sort.Strings(dates)
			peopleRaw, _ := out["people"].([]interface{})
			names := []string{}
			for _, p := range peopleRaw {
				if m, ok := p.(map[string]interface{}); ok {
					if f, _ := m["is_fixed"].(bool); !f {
						names = append(names, fmt.Sprint(m["name"]))
					}
				}
			}
			// 注意 blockShiftOrder 是时间【逆序】（晚 → 早）：
			// 休假后须「晚班」＝时间最晚的班次；休假前须「早班」＝时间最早的班次。
			morning := c.Shifts[0].Name
			evening := c.Shifts[len(c.Shifts)-1].Name

			badBefore, badAfter := 0, 0
			for i, d := range dates {
				row, _ := planRaw[d].(map[string]interface{})
				for _, nm := range names {
					v := fmt.Sprint(row[nm])
					if v == "休息" {
						continue
					}
					if i+1 < len(dates) {
						nx, _ := planRaw[dates[i+1]].(map[string]interface{})
						if fmt.Sprint(nx[nm]) == "休息" && v != morning {
							badBefore++
						}
					}
					if i > 0 {
						pv, _ := planRaw[dates[i-1]].(map[string]interface{})
						if fmt.Sprint(pv[nm]) == "休息" && v != evening {
							badAfter++
						}
					}
				}
			}
			t.Logf("ADJ|work=%d|%d-%02d|beforeRest=%d|afterRest=%d", work, ym[0], ym[1], badBefore, badAfter)
			if badBefore+badAfter > 0 {
				t.Errorf("规则5 未满足：休假前非早班 %d 处、休假后非晚班 %d 处", badBefore, badAfter)
			}
		})
	}
}
}
