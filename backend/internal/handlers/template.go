package handlers

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// ===================== 固定模板（xlsx） =====================

// DownloadScheduleTemplateXLSX GET /api/templates/schedule-template
// 班表固定模板：人×日期矩阵（早/中/晚/夜/休），Sheet「信息」标注部门/年份/月份
func DownloadScheduleTemplateXLSX(c *gin.Context) {
	f := excelize.NewFile()
	sheet := "班表"
	f.SetSheetName("Sheet1", sheet)

	// 表头：序号 姓名 中免工号 1..31
	heads := []string{"序号", "姓名", "中免工号"}
	f.SetCellValue(sheet, "A1", "X月份班表（固定模板）")
	f.SetCellValue(sheet, "A2", heads[0])
	f.SetCellValue(sheet, "B2", heads[1])
	f.SetCellValue(sheet, "C2", heads[2])
	for d := 1; d <= 31; d++ {
		col, _ := excelize.CoordinatesToCellName(3+d, 2)
		f.SetCellValue(sheet, col, d)
	}
	// 示例行（早/中/休）
	exRow := []string{"1", "张三", "1001", "早", "休", "中", "早", "早", "中", "休"}
	for i, v := range exRow {
		col, _ := excelize.CoordinatesToCellName(1+i, 3)
		f.SetCellValue(sheet, col, v)
	}
	// 说明行
	f.SetCellValue(sheet, "A4", "填写说明：每个员工一行；第 1~31 列填当天班次：早/中/晚/夜/休（可写「早班/中班/休息」）。姓名与工号必填，工号须与系统一致（新员工需先在设置-人员里添加）。")
	f.SetCellValue(sheet, "A5", "Sheet「信息」里填写部门与年月；或把文件名命名为「部门2026年9月份班表」。上传后按此部门/年月导入。")

	// 信息 sheet
	info := "信息"
	f.NewSheet(info)
	rows := [][2]string{{"部门", ""}, {"仓库", ""}, {"年份", "2026"}, {"月份", "09"}}
	for i, r := range rows {
		colA, _ := excelize.CoordinatesToCellName(1, i+1)
		colB, _ := excelize.CoordinatesToCellName(2, i+1)
		f.SetCellValue(info, colA, r[0])
		f.SetCellValue(info, colB, r[1])
	}
	f.SetColWidth(sheet, "A", "A", 6)
	f.SetColWidth(sheet, "B", "B", 12)
	f.SetColWidth(sheet, "C", "C", 12)

	writeXLSX(c, f, "班表模板.xlsx")
}

// DownloadTaskTemplateXLSX GET /api/templates/task-template
// 任务固定模板：Sheet「每日工作内容」+「月度工作内容」，对应交接表格式
func DownloadTaskTemplateXLSX(c *gin.Context) {
	f := excelize.NewFile()
	daily := "每日工作内容"
	f.SetSheetName("Sheet1", daily)
	f.SetCellValue(daily, "A1", "每日工作内容（固定模板）：每天固定时间要做的任务")
	// 表头：时间 时段工作 负责班次
	f.SetCellValue(daily, "A2", "时间")
	f.SetCellValue(daily, "B2", "时段工作")
	f.SetCellValue(daily, "C2", "负责班次")
	dailySample := [][3]string{
		{"09:00", "发数据（每日作业数据）", "早班"},
		{"10:00", "巡检仓库", "早班"},
		{"14:00", "盘点（抽盘、循环盘）", "中班"},
		{"20:00", "与外包公司核对当天作业数据", "中班"},
	}
	for i, r := range dailySample {
		for j, v := range r {
			col, _ := excelize.CoordinatesToCellName(1+j, 3+i)
			f.SetCellValue(daily, col, v)
		}
	}
	f.SetCellValue(daily, "A7", "填写说明：时间格式 HH:MM；负责班次填 早班/中班/晚班/早晚/全员（交接班按中班导入）。")

	monthly := "月度工作内容"
	f.NewSheet(monthly)
	f.SetCellValue(monthly, "A1", "月度工作内容（固定模板）：每月固定日期要做的任务")
	f.SetCellValue(monthly, "A2", "日期")
	f.SetCellValue(monthly, "B2", "业务主题")
	f.SetCellValue(monthly, "C2", "工作内容")
	f.SetCellValue(monthly, "D2", "处理人")
	monthlySample := [][4]string{
		{"1号", "预订仓超时订单", "超时未提及退货未回订单，通过邮件形式发送至各负责人", ""},
		{"10号", "精品类物料汇总", "精品物料缺口每月10日前邮件反馈各负责人", ""},
		{"20号", "精品送修", "有需送修20号发邮件", ""},
	}
	for i, r := range monthlySample {
		for j, v := range r {
			col, _ := excelize.CoordinatesToCellName(1+j, 3+i)
			f.SetCellValue(monthly, col, v)
		}
	}
	f.SetCellValue(monthly, "A6", "填写说明：日期填「N号」；处理人填系统内员工姓名（可空）；工作内容为任务备注。")

	info := "信息"
	f.NewSheet(info)
	rows := [][2]string{{"部门", ""}, {"仓库", ""}, {"年份", "2026"}, {"月份", "09"}}
	for i, r := range rows {
		colA, _ := excelize.CoordinatesToCellName(1, i+1)
		colB, _ := excelize.CoordinatesToCellName(2, i+1)
		f.SetCellValue(info, colA, r[0])
		f.SetCellValue(info, colB, r[1])
	}
	writeXLSX(c, f, "任务模板.xlsx")
}

