package handlers

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildSeqTaskXLSX 模拟用户实际表格：比模板多一列「序号」
// 每日工作内容：序号 | 时间 | 时段工作 | 负责班次
// 月度工作内容：序号 | 日期 | 业务主题 | 工作内容 | 处理人
func buildSeqTaskXLSX(t *testing.T) *excelize.File {
	f := excelize.NewFile()

	daily := "每日工作内容"
	if err := f.SetSheetName("Sheet1", daily); err != nil {
		t.Fatal(err)
	}
	f.SetCellValue(daily, "A1", "每日工作内容（交接表）")
	for j, h := range []string{"序号", "时间", "时段工作", "负责班次"} {
		c, _ := excelize.CoordinatesToCellName(1+j, 2)
		f.SetCellValue(daily, c, h)
	}
	dailyRows := [][4]string{
		{"1", "09:00", "发数据（每日作业数据）", "早班"},
		{"2", "10:00", "巡检仓库", "早班"},
		{"3", "14:00", "盘点（抽盘、循环盘）", "中班"},
	}
	for i, r := range dailyRows {
		for j, v := range r {
			c, _ := excelize.CoordinatesToCellName(1+j, 3+i)
			f.SetCellValue(daily, c, v)
		}
	}
	f.SetCellValue(daily, "A6", "填写说明：时间格式 HH:MM；负责班次填 早班/中班/晚班/早晚/全员。")

	monthly := "月度工作内容"
	f.NewSheet(monthly)
	f.SetCellValue(monthly, "A1", "月度工作内容（交接表）")
	for j, h := range []string{"序号", "日期", "业务主题", "工作内容", "处理人", "跟进人"} {
		c, _ := excelize.CoordinatesToCellName(1+j, 2)
		f.SetCellValue(monthly, c, h)
	}
	monthlyRows := [][5]string{
		{"1", "1号", "预订仓超时订单", "超时未提及退货未回订单，通过邮件形式发送至各负责人", "张三"},
		{"2", "10号", "精品类物料汇总", "精品物料缺口每月10日前邮件反馈各负责人", ""},
		{"3", "20号", "精品送修", "有需送修20号发邮件", ""},
	}
	for i, r := range monthlyRows {
		for j, v := range r {
			c, _ := excelize.CoordinatesToCellName(1+j, 3+i)
			f.SetCellValue(monthly, c, v)
		}
	}
	f.SetCellValue(monthly, "A6", "填写说明：日期填「N号」；处理人填系统内员工姓名（可空）。")

	info := "信息"
	f.NewSheet(info)
	f.SetCellValue(info, "A1", "部门")
	f.SetCellValue(info, "A2", "仓库")
	f.SetCellValue(info, "A3", "年份")
	f.SetCellValue(info, "B3", "2026")
	f.SetCellValue(info, "A4", "月份")
	f.SetCellValue(info, "B4", "09")
	return f
}

func TestTaskDailyHeadMatch(t *testing.T) {
	f := buildSeqTaskXLSX(t)
	data, cm, headRow, ok := readTaskSheet(f, []string{"每日", "每天", "日工作"}, []string{"time", "title", "shift"})
	if !ok {
		t.Fatal("未定位到每日工作内容 sheet")
	}
	if headRow != 1 {
		t.Fatalf("表头行应为第 2 行（索引 1），实际 %d", headRow)
	}
	want := map[int]string{0: "skip", 1: "time", 2: "title", 3: "shift"}
	for j, w := range want {
		if cm[j] != w {
			t.Errorf("第 %d 列应映射为 %s，实际 %s", j+1, w, cm[j])
		}
	}
	if got := cellAt(data[0], cm, "time"); got != "09:00" {
		t.Errorf("时间列取值错误：%q", got)
	}
	if got := cellAt(data[0], cm, "title"); got != "发数据（每日作业数据）" {
		t.Errorf("任务内容列取值错误：%q", got)
	}
	if got := cellAt(data[0], cm, "shift"); got != "早班" {
		t.Errorf("负责班次列取值错误：%q", got)
	}
	if !isCommentRow(data[len(data)-1]) {
		t.Error("尾部「填写说明」行应被识别为注释行")
	}
}

func TestTaskMonthlyHeadMatch(t *testing.T) {
	f := buildSeqTaskXLSX(t)
	data, cm, _, ok := readTaskSheet(f, []string{"月度", "每月", "月工作"}, []string{"day", "subject", "content", "assignee"})
	if !ok {
		t.Fatal("未定位到月度工作内容 sheet")
	}
	want := map[int]string{0: "skip", 1: "day", 2: "subject", 3: "content", 4: "assignee"}
	for j, w := range want {
		if cm[j] != w {
			t.Errorf("第 %d 列应映射为 %s，实际 %s", j+1, w, cm[j])
		}
	}
	if got := cellAt(data[0], cm, "day"); got != "1号" {
		t.Errorf("日期列取值错误：%q", got)
	}
	if got := cellAt(data[0], cm, "subject"); got != "预订仓超时订单" {
		t.Errorf("业务主题列取值错误：%q", got)
	}
	if got := cellAt(data[0], cm, "content"); got != "超时未提及退货未回订单，通过邮件形式发送至各负责人" {
		t.Errorf("工作内容列取值错误：%q", got)
	}
	if got := cellAt(data[0], cm, "assignee"); got != "张三" {
		t.Errorf("处理人列取值错误：%q", got)
	}
	if parseMonthDay(cellAt(data[0], cm, "day")) != 1 {
		t.Error("日期「1号」应解析为 1")
	}
	// 「跟进人」也是 assignee 的别名，重复字段只认最左一列，后续列应被忽略
	if cm[5] != "skip" {
		t.Errorf("重复的「跟进人」列应被忽略，实际 %s", cm[5])
	}
}

