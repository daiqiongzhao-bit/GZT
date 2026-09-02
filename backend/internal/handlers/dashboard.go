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

	type dutyRow struct {
		DeptID   uint   `json:"dept_id"`
		DeptName string `json:"dept_name"`
		Name     string `json:"name"`
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
			dutyRows = append(dutyRows, dutyRow{DeptID: s.DeptID, DeptName: deptName[s.DeptID], Name: n, Shift: s.Shift})
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
	monthTaskCount := 0
	monthlyCount := 0
	var todayTasks []models.Task
	var monthTasks []models.Task
	var monthlyTasks []models.Task
	for _, t := range tasks {
		ov := isOverdue(t)
		due := isDueToday(t)
		mt := isDueThisMonth(t)
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

	c.JSON(http.StatusOK, gin.H{
		"today":           today,
		"on_duty_count":   onDutyCount,
		"today_tasks":     todayTaskCount,
		"overdue_count":   overdueCount,
		"month_tasks":       monthTaskCount,
		"monthly_tasks":     monthlyCount,
		"on_duty":           onDuty,
		"on_duty_rows":      dutyRows,
		"today_task_list":   todayTasks,
		"month_task_list":   monthTasks,
		"monthly_task_list": monthlyTasks,
	})
}