func writeXLSX(c *gin.Context, f *excelize.File, filename string) {
	buf, err := f.WriteToBuffer()
	if err != nil {
		c.JSON(500, gin.H{"error": "模板生成失败"})
		return
	}
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", filename))
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

// ===================== xlsx 班表矩阵解析 =====================

// shiftMap 班次简写 → 系统班次
func shiftMap(v string) (string, bool) {
	switch strings.TrimSpace(v) {
	case "早", "早班":
		return "早班", true
	case "中", "中班":
		return "中班", true
	case "晚", "晚班":
		return "晚班", true
	case "夜", "夜班":
		return "夜班", true
	case "休", "休息":
		return "休息", true
	case "交接班":
		return "中班", true
	case "早晚", "早晚班":
		return "早晚", true
	case "全员", "所有人":
		return "全员", true
	// 交接表里常见「早、中」表示早班与中班都要执行 → 全员
	case "早、中", "早,中", "早中", "早班、中班", "早班,中班", "早中班":
		return "全员", true
	}
	return "", false
}

// xlsxMeta 从 Sheet「信息」或文件名解析部门/年月。
// 「信息」sheet 支持两种写法：
//   - 纵向（模板格式）：A3=年份 B3=2026，A4=月份 B4=08
//   - 横向（用户交接表常见）：首行 部门|仓库|年份|月份，次行是对应值
func xlsxMeta(f *excelize.File, filename string) (year int, month int, ok bool) {
	year, month = 0, 0
	if f != nil {
		if idx, err := f.GetSheetIndex("信息"); err == nil && idx >= 0 {
			// 1) 纵向：B3 年份 / B4 月份
			val := func(cell string) string {
				v, _ := f.GetCellValue("信息", cell)
				return strings.TrimSpace(v)
			}
			y, _ := strconv.Atoi(val("B3"))
			m, _ := strconv.Atoi(val("B4"))
			if y > 2000 && m >= 1 && m <= 12 {
				return y, m, true
			}
			// 2) 横向：找「年份」「月份」表头，取右侧或下方的值
			rows, _ := f.GetRows("信息")
			cellAt := func(ri, ci int) string {
				if ri >= len(rows) || ci >= len(rows[ri]) {
					return ""
				}
				return strings.TrimSpace(rows[ri][ci])
			}
			y2, m2 := 0, 0
			for i := 0; i < len(rows) && i < 4; i++ {
				for j := 0; j < len(rows[i]); j++ {
					h := cellAt(i, j)
					if h == "" {
						continue
					}
					var target *int
					switch {
					case strings.Contains(h, "年份"):
						target = &y2
					case strings.Contains(h, "月份"):
						target = &m2
					default:
						continue
					}
					// 优先取右侧单元格，其次取下方单元格
					for _, cand := range [][2]int{{i, j + 1}, {i + 1, j}} {
						n, err := strconv.Atoi(cellAt(cand[0], cand[1]))
						if err == nil {
							*target = n
							break
						}
					}
				}
			}
			if y2 > 2000 && m2 >= 1 && m2 <= 12 {
				return y2, m2, true
			}
		}
	}
	// 文件名形如「三亚预订仓2026年9月份班表」「xxx 2026-09」
	re := regexp.MustCompile(`(20\d{2})\s*年\s*(\d{1,2})\s*月`)
	if m := re.FindStringSubmatch(filename); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if y > 2000 && mo >= 1 && mo <= 12 {
			return y, mo, true
		}
	}
	return 0, 0, false
}