// TestTaskMetaHorizontal 「信息」sheet 为横向表头（部门|仓库|年份|月份 + 次行取值）
func TestTaskMetaHorizontal(t *testing.T) {
	f := excelize.NewFile()
	info := "信息"
	if err := f.SetSheetName("Sheet1", info); err != nil {
		t.Fatal(err)
	}
	for j, h := range []string{"部门", "仓库", "年份", "月份"} {
		c, _ := excelize.CoordinatesToCellName(1+j, 1)
		f.SetCellValue(info, c, h)
	}
	for j, v := range []string{"物流部", "三亚预订仓", "2026", "08"} {
		c, _ := excelize.CoordinatesToCellName(1+j, 2)
		f.SetCellValue(info, c, v)
	}
	y, m, ok := xlsxMeta(f, "交接表.xlsx")
	if !ok || y != 2026 || m != 8 {
		t.Errorf("横向「信息」表头解析错误：%d-%d ok=%v", y, m, ok)
	}
}

func TestTaskMetaYearMonth(t *testing.T) {
	f := buildSeqTaskXLSX(t)
	y, m, ok := xlsxMeta(f, "交接表.xlsx")
	if !ok || y != 2026 || m != 9 {
		t.Errorf("年月解析错误：%d-%d ok=%v", y, m, ok)
	}
}

// buildNoHeadXLSX 无表头表格：仅数据行，首列为序号（兜底路径）
func buildNoHeadXLSX(t *testing.T) *excelize.File {
	f := excelize.NewFile()
	daily := "每日工作内容"
	if err := f.SetSheetName("Sheet1", daily); err != nil {
		t.Fatal(err)
	}
	rows := [][4]string{
		{"1", "09:00", "发数据", "早班"},
		{"2", "10:00", "巡检仓库", "早班"},
		{"3", "14:00", "盘点", "中班"},
	}
	for i, r := range rows {
		for j, v := range r {
			c, _ := excelize.CoordinatesToCellName(1+j, 1+i)
			f.SetCellValue(daily, c, v)
		}
	}
	return f
}

// realSheetRows 统计一行中有效任务条数：跳过空行/说明行/签字行，时间为空时沿用上一行
func realSheetRows(data [][]string, cm taskColMap) (int, []string) {
	n, times, last := 0, []string(nil), ""
	for _, r := range data {
		if isCommentRow(r) {
			continue
		}
		title := cellAt(r, cm, "title")
		if title == "" {
			title = cellAt(r, cm, "content")
		}
		if title == "" || isSignRow(title) {
			continue
		}
		tm := normalizeClock(cellAt(r, cm, "time"))
		if tm == "" {
			tm = last
		}
		if tm != "" {
			last = tm
		}
		times = append(times, tm)
		n++
	}
	return n, times
}

// buildRealJiaojieXLSX 复刻真实交接表：A 列整列留空、时间列合并单元格、尾部签字行、右侧打卡矩阵
func buildRealJiaojieXLSX(t *testing.T) *excelize.File {
	f := excelize.NewFile()
	daily := "每日工作内容"
	if err := f.SetSheetName("Sheet1", daily); err != nil {
		t.Fatal(err)
	}
	f.SetCellValue(daily, "B1", "三亚预订仓每日工作日程表")
	// 表头在第 2 行，且列不连续（B/C/E），右侧还有打卡矩阵表头
	for cell, v := range map[string]string{"B2": "时间", "C2": "时段工作", "E2": "负责班次", "H2": "责任主管"} {
		f.SetCellValue(daily, cell, v)
	}
	for j := 0; j < 31; j++ { // H~ 打卡矩阵 1..31
		c, _ := excelize.CoordinatesToCellName(8+j, 3)
		f.SetCellValue(daily, c, j+1)
	}
	// 数据：B 列时间合并（10:00 覆盖两行），中间夹空行
	body := [][3]string{
		{"09:00", "发数据（每日作业数据）", "早班"},
		{"10:00", "巡检仓库", "早班"},
		{"", "盘点（循环盘）", "早班"}, // 合并单元格，时间为空
		{"11:00", "发数据（小时报表）", "早班"},
		{"", "", "早班"}, // 空内容行
		{"16:00", "邮寄称重", "早、中"},
		{"18:00", "查看邮寄批次是否都关闭", "交接班"},
	}
	for i, r := range body {
		for j, v := range r {
			if v == "" {
				continue
			}
			c, _ := excelize.CoordinatesToCellName(2+j, 4+i) // B=2
			f.SetCellValue(daily, c, v)
		}
	}
	f.MergeCell(daily, "B5", "B6") // 10:00 合并两行 → 第 6 行 B 列读到空
	f.SetCellValue(daily, "B11", ">>>>>>此栏由组长签字确认：")
	f.SetCellValue(daily, "B12", ">>>>>>此栏由主管签字确认：")
	return f
}

