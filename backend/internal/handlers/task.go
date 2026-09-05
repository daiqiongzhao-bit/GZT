package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// monthlyDueTime 月度任务的默认截止时间。
// 业务含义：截止日当天 9:00 之前完成都算准时，9:00 之后仍未完成才算逾期
// —— 即「当天上班后处理完即可」，而不是凌晨 0 点一过就判逾期。
const monthlyDueTime = "09:00"

// monthDayOf 从 YYYY-MM-DDTHH:MM 提取"日"（1-31），失败或无值返回 0
func monthDayOf(deadline string) int {
	if deadline == "" {
		return 0
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", deadline, time.Local); err == nil {
		return t.Day()
	}
	return 0
}

// timeOf 从 YYYY-MM-DDTHH:MM 提取 HH:MM，失败返回空串
func timeOf(deadline string) string {
	if t, err := time.ParseInLocation("2006-01-02T15:04", deadline, time.Local); err == nil {
		return t.Format("15:04")
	}
	return ""
}

// isOverdue 判断任务是否逾期（未完成且已超过其执行/截止时间）
func isOverdue(t models.Task) bool {
	if t.Status == models.TaskStatusDone {
		return false
	}
	now := time.Now()
	switch t.Type {
	case models.TaskTypeOnce:
		if t.Deadline != "" {
			if dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local); err == nil && dl.Before(now) {
				return true
			}
		}
	case models.TaskTypeDaily:
		if t.Time != "" {
			if dt, err := time.ParseInLocation("2006-01-02T15:04", now.Format("2006-01-02")+"T"+t.Time, time.Local); err == nil && dt.Before(now) {
				return true
			}
		}
	case models.TaskTypeMonthly:
		// 按完整日期比较（而非只比「日」），跨月也成立：
		// 8 月的月度任务到了 10 月 1 日不会因为在「1 号」而被误判为未到期。
		//
		// 截止日当天整天都算数：deadline 里的 09:00 只是晨间推送提醒时点，
		// 不是完成期限。月度工作（超时订单、盘点建单等）一整天都能处理，
		// 当天 23:59 前完成均算准时，次日仍未完成才判逾期
		// —— 即「当天都未完成才算逾期」，而非早上 9 点一到就标红。
		if len(t.Deadline) < 10 {
			return false
		}
		dueDay := t.Deadline[:10]
		if _, err := time.ParseInLocation("2006-01-02", dueDay, time.Local); err != nil {
			return false
		}
		return dueDay < now.Format("2006-01-02")
	}
	return false
}

// isDueToday 判断任务今天是否应当处理
func isDueToday(t models.Task) bool {
	if t.Status == models.TaskStatusDone {
		return false
	}
	now := time.Now()
	switch t.Type {
	case models.TaskTypeDaily:
		return true
	case models.TaskTypeOnce:
		if t.Deadline == "" {
			return false
		}
		if dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local); err == nil {
			return dl.Format("2006-01-02") == now.Format("2006-01-02")
		}
		return false
	case models.TaskTypeMonthly:
		md := monthDayOf(t.Deadline)
		return md != 0 && md == now.Day()
	}
	return false
}

// isDueThisMonth 判断任务本月内是否应当处理（本月待办）
func isDueThisMonth(t models.Task) bool {
	if t.Status == models.TaskStatusDone {
		return false
	}
	now := time.Now()
	switch t.Type {
	case models.TaskTypeDaily, models.TaskTypeMonthly:
		return true
	case models.TaskTypeOnce:
		if t.Deadline == "" {
			return false
		}
		if dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local); err == nil {
			return dl.Format("2006-01") == now.Format("2006-01")
		}
		return false
	}
	return false
}

// resolveAssigneeID 根据负责人姓名解析对应用户ID；解析不到返回 0（部门公共任务）
func resolveAssigneeID(name string, deptID uint, super bool) uint {
	if name == "" {
		return 0
	}
	var u models.User
	q := db.DB.Select("id").Where("name = ?", name)
	if !super {
		q = q.Where("dept_id = ?", deptID)
	}
	if err := q.First(&u).Error; err != nil {
		return 0
	}
	return u.ID
}