// importSchedulesFromXLSX 解析「人×日期」矩阵班表（固定模板格式）
func importSchedulesFromXLSX(c *gin.Context, f multipart.File, cl *models.Claims, deptID uint, filename string) (int, int, []string, []string, string) {
	xf, err := excelize.OpenReader(f)
	if err != nil {
		return 0, 0, []string{"xlsx 解析失败: " + err.Error()}, nil, ""
	}
	defer xf.Close()

	year, month, ok := xlsxMeta(xf, filename)
	if !ok {
		return 0, 0, []string{"无法识别年份/月份：请在 Sheet「信息」填写，或把文件名命名为「…2026年9月份班表」"}, nil, ""
	}

	sheet := "班表"
	if idx, _ := xf.GetSheetIndex(sheet); idx < 0 {
		sheet = xf.GetSheetName(0)
	}
	rows, err := xf.GetRows(sheet)
	if err != nil {
		return 0, 0, []string{"读取班表 sheet 失败: " + err.Error()}, nil, ""
	}
	// 定位表头行（含「姓名」）
	headRow, nameCol, dayCols := -1, -1, map[int]int{}
	for i, r := range rows {
		if len(r) < 4 {
			continue
		}
		for j, cell := range r {
			t := strings.TrimSpace(cell)
			if t == "姓名" {
				headRow, nameCol = i, j
			} else if d, err := strconv.Atoi(t); err == nil && d >= 1 && d <= 31 && headRow == i {
				dayCols[j] = d
			}
		}
		if headRow == i {
			break
		}
	}
	if headRow < 0 || nameCol < 0 || len(dayCols) == 0 {
		return 0, 0, []string{"模板格式不对：未找到「姓名」列与 1~31 日表头"}, nil, ""
	}

	created, failed, skipped := 0, 0, 0
	var errs []string
	impNames := map[string]bool{}
	for i := headRow + 1; i < len(rows); i++ {
		r := rows[i]
		if nameCol >= len(r) {
			continue
		}
		name := strings.TrimSpace(r[nameCol])
		if name == "" || name == "序号" {
			continue
		}
		for col, day := range dayCols {
			if col >= len(r) {
				continue
			}
			raw := strings.TrimSpace(r[col])
			if raw == "" {
				continue
			}
			shift, mok := shiftMap(raw)
			if !mok {
				failed++
				errs = append(errs, fmt.Sprintf("%s %d日[%s]无法识别，已跳过", name, day, raw))
				continue
			}
			date := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
			impNames[name] = true
			// 一人一天只在一个班次：导入即替换当天该人其他班次的旧记录
			replacePersonShifts(date, deptID, []string{name}, 0)
			peopleJSON, _ := json.Marshal([]string{name})
			// 去重：同日期+班次+人员已存在则跳过
			var exists int64
			db.DB.Model(&models.Schedule{}).Where("date = ? AND shift = ? AND people = ?", date, shift, string(peopleJSON)).Count(&exists)
			if exists > 0 {
				skipped++
				continue
			}
			s := models.Schedule{Date: date, Shift: shift, People: string(peopleJSON), DeptID: deptID}
			if err := db.DB.Create(&s).Error; err != nil {
				failed++
				errs = append(errs, fmt.Sprintf("%s %s 创建失败: %s", date, name, err.Error()))
				continue
			}
			created++
		}
	}
	if created > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("Excel 导入班表 %d 条（%04d-%02d）", created, year, month))
		names := make([]string, 0, len(impNames))
		for n := range impNames {
			names = append(names, n)
		}
		notifyPeopleByName(names, "schedule", "班表更新",
			fmt.Sprintf("%s 于 %s 批量更新了你 %04d-%02d 的班表（%d 条）", claimsName(cl), time.Now().Format("2006-01-02 15:04"), year, month, created),
			cl.UserID, claimsName(cl))
	}
	return created, failed, append(errs, fmt.Sprintf("已跳过重复 %d 条", skipped)), unknownScheduleNames(impNames), fmt.Sprintf("%04d-%02d", year, month)
}

// ===================== xlsx 任务解析（按表头匹配，兼容多余列/乱序） =====================