// TestCommentRowLeadingEmpty A 列留空的表格不能被整体误判为说明行（v0.12.5 回归）
func TestCommentRowLeadingEmpty(t *testing.T) {
	f := buildRealJiaojieXLSX(t)
	data, cm, _, ok := readTaskSheet(f, []string{"每日", "每天", "日工作"}, []string{"time", "title", "shift"})
	if !ok {
		t.Fatal("未定位到每日工作内容 sheet")
	}
	if len(data) == 0 {
		t.Fatal("data 为空")
	}
	// 关键回归点：首列（A 列）为空但 B/C/E 有值 → 必须判定为数据行
	if isCommentRow(data[0]) {
		t.Fatalf("首列为空的数据行被误判为说明行：%#v", data[0])
	}
	if !isCommentRow([]string{}) {
		t.Error("空行应识别为说明行")
	}
	if !isCommentRow([]string{"填写说明：时间格式 HH:MM"}) {
		t.Error("仅首列有值的长文本应识别为说明行")
	}
	// 真实表：7 行里 1 行内容为空，应识别 6 条
	n, times := realSheetRows(data, cm)
	if n != 6 {
		t.Errorf("应识别 6 条任务，实际 %d（%v）", n, times)
	}
	want := []string{"09:00", "10:00", "10:00", "11:00", "16:00", "18:00"}
	for i, w := range want {
		if i < len(times) && times[i] != w {
			t.Errorf("第 %d 条时间应为 %s（合并单元格沿用上一行），实际 %s", i+1, w, times[i])
		}
	}
}

// TestSignRowFiltered 交接表尾部的签字确认行必须被过滤
func TestSignRowFiltered(t *testing.T) {
	for _, s := range []string{">>>>>>此栏由组长签字确认：", "此栏由主管签字确认：", "负责人确认:"} {
		if !isSignRow(s) {
			t.Errorf("签字行应被识别：%q", s)
		}
	}
	for _, s := range []string{"巡检仓库", "发数据（每日作业数据）", ""} {
		if isSignRow(s) {
			t.Errorf("正常任务被误判为签字行：%q", s)
		}
	}
}

// TestShiftMapMultiShift 「早、中」表示两个班都要做 → 全员
func TestShiftMapMultiShift(t *testing.T) {
	if got, ok := shiftMap("早、中"); !ok || got != "全员" {
		t.Errorf("「早、中」应映射为全员，实际 %s ok=%v", got, ok)
	}
	if got, ok := shiftMap("交接班"); !ok || got != "中班" {
		t.Errorf("「交接班」应映射为中班，实际 %s ok=%v", got, ok)
	}
	if _, ok := shiftMap("不知道什么班"); ok {
		t.Error("未知班次应返回 false")
	}
}

// TestNormalizeClock 各种时间写法都应归一为 HH:MM（Excel「h:mm」格式给出的 9:00 曾被丢弃）
func TestNormalizeClock(t *testing.T) {
	cases := map[string]string{
		"09:00": "09:00", "9:00": "09:00", "09:00:00": "09:00", "9:00:00": "09:00",
		"18:30": "18:30", "0:05": "00:05", "23:59": "23:59",
		"9点": "09:00", "9：00": "09:00", " 10:00 ": "10:00",
	}
	for in, want := range cases {
		if got := normalizeClock(in); got != want {
			t.Errorf("normalizeClock(%q) = %q，期望 %q", in, got, want)
		}
	}
	// Excel 时间小数：0.375 天 = 09:00
	if got := normalizeClock("0.375"); got != "09:00" {
		t.Errorf("Excel 时间小数 0.375 应为 09:00，实际 %q", got)
	}
	for _, in := range []string{"", "abc", "25:00", "10:70", "十点"} {
		if got := normalizeClock(in); got != "" {
			t.Errorf("normalizeClock(%q) 应返回空，实际 %q", in, got)
		}
	}
}

func TestTaskFallbackSkipSeqCol(t *testing.T) {
	f := buildNoHeadXLSX(t)
	_, cm, _, ok := readTaskSheet(f, []string{"每日", "每天", "日工作"}, []string{"time", "title", "shift"})
	if !ok {
		t.Fatal("未定位到 sheet")
	}
	// 无表头时首行当标题跳过，剩余 2 行；首列序号应被识别并整体右移
	if cm[0] != "skip" {
		t.Errorf("无表头时首列序号应被忽略，实际 %s", cm[0])
	}
	if cm[1] != "time" || cm[2] != "title" || cm[3] != "shift" {
		t.Errorf("兜底列序错误：%v", cm)
	}
}
