package handlers

import (
	"testing"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// TestParseAssigneeNames 负责人姓名解析：
// 优先 assignees（JSON 数组），回退 assignee（顿号/逗号等分隔的历史数据）。
func TestParseAssigneeNames(t *testing.T) {
	cases := []struct {
		name      string
		assignees string
		assignee  string
		want      []string
	}{
		{"JSON 数组优先", `["张伟","李娜"]`, "王芳", []string{"张伟", "李娜"}},
		{"单元素数组", `["吴敏"]`, "吴敏", []string{"吴敏"}},
		{"空数组回退 assignee", `[]`, "张伟、李娜", []string{"张伟", "李娜"}},
		{"非法 JSON 回退 assignee", `not-json`, "张伟、李娜", []string{"张伟", "李娜"}},
		{"顿号分隔", "", "张伟、李娜", []string{"张伟", "李娜"}},
		{"中英文逗号混合", "", "张伟, 李娜，王芳", []string{"张伟", "李娜", "王芳"}},
		{"斜杠分隔", "", "张伟/李娜", []string{"张伟", "李娜"}},
		{"数组内空白被剔除", `["张伟","  ","李娜"]`, "", []string{"张伟", "李娜"}},
		{"全空返回 nil", "", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseAssigneeNames(c.assignees, c.assignee)
			if len(got) != len(c.want) {
				t.Fatalf("解析结果 %v，期望 %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("解析结果 %v，期望 %v", got, c.want)
				}
			}
		})
	}
}

// TestFormatAssigneeText 负责人展示为「姓名（工号）」：查不到工号时只显示姓名，
// 不能出现「张三（）」这种空括号。
func TestFormatAssigneeText(t *testing.T) {
	userNo := map[string]string{
		"张伟": "YY001",
		"李娜": "YY002",
		// 王芳 故意不登记工号
	}
	cases := []struct {
		name  string
		names []string
		want  string
	}{
		{"带工号", []string{"张伟"}, "张伟（YY001）"},
		{"多人混合", []string{"张伟", "王芳", "李娜"}, "张伟（YY001）、王芳、李娜（YY002）"},
		{"无工号不补空括号", []string{"王芳"}, "王芳"},
		{"空列表返回空串", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatAssigneeText(c.names, userNo); got != c.want {
				t.Fatalf("格式化结果 %q，期望 %q", got, c.want)
			}
		})
	}
}

// TestShiftTimeRange 班次时间段查询：
// 优先命中本部门同名班次；本部门没有时跨部门兜底；「全员/休息/空」不返回时间段。
func TestShiftTimeRange(t *testing.T) {
	m := shiftTimeLookup{
		1: {"早班": "09:00-18:00"},
		2: {"早班": "08:00-16:00", "晚班": "16:00-00:00"},
	}
	cases := []struct {
		name   string
		deptID uint
		shift  string
		want   string
	}{
		{"本部门命中", 2, "早班", "08:00-16:00"},
		{"他部门同名走跨部门兜底", 3, "早班", "09:00-18:00"},
		{"本部门未配该班次走兜底", 2, "夜班", ""},
		{"全员不返回时间段", 1, "全员", ""},
		{"休息不返回时间段", 1, "休息", ""},
		{"空班次不返回时间段", 1, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shiftTimeRange(c.deptID, c.shift, m); got != c.want {
				t.Fatalf("查询结果 %q，期望 %q", got, c.want)
			}
		})
	}
}

// TestShiftTimeRangeNilMap 空映射表不应 panic（未配置任何班次的新部署）。
func TestShiftTimeRangeNilMap(t *testing.T) {
	if got := shiftTimeRange(1, "早班", nil); got != "" {
		t.Fatalf("空映射表应返回空串，实际 %q", got)
	}
}

// TestLoadShiftTimes 从班次配置构建映射，含单边时间的降级文案。
func TestLoadShiftTimes(t *testing.T) {
	openTestDB(t, &models.ShiftConfig{})
	db.DB.Create(&models.ShiftConfig{DeptID: 1, Name: "早班", StartTime: "09:00", EndTime: "17:00"})
	db.DB.Create(&models.ShiftConfig{DeptID: 1, Name: "中班", StartTime: "13:00"}) // 只有开始
	db.DB.Create(&models.ShiftConfig{DeptID: 1, Name: "夜班", EndTime: "09:00"})   // 只有结束
	db.DB.Create(&models.ShiftConfig{DeptID: 2, Name: "早班", StartTime: "08:00", EndTime: "16:00"})
	db.DB.Create(&models.ShiftConfig{DeptID: 3, Name: "空班"})                                     // 全空，应被跳过
	db.DB.Create(&models.ShiftConfig{DeptID: 3, Name: "", StartTime: "09:00", EndTime: "17:00"}) // 无名，应被跳过

	m := loadShiftTimes()
	if got := m[1]["早班"]; got != "09:00-17:00" {
		t.Fatalf("部门1早班 = %q，期望 09:00-17:00", got)
	}
	if got := m[1]["中班"]; got != "13:00 起" {
		t.Fatalf("部门1中班 = %q，期望 13:00 起", got)
	}
	if got := m[1]["夜班"]; got != "至 09:00" {
		t.Fatalf("部门1夜班 = %q，期望 至 09:00", got)
	}
	// 同名班次在不同部门各自独立，不能被覆盖
	if got := m[2]["早班"]; got != "08:00-16:00" {
		t.Fatalf("部门2早班 = %q，期望 08:00-16:00（不能被部门1覆盖）", got)
	}
	// 无名 / 无时间的配置被跳过
	if _, ok := m[3]["空班"]; ok {
		t.Fatal("无时间段的班次不应入表")
	}
	if _, ok := m[3][""]; ok {
		t.Fatal("无名称的班次不应入表")
	}
}