// taskHeadAlias 表头别名 → 规范字段；"skip" 表示该列不导入
var taskHeadAlias = map[string]string{
	"序号": "skip", "编号": "skip", "序": "skip", "no": "skip",
	"时间": "time", "执行时间": "time", "时间点": "time", "时段": "time", "开始时间": "time",
	"日期": "day", "几号": "day", "每月日期": "day",
	"时段工作": "title", "工作事项": "title", "任务内容": "title", "任务名称": "title",
	"任务": "title", "标题": "title", "事项": "title", "项目": "title",
	"业务主题": "subject", "主题": "subject",
	"工作内容": "content", "内容": "content", "具体工作": "content", "工作描述": "content",
	"负责班次": "shift", "班次": "shift", "责任班次": "shift", "执行班次": "shift",
	"处理人": "assignee", "负责人": "assignee", "责任人": "assignee", "跟进人": "assignee",
	"备注": "note", "说明": "note", "要求": "note", "备注说明": "note",
}

// taskColMap 列号 → 规范字段
type taskColMap map[int]string

// normHead 归一化表头文本：去空格、去尾部的 * 与冒号、全角转半角
func normHead(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "*＊:：")
	s = strings.NewReplacer(" ", "", "　", "", "（", "(", "）", ")").Replace(s)
	return strings.ToLower(s)
}

// findSheetByKey 按关键字模糊匹配 sheet 名
func findSheetByKey(xf *excelize.File, keys ...string) string {
	for _, name := range xf.GetSheetList() {
		for _, k := range keys {
			if strings.Contains(name, k) {
				return name
			}
		}
	}
	return ""
}

// findTaskHead 在前 maxScan 行中找表头行：命中已知别名最多且不少于 2 个的行
func findTaskHead(rows [][]string, maxScan int) (int, taskColMap) {
	best, bestHits, bestMap := -1, 0, taskColMap{}
	for i := 0; i < len(rows) && i < maxScan; i++ {
		m, hits, used := taskColMap{}, 0, map[string]bool{}
		for j, cell := range rows[i] {
			f, ok := taskHeadAlias[normHead(cell)]
			if !ok {
				continue
			}
			if f != "skip" && used[f] {
				// 同一字段出现多列（如「处理人」+「跟进人」）：只认最左一列，其余忽略
				m[j] = "skip"
				continue
			}
			if f != "skip" {
				used[f] = true
				hits++
			}
			m[j] = f
		}
		if hits > bestHits {
			best, bestHits, bestMap = i, hits, m
		}
	}
	if bestHits < 2 {
		return -1, nil
	}
	return best, bestMap
}

// isSeqCol 判断某列是否为连续递增的序号列（1,2,3… 或 2,3,4… 均可）
func isSeqCol(rows [][]string, j int) bool {
	vals := make([]int, 0, len(rows))
	for _, r := range rows {
		if j >= len(r) {
			continue
		}
		v := strings.TrimSpace(r[j])
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return false
		}
		vals = append(vals, n)
	}
	if len(vals) < 2 {
		return false
	}
	for i := 1; i < len(vals); i++ {
		if vals[i] != vals[i-1]+1 {
			return false
		}
	}
	return true
}

// buildFallbackMap 无表头时按固定列序兜底；首列若是序号则整体右移一列
func buildFallbackMap(rows [][]string, cols []string) taskColMap {
	m := taskColMap{}
	offset := 0
	if isSeqCol(rows, 0) {
		offset = 1
		m[0] = "skip" // 首列是序号，明确标记为忽略
	}
	for i, c := range cols {
		m[i+offset] = c
	}
	return m
}

// cellAt 按规范字段取单元格值
func cellAt(r []string, m taskColMap, key string) string {
	for j, f := range m {
		if f == key && j < len(r) {
			return strings.TrimSpace(r[j])
		}
	}
	return ""
}

// isCommentRow 识别空白行与模板尾部的「填写说明」行。
// 判定规则（顺序敏感）：
//  1. 整行无有效内容 → 空行，跳过
//  2. 仅首列有值，且是长文本或含「说明」→ 说明行，跳过
//  3. 其它情况一律视为数据行
//
// 注意：首列为空但其它列有值的行是正常数据。很多交接表 A 列整列留空（数据从 B 列开始），
// 早期版本把这类行全部误判为说明行，导致「每日任务导入 0 条」。
func isCommentRow(r []string) bool {
	nonEmpty, first, other := 0, "", ""
	for i, v := range r {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		nonEmpty++
		if i == 0 {
			first = v
		} else if other == "" {
			other = v
		}
	}
	if nonEmpty == 0 {
		return true
	}
	if other == "" {
		return len([]rune(first)) > 8 || strings.Contains(first, "说明")
	}
	return false
}

