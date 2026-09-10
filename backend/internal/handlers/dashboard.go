package handlers

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// Dashboard 概览数据聚合
func Dashboard(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	scope := deptScopeIDs(c)
	today := time.Now().Format("2006-01-02")
	// 班次时间段映射：给「我的今日班次」和今日待办补齐 09:00-17:00 这类可读信息
	shiftTimeMap := loadShiftTimes()

	var onDuty []models.Schedule
	// 「今日当班」只算真正上班的人：休息的不计入人数，也不出现在当班列表里
	// （班次为字面量，与 shiftMap 保持一致：早班/中班/晚班/夜班/休息/早晚/全员）
	sq := db.DB.Where("date = ?", today).Where("shift <> ?", "休息")
	if len(scope) > 0 {
		sq = sq.Where("dept_id IN ?", scope)
	}
	sq.Find(&onDuty)

	var tasks []models.Task
	tq := db.DB
	if len(scope) > 0 {
		tq = tq.Where("dept_id IN ?", scope)
	}
	tq.Find(&tasks)

	// 部门名映射（今日当班按「部门 / 姓名 / 班次」展示）
	var depts []models.Department
	db.DB.Find(&depts)
	deptName := map[uint]string{}
	for _, d := range depts {
		deptName[d.ID] = d.Name
	}
	// 姓名 -> 工号：今日当班名单里带上工号，便于同名人员区分
	nameNo := map[string]string{}
	var dutyUsers []models.User
	db.DB.Find(&dutyUsers)
	for _, u := range dutyUsers {
		if n := strings.TrimSpace(u.Name); n != "" {
			if _, dup := nameNo[n]; !dup {
				nameNo[n] = strings.TrimSpace(u.EmpNo)
			}
		}
	}

	type dutyRow struct {
		DeptID   uint   `json:"dept_id"`
		DeptName string `json:"dept_name"`
		Name     string `json:"name"`
		EmpNo    string `json:"emp_no"`
		Shift    string `json:"shift"`
	}
	var dutyRows []dutyRow

	onDutyCount := 0
	for _, s := range onDuty {
		var people []string
		json.Unmarshal([]byte(s.People), &people)
		onDutyCount += len(people)
		for _, n := range people {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			dutyRows = append(dutyRows, dutyRow{
				DeptID:   s.DeptID,
				DeptName: deptName[s.DeptID],
				Name:     n,
				EmpNo:    nameNo[n],
				Shift:    s.Shift,
			})
		}
	}
	sort.Slice(dutyRows, func(i, j int) bool {
		if dutyRows[i].DeptName != dutyRows[j].DeptName {
			return dutyRows[i].DeptName < dutyRows[j].DeptName
		}
		if dutyRows[i].Shift != dutyRows[j].Shift {
			return dutyRows[i].Shift < dutyRows[j].Shift
		}
		return dutyRows[i].Name < dutyRows[j].Name
	})
	todayTaskCount := 0
	overdueCount := 0
	runningCount := 0
	startingCount := 0
	monthTaskCount := 0
	monthlyCount := 0
	var todayTasks []models.Task
	var monthTasks []models.Task
	var monthlyTasks []models.Task
	for _, t := range tasks {
		ov := isOverdue(t)
		due := isDueToday(t)
		mt := isDueThisMonth(t)
		// v0.14.0：已到点且仍在宽限期内 = 正在执行（供插件角标与前端标记使用）
		run := isRunning(t)
		if run {
			runningCount++
		}
		// v0.14.1：距开始 ≤30 分钟尚未到点 = 即将开始
		start := isStarting(t)
		if start {
			startingCount++
		}
		// 当月任务 = 月度任务（type=monthly），展示全量含已完成，便于查看当月整月进度
		if t.Type == models.TaskTypeMonthly {
			t.Overdue = ov
			t.DueToday = due
			t.DueThisMonth = mt
			monthlyTasks = append(monthlyTasks, t)
			monthlyCount++
		}
		if due {
			t.Overdue = ov
			t.DueToday = due
			t.Running = run
			if run {
				t.RunningLeft = runningLeftMinutes(t)
			}
			t.Starting = start
			if start {
				t.StartingIn = startingInMinutes(t)
			}
			todayTasks = append(todayTasks, t)
			todayTaskCount++
		}
		if mt {
			t.Overdue = ov
			t.DueThisMonth = mt
			monthTasks = append(monthTasks, t)
			monthTaskCount++
		}
		if ov {
			overdueCount++
		}
	}

	// 「我的今日班次」：在今日当班名单里匹配当前登录用户。
	// v0.17.0：原来只按「姓名」匹配，工号（用户名）与姓名不一致时匹配不到；
	// 且插件端又自己做了一次本地匹配，两处逻辑容易分叉。现在统一由服务端判定：
	// 姓名、工号（EmpNo）、用户名任一命中即算当班，并把班次时间段一并下发。
	var me models.User
	meOK := false
	if cl := currentClaims(c); cl != nil {
		if db.DB.First(&me, cl.UserID).Error == nil {
			meOK = true
		}
	}
	myShift := ""
	myDeptID := uint(0)
	myShiftTime := ""
	if meOK {
		keys := make([]string, 0, 3)
		for _, v := range []string{me.Name, me.EmpNo, me.Username} {
			if v = strings.TrimSpace(v); v != "" {
				keys = append(keys, v)
			}
		}
		for _, s := range onDuty {
			var people []string
			json.Unmarshal([]byte(s.People), &people)
			hit := false
			for _, n := range people {
				n = strings.TrimSpace(n)
				for _, k := range keys {
					if n == k {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if hit {
				myShift = s.Shift
				myDeptID = s.DeptID
				myShiftTime = shiftTimeRange(s.DeptID, s.Shift, shiftTimeMap)
				break
			}
		}
	}

	// 今日待办列表补齐「姓名（工号）」与「班次」，供插件/前端直接展示，避免各端重复拼装。
	// 负责人来源优先级：assignees(JSON 数组) > assignee（顿号分隔的历史数据）。
	userNo := map[string]string{}
	var allUsers []models.User
	db.DB.Find(&allUsers)
	for _, u := range allUsers {
		if n := strings.TrimSpace(u.Name); n != "" {
			if _, dup := userNo[n]; !dup {
				userNo[n] = strings.TrimSpace(u.EmpNo)
			}
		}
	}
	attachAssigneeInfo := func(list []models.Task) {
		for i := range list {
			names := parseAssigneeNames(list[i].Assignees, list[i].Assignee)
			list[i].AssigneeNames = names
			list[i].AssigneeText = formatAssigneeText(names, userNo)
			if list[i].ShiftTime == "" {
				list[i].ShiftTime = shiftTimeRange(list[i].DeptID, list[i].Shift, shiftTimeMap)
			}
		}
	}
	attachAssigneeInfo(todayTasks)
	attachAssigneeInfo(monthTasks)
	attachAssigneeInfo(monthlyTasks)

	c.JSON(http.StatusOK, gin.H{
		"today":             today,
		"my_shift":          myShift,
		"my_dept_id":        myDeptID,
		"my_shift_time":     myShiftTime,
		"me_name":           strings.TrimSpace(me.Name),
		"me_emp_no":         strings.TrimSpace(me.EmpNo),
		"on_duty_count":     onDutyCount,
		"today_tasks":       todayTaskCount,
		"overdue_count":     overdueCount,
		"running_count":     runningCount,
		"starting_count":    startingCount,
		"month_tasks":       monthTaskCount,
		"monthly_tasks":     monthlyCount,
		"on_duty":           onDuty,
		"on_duty_rows":      dutyRows,
		"today_task_list":   todayTasks,
		"month_task_list":   monthTasks,
		"monthly_task_list": monthlyTasks,
	})
}

// parseAssigneeNames 解析任务负责人姓名列表。
// 优先 assignees（JSON 数组），回退 assignee（顿号/逗号等分隔的历史数据）。
func parseAssigneeNames(assignees, assignee string) []string {
	if s := strings.TrimSpace(assignees); s != "" {
		var arr []string
		if json.Unmarshal([]byte(s), &arr) == nil {
			out := make([]string, 0, len(arr))
			for _, n := range arr {
				if n = strings.TrimSpace(n); n != "" {
					out = append(out, n)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if s := strings.TrimSpace(assignee); s != "" {
		parts := strings.FieldsFunc(s, func(r rune) bool {
			return r == '、' || r == ',' || r == '，' || r == ';' || r == '；' || r == '/'
		})
		out := make([]string, 0, len(parts))
		for _, n := range parts {
			if n = strings.TrimSpace(n); n != "" {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}

// formatAssigneeText 把姓名列表格式化为「张三（YY001）、李四（YY002）」。
// 查不到工号时只显示姓名，不显示空括号。
func formatAssigneeText(names []string, userNo map[string]string) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if no := strings.TrimSpace(userNo[n]); no != "" {
			parts = append(parts, n+"（"+no+"）")
			continue
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, "、")
}

// shiftTimeMap 部门+班次 -> 时间段文案（如 "09:00-17:00"）。
// 同一班次名在不同部门时间段可能不同，故按 dept 维度建表，再退化到「任意部门同名班次」。
type shiftTimeLookup map[uint]map[string]string

func shiftTimeRange(deptID uint, shift string, m shiftTimeLookup) string {
	shift = strings.TrimSpace(shift)
	if shift == "" || shift == "全员" || shift == "休息" || m == nil {
		return ""
	}
	if d, ok := m[deptID]; ok {
		if v := d[shift]; v != "" {
			return v
		}
	}
	// 跨部门兜底：同名班次取第一个配到的时间段
	for _, d := range m {
		if v := d[shift]; v != "" {
			return v
		}
	}
	return ""
}

// loadShiftTimes 读取班次配置，构建部门+班次 -> 时间段 映射。
func loadShiftTimes() shiftTimeLookup {
	m := shiftTimeLookup{}
	var cfgs []models.ShiftConfig
	if err := db.DB.Find(&cfgs).Error; err != nil {
		return m
	}
	for _, c := range cfgs {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		start := strings.TrimSpace(c.StartTime)
		end := strings.TrimSpace(c.EndTime)
		if start == "" && end == "" {
			continue
		}
		if m[c.DeptID] == nil {
			m[c.DeptID] = map[string]string{}
		}
		if start != "" && end != "" {
			m[c.DeptID][name] = start + "-" + end
		} else if start != "" {
			m[c.DeptID][name] = start + " 起"
		} else {
			m[c.DeptID][name] = "至 " + end
		}
	}
	return m
}