func ListTasks(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	scope := deptScopeIDs(c)
	var list []models.Task
	q := db.DB.Order("created_at desc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	// 部门内员工互相可见：执行者同样可见本部门全部任务（写权限仍按负责人约束）
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range list {
		list[i].Overdue = isOverdue(list[i])
		list[i].DueToday = isDueToday(list[i])
		list[i].DueThisMonth = isDueThisMonth(list[i])
	}
	c.JSON(http.StatusOK, list)
}

// TaskCounts GET /api/tasks/counts 返回当前可见范围的待办统计（导航角标用）
func TaskCounts(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	scope := deptScopeIDs(c)
	var list []models.Task
	q := db.DB.Where("status = ?", models.TaskStatusTodo)
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	overdue, today, dueTotal := 0, 0, 0
	if err := q.Find(&list).Error; err == nil {
		for _, t := range list {
			o, d := isOverdue(t), isDueToday(t)
			if o {
				overdue++
			}
			if d {
				today++
			}
			// 「现在就该处理」= 逾期 ∪ 今日到期，同一任务只算一次。
			// 早期导航角标直接 overdue+today 相加，逾期任务被数了两遍
			// （2 条任务显示成 4），容易让人误以为待办翻倍，故这里给出
			// 去重后的总数供角标使用。
			if o || d {
				dueTotal++
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"overdue":       overdue,
		"today":         today,
		"due_total":     dueTotal,
		"total_pending": len(list),
	})
}

type taskReq struct {
	Title    string `json:"title"`
	Type     string `json:"type"`
	Shift    string `json:"shift"`
	Time     string `json:"time"`
	Deadline string `json:"deadline"`
	Assignee string `json:"assignee"`
	Priority string `json:"priority"`
	Note     string `json:"note"`
	DeptID   uint   `json:"dept_id"`
}

// CreateTask 新建任务
func CreateTask(c *gin.Context) {
	var req taskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务内容不能为空"})
		return
	}
	cl := currentClaims(c)
	deptID := req.DeptID
	if deptID == 0 {
		deptID = cl.DeptID // 未指定部门时兜底为本部门
	}
	if !canManageDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权在该部门下操作"})
		return
	}
	assigneeID := resolveAssigneeID(req.Assignee, deptID, cl.Role == models.RoleSuperAdmin)
	typ := req.Type
	if typ == "" {
		typ = models.TaskTypeDaily
	}
	t := models.Task{
		Title:      req.Title,
		Type:       typ,
		Shift:      req.Shift,
		Time:       req.Time,
		Deadline:   normalizeTaskDeadline(typ, req.Deadline),
		Assignee:   req.Assignee,
		AssigneeID: assigneeID,
		Priority:   req.Priority,
		Note:       req.Note,
		DeptID:     deptID,
		Status:     models.TaskStatusTodo,
	}
	if t.Shift == "" {
		t.Shift = "全员"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if err := db.DB.Create(&t).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "新建任务: "+t.Title)
	c.JSON(http.StatusOK, t)
}

// UpdateTask PUT /tasks/:id 编辑任务（部门管/超管）
func UpdateTask(c *gin.Context) {
	var req taskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务内容不能为空"})
		return
	}
	cl := currentClaims(c)
	var t models.Task
	if err := db.DB.First(&t, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	if !canManageDept(c, t.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能编辑本部门任务"})
		return
	}
	deptID := req.DeptID
	if deptID == 0 {
		deptID = t.DeptID // 未选择部门时保持原部门
	}
	if !canManageDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权把任务移到该部门"})
		return
	}
	t.Title = req.Title
	t.Type = req.Type
	if t.Type == "" {
		t.Type = models.TaskTypeDaily
	}
	t.Shift = req.Shift
	t.Time = req.Time
	t.Deadline = normalizeTaskDeadline(t.Type, req.Deadline)
	t.Assignee = req.Assignee
	t.AssigneeID = resolveAssigneeID(req.Assignee, deptID, cl.Role == models.RoleSuperAdmin)
	t.Priority = req.Priority
	t.Note = req.Note
	t.DeptID = deptID
	if t.Shift == "" {
		t.Shift = "全员"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if err := db.DB.Save(&t).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "编辑任务: "+t.Title)
	c.JSON(http.StatusOK, t)
}