// isSignRow 识别交接表尾部的审批/签字行，如「>>>>>>此栏由组长签字确认：」
func isSignRow(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, ">>") || strings.HasPrefix(s, "<<") || strings.HasPrefix(s, "--") {
		return true
	}
	return strings.Contains(s, "签字") || strings.Contains(s, "确认：") || strings.Contains(s, "确认:")
}

// readTaskSheet 定位 sheet 并解析出数据行 + 列映射；找不到 sheet 返回 ok=false
func readTaskSheet(xf *excelize.File, keys []string, fallback []string) (data [][]string, colMap taskColMap, headRow int, ok bool) {
	sheet := findSheetByKey(xf, keys...)
	if sheet == "" {
		return nil, nil, -1, false
	}
	raw, err := xf.GetRows(sheet)
	if err != nil {
		return nil, nil, -1, false
	}
	scan := 6
	if len(raw) < scan {
		scan = len(raw)
	}
	if hr, m := findTaskHead(raw, scan); hr >= 0 {
		return raw[hr+1:], m, hr, true
	}
	// 兜底：仅当首行是说明性标题时才跳过，避免误删第一行数据
	body := raw
	if len(body) > 0 && isCommentRow(body[0]) {
		body = body[1:]
	}
	return body, buildFallbackMap(body, fallback), -1, true
}

// describeColMap 生成「第N列(表头) → 字段」的可读映射，供导入结果回显
var taskFieldLabel = map[string]string{
	"time": "时间", "day": "日期", "title": "任务内容", "subject": "业务主题",
	"content": "工作内容", "shift": "负责班次", "assignee": "处理人", "note": "备注", "skip": "（忽略）",
}

func describeColMap(m taskColMap) string {
	if len(m) == 0 {
		return ""
	}
	cols := make([]int, 0, len(m))
	for j := range m {
		cols = append(cols, j)
	}
	sort.Ints(cols)
	parts := make([]string, 0, len(cols))
	for _, j := range cols {
		name, _ := excelize.ColumnNumberToName(j + 1)
		label := taskFieldLabel[m[j]]
		if label == "" {
			label = m[j]
		}
		parts = append(parts, fmt.Sprintf("%s列→%s", name, label))
	}
	return strings.Join(parts, "，")
}

// importTasksFromXLSX 解析任务表：每日工作内容 + 月度工作内容
// 核心：按表头名匹配列，不再依赖固定列号，可兼容多余列（如序号）与不同列序
func importTasksFromXLSX(c *gin.Context, f multipart.File, cl *models.Claims, deptID uint, filename string) (int, int, []string, string) {
	xf, err := excelize.OpenReader(f)
	if err != nil {
		return 0, 0, []string{"xlsx 解析失败: " + err.Error()}, ""
	}
	defer xf.Close()

	year, month, metaOK := xlsxMeta(xf, filename)
	created, failed := 0, 0
	var errs []string
	var maps []string
	var samples []string
	super := cl.Role == models.RoleSuperAdmin

	firstNonEmpty := func(vals ...string) string {
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	// clip 截断长文本，避免样例回显过长
	clip := func(s string) string {
		rs := []rune(s)
		if len(rs) > 24 {
			return string(rs[:24]) + "…"
		}
		return s
	}

	// ---------- 每日工作内容：时间 / 时段工作(任务内容) / 负责班次 / 备注 ----------
	if data, colMap, _, ok := readTaskSheet(xf, []string{"每日", "每天", "日工作"}, []string{"time", "title", "shift"}); ok {
		maps = append(maps, "每日工作内容："+describeColMap(colMap))
		lastTime, dup := "", 0 // 合并单元格造成时间空行时沿用上一行；同部门同标题同时间视为重复
		for _, r := range data {
			if isCommentRow(r) {
				continue
			}
			title := firstNonEmpty(cellAt(r, colMap, "title"), cellAt(r, colMap, "content"))
			if title == "" || isSignRow(title) {
				continue
			}
			tm := normalizeClock(cellAt(r, colMap, "time"))
			if tm == "" {
				tm = lastTime // 合并单元格：同一时段的后续工作沿用上面的时间
			}
			if tm != "" {
				lastTime = tm
			}
			t := models.Task{
				Title:    title,
				Type:     models.TaskTypeDaily,
				Note:     firstNonEmpty(cellAt(r, colMap, "note")),
				DeptID:   deptID,
				Status:   models.TaskStatusTodo,
				Priority: "medium",
				Shift:    "全员",
				Time:     tm,
			}
			if s, mok := shiftMap(cellAt(r, colMap, "shift")); mok {
				t.Shift = s
			}
			var exists int64
			db.DB.Model(&models.Task{}).Where("dept_id = ? AND type = ? AND title = ? AND time = ?", deptID, models.TaskTypeDaily, t.Title, t.Time).Count(&exists)
			if exists > 0 {
				dup++
				continue
			}
			if err := db.DB.Create(&t).Error; err != nil {
				failed++
				errs = append(errs, "每日["+title+"]失败: "+err.Error())
				continue
			}
			if created+failed < 3 {
				samples = append(samples, fmt.Sprintf("时间=%s 内容=%s 班次=%s", t.Time, clip(t.Title), t.Shift))
			}
			created++
		}
		if dup > 0 {
			maps = append(maps, fmt.Sprintf("每日：已跳过重复 %d 条", dup))
		}
	}

	// ---------- 月度工作内容：日期 / 业务主题(任务内容) / 工作内容(备注) / 处理人 ----------
	if data, colMap, _, ok := readTaskSheet(xf, []string{"月度", "每月", "月工作"}, []string{"day", "subject", "content", "assignee"}); ok {
		maps = append(maps, "月度工作内容："+describeColMap(colMap))
		if len(data) > 0 && !metaOK {
			return created, failed, []string{"月度任务需要年份/月份：请在 Sheet「信息」填写，或把文件名命名为「…2026年9月份…」"}, strings.Join(maps, " | ")
		}
		dup := 0
		for _, r := range data {
			if isCommentRow(r) {
				continue
			}
			subject, content := cellAt(r, colMap, "subject"), cellAt(r, colMap, "content")
			title, note := subject, content
			if title == "" {
				title, note = content, firstNonEmpty(cellAt(r, colMap, "note"))
			} else if note == "" {
				note = firstNonEmpty(cellAt(r, colMap, "note"))
			}
			if title == "" || isSignRow(title) {
				continue
			}
			day := parseMonthDay(cellAt(r, colMap, "day"))
			if day < 1 || day > 31 {
				failed++
				errs = append(errs, "月度["+title+"]日期格式不对（应为「N号」）")
				continue
			}
			t := models.Task{
				Title:    title,
				Type:     models.TaskTypeMonthly,
				Note:     note,
				Assignee: cellAt(r, colMap, "assignee"),
				DeptID:   deptID,
				Status:   models.TaskStatusTodo,
				Priority: "medium",
				Shift:    "全员",
				// 月度任务默认当天 09:00 截止：上班后处理完即可，不必凌晨 0 点就判逾期
				Deadline: fmt.Sprintf("%04d-%02d-%02dT%s", year, month, day, monthlyDueTime),
			}
			if s, mok := shiftMap(cellAt(r, colMap, "shift")); mok {
				t.Shift = s
			}
			t.AssigneeID = resolveAssigneeID(t.Assignee, deptID, super)
			var exists int64
			db.DB.Model(&models.Task{}).Where("dept_id = ? AND type = ? AND title = ? AND deadline = ?", deptID, models.TaskTypeMonthly, t.Title, t.Deadline).Count(&exists)
			if exists > 0 {
				dup++
				continue
			}
			if err := db.DB.Create(&t).Error; err != nil {
				failed++
				errs = append(errs, "月度["+title+"]失败: "+err.Error())
				continue
			}
			if len(samples) < 6 && created+failed < 3 {
				samples = append(samples, fmt.Sprintf("截止=%s 内容=%s 备注=%s", t.Deadline[:10], clip(t.Title), clip(t.Note)))
			}
			created++
		}
		if dup > 0 {
			maps = append(maps, fmt.Sprintf("月度：已跳过重复 %d 条", dup))
		}
	}

	if created == 0 && failed == 0 && len(maps) == 0 {
		errs = append(errs, "未识别到工作表：请保留名为「每日工作内容」和/或「月度工作内容」的 Sheet")
	}
	if created > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("Excel 导入任务 %d 条", created))
	}

	report := strings.Join(maps, "\n")
	if len(samples) > 0 {
		report += "\n解析示例：" + strings.Join(samples, "；")
	}
	return created, failed, errs, strings.TrimSpace(report)
}

// parseMonthDay 解析「1号/15号/31号」→ 数字
func parseMonthDay(s string) int {
	s = strings.TrimSpace(strings.TrimSuffix(s, "号"))
	s = strings.TrimSuffix(s, "日")
	n, _ := strconv.Atoi(s)
	return n
}

// 兼容文件名后缀判断
func isXLSX(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".xlsx" || ext == ".xlsm"
}