// ToggleTask 完成任务/重开任务。
// v0.0.2：支持明确意图（幂等）——请求体 {to:"done"|"todo"} 时按意图执行：
//
//	点"完成"永远是完成，点"重开"永远是重开，不受其他端陈旧状态影响；
//
// 不传 to 时保持旧的"翻转"行为（兼容历史调用）。
func ToggleTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t models.Task
	if err := db.DB.First(&t, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	cl := currentClaims(c)
	if !canManageDept(c, t.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作其他部门任务"})
		return
	}
	// 解析明确意图（可选）
	var req struct {
		To string `json:"to"`
	}
	_ = c.ShouldBindJSON(&req)
	target := req.To
	if target != models.TaskStatusDone && target != models.TaskStatusTodo {
		target = "" // 未指定 → 翻转
	}
	// 幂等：意图与当前状态一致时直接返回现状，不做任何重复写入
	if target == t.Status {
		c.JSON(http.StatusOK, t)
		return
	}
	switch {
	case target == models.TaskStatusTodo || (target == "" && t.Status == models.TaskStatusDone):
		// 重开（显式 to=todo，或旧翻转语义下原本是 done）
		t.Status = models.TaskStatusTodo
		t.CompletedBy = "" // 重开清空"当前完成人"展示
	case target == models.TaskStatusDone || (target == "" && t.Status != models.TaskStatusDone):
		// 完成（显式 to=done，或旧翻转语义下原本是 todo）
		t.Status = models.TaskStatusDone
		t.CompletedBy = cl.Username // 记录最近一次完成人
		now := time.Now()
		t.CompletedAt = now // 最近一次完成时间
		// 写入完成审计记录（精确到秒，可回溯查询）
		_ = db.DB.Create(&models.TaskCompletion{
			TaskID:      t.ID,
			TaskTitle:   t.Title,
			UserID:      cl.UserID,
			UserName:    cl.Username,
			DeptID:      t.DeptID,
			CompletedAt: now,
		}).Error
	}
	db.DB.Save(&t)
	verb := "完成"
	if t.Status == models.TaskStatusTodo {
		verb = "重开"
	}
	addLog(c, cl.UserID, cl.Username, verb+"任务: "+t.Title)
	c.JSON(http.StatusOK, t)
}

// DeleteTask 删除任务
func DeleteTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t models.Task
	if err := db.DB.First(&t, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	cl := currentClaims(c)
	if !canManageDept(c, t.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除其他部门任务"})
		return
	}
	// 执行者仅可删除指派给本人的任务（assignee_id=0 视为部门公共）
	if cl.Role == models.RoleExecutor && t.AssigneeID != 0 && t.AssigneeID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除非本人负责的任务"})
		return
	}
	if err := db.DB.Delete(&t).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "删除任务: "+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BatchDeleteTasks POST /tasks/batch-delete 批量删除任务（部门管/超管）
// 逐条校验部门权限，越权的跳过并计入失败；执行者仅可删除指派给本人的任务
func BatchDeleteTasks(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先选择要删除的任务"})
		return
	}
	cl := currentClaims(c)
	ok, failed := 0, 0
	var errs []string
	for _, id := range req.IDs {
		var t models.Task
		if err := db.DB.First(&t, id).Error; err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("ID %d 不存在", id))
			continue
		}
		if !canManageDept(c, t.DeptID) {
			failed++
			errs = append(errs, fmt.Sprintf("「%s」无权删除（其他部门）", t.Title))
			continue
		}
		if cl.Role == models.RoleExecutor && t.AssigneeID != 0 && t.AssigneeID != cl.UserID {
			failed++
			errs = append(errs, fmt.Sprintf("「%s」无权删除（非本人负责）", t.Title))
			continue
		}
		if err := db.DB.Delete(&t).Error; err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("「%s」删除失败: %s", t.Title, err.Error()))
			continue
		}
		ok++
	}
	if ok > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("批量删除任务 %d 条", ok))
	}
	c.JSON(http.StatusOK, gin.H{"deleted": ok, "failed": failed, "errors": errs})
}

// normalizeTime helper（预留）
func normalizeTime(s string) string {
	return strings.TrimSpace(s)
}

// ListTaskCompletions 某任务的完成历史（按时间倒序）
func ListTaskCompletions(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var task models.Task
	if err := db.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	if !canManageDept(c, task.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看其他部门任务记录"})
		return
	}
	var list []models.TaskCompletion
	if err := db.DB.Where("task_id = ?", id).Order("completed_at desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ListCompletions 全局完成记录查询（支持 task_id / user_id / from / to 筛选）
// 部门管理员仅能查本部门；超管查全量
func ListCompletions(c *gin.Context) {
	scope := deptScopeIDs(c)
	q := db.DB.Model(&models.TaskCompletion{})
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	if v := c.Query("task_id"); v != "" {
		q = q.Where("task_id = ?", v)
	}
	if v := c.Query("user_id"); v != "" {
		q = q.Where("user_id = ?", v)
	}
	if v := c.Query("from"); v != "" {
		q = q.Where("completed_at >= ?", v)
	}
	if v := c.Query("to"); v != "" {
		q = q.Where("completed_at <= ?", v)
	}
	var list []models.TaskCompletion
	if err := q.Order("completed_at desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}
