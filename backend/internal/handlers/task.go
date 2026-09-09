package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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

// isSoonOverdue v0.9.2：距截止 ≤ soonOverdueMinutes 分钟（默认 30）但尚未逾期
// —— 比如 11:00 截单、当前 10:35，距离 25 分钟，需要橙色「即将逾期」提示
// 让使用者知道这个任务马上就要逾期了，比单纯标红更早介入
func isSoonOverdue(t models.Task) bool {
	if t.Status == models.TaskStatusDone {
		return false
	}
	now := time.Now()
	switch t.Type {
	case models.TaskTypeOnce:
		if t.Deadline == "" {
			return false
		}
		dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local)
		if err != nil {
			return false
		}
		if !dl.After(now) {
			return false // 已逾期不算「即将」
		}
		return dl.Sub(now) <= time.Duration(soonOverdueMinutes())*time.Minute
	case models.TaskTypeDaily:
		if t.Time == "" {
			return false
		}
		dt, err := time.ParseInLocation("2006-01-02T15:04", now.Format("2006-01-02")+"T"+t.Time, time.Local)
		if err != nil {
			return false
		}
		if !dt.After(now) {
			return false
		}
		return dt.Sub(now) <= time.Duration(soonOverdueMinutes())*time.Minute
	}
	return false
}

// taskUrgencyKey v0.9.2：给未完成任务算一个「截止紧迫度」排序键。
// 归一为 YYYY-MM-DDTHH:MM 字符串，字符串越小越紧迫：
//  - 单次/月度任务用 deadline；daily 用当天 time
//  - 解析失败或无截止时间的放到最后（用 nowStr，保证排在同窗但靠后），
//    但已逾期/即将逾期已在排序第一步被提到最前，这里只是相对序。
func taskUrgencyKey(t models.Task, nowStr string) string {
	var k string
	switch t.Type {
	case models.TaskTypeOnce, models.TaskTypeMonthly:
		k = t.Deadline
	case models.TaskTypeDaily:
		if t.Time != "" {
			k = time.Now().Format("2006-01-02") + "T" + t.Time
		}
	}
	if k == "" {
		return "9999-12-31T23:59"
	}
	if len(k) >= 16 {
		return k[:16]
	}
	if len(k) == 10 { // 只有日期
		return k + "T23:59"
	}
	return k
}

// isOverdue 判断任务是否逾期（未完成且已超过其执行/截止时间）
func isOverdue(t models.Task) bool {
	if t.Status == models.TaskStatusDone {
		return false
	}
	now := time.Now()
	grace := time.Duration(overdueGraceMinutes()) * time.Minute
	switch t.Type {
	case models.TaskTypeOnce:
		if t.Deadline != "" {
			if dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local); err == nil && dl.Add(grace).Before(now) {
				return true
			}
		}
	case models.TaskTypeDaily:
		if t.Time != "" {
			if dt, err := time.ParseInLocation("2006-01-02T15:04", now.Format("2006-01-02")+"T"+t.Time, time.Local); err == nil && dt.Add(grace).Before(now) {
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
		// 每日任务可选「按周执行」：命中勾选的星期才处理；未勾选=每天都要处理
		return isWeekDayMatch(t.WeekDays)
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

// isDueThisMonth 判断任务本月内是否应当处理（本月待办清单，含已完成）
// 不再过滤已完成状态——本月的「完成率」需要分子分母都包含已完成项
// （isDueToday/isOverdue 仍过滤已完成，已完成的不是「待办」也不是「逾期」，语义不同）
func isDueThisMonth(t models.Task) bool {
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

// ===================== 单人/多人负责人 + 按周执行 辅助 =====================

// resolveAssigneeIDs 按姓名批量解析用户ID（逐名解析，跳过解析不到/重复的）
func resolveAssigneeIDs(names []string, deptID uint, super bool) []uint {
	var ids []uint
	seen := map[uint]bool{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		id := resolveAssigneeID(n, deptID, super)
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// splitNames 把负责人字符串切成姓名列表（兼容 、;；,，/空格 分隔）
func splitNames(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '、' || r == ';' || r == '；' || r == ',' || r == '，' || r == '/' || r == ' ' || r == '\t'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return dedupeStrs(out)
}

// assigneeNamesFrom 归一前端/CSV 传入的负责人：优先取 assignees 数组，空则解析 assignee 字符串
func assigneeNamesFrom(assignee string, assignees []string) []string {
	if len(assignees) == 0 {
		return splitNames(assignee)
	}
	var out []string
	for _, a := range assignees {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return dedupeStrs(out)
}

func dedupeStrs(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func jsonStrings(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}

func jsonUints(ids []uint) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

// applyAssignees 把负责人名单写入任务：展示名（顿号连接）、首个ID、姓名JSON、ID JSON
func applyAssignees(t *models.Task, names []string, deptID uint, super bool) {
	names = dedupeStrs(names)
	if len(names) == 0 {
		t.Assignee = ""
		t.AssigneeID = 0
		t.Assignees = ""
		t.AssigneeIDs = ""
		return
	}
	ids := resolveAssigneeIDs(names, deptID, super)
	t.Assignee = strings.Join(names, "、")
	t.Assignees = jsonStrings(names)
	t.AssigneeIDs = jsonUints(ids)
	t.AssigneeID = 0
	if len(ids) > 0 {
		t.AssigneeID = ids[0]
	}
}

// taskAssigneeNames 返回任务负责人姓名列表（多人解析；兼容旧的单 Assignee 字段）
func taskAssigneeNames(t models.Task) []string {
	if t.Assignees != "" {
		var names []string
		if json.Unmarshal([]byte(t.Assignees), &names) == nil && len(names) > 0 {
			return names
		}
	}
	return splitNames(t.Assignee)
}

// taskAssigneeIDs 返回负责人用户ID列表（兼容旧的 AssigneeID 字段）
func taskAssigneeIDs(t models.Task) []uint {
	if t.AssigneeIDs != "" {
		var ids []uint
		if json.Unmarshal([]byte(t.AssigneeIDs), &ids) == nil {
			return ids
		}
	}
	if t.AssigneeID != 0 {
		return []uint{t.AssigneeID}
	}
	return nil
}

// canAssigneeOperate 执行者能否对该任务操作：负责人本人可；assignee_id=0（部门公共）任何人可
func canAssigneeOperate(t models.Task, uid uint) bool {
	ids := taskAssigneeIDs(t)
	if len(ids) == 0 {
		return true // 未指派/部门公共
	}
	for _, id := range ids {
		if id == uid {
			return true
		}
	}
	return false
}

// normalizeWeekDays 归一「按周执行」星期："周一,周三" / "1,3,5" → "1,3,5"（1=周一…7=周日）；空返回空
func normalizeWeekDays(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	rep := strings.NewReplacer(
		"星期日", "7", "星期天", "7", "周天", "7", "周日", "7",
		"星期六", "6", "周六", "6", "星期五", "5", "周五", "5",
		"星期四", "4", "周四", "4", "星期三", "3", "周三", "3",
		"星期二", "2", "周二", "2", "星期一", "1", "周一", "1",
	)
	s = rep.Replace(s)
	seen := map[int]bool{}
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '/' || r == ' '
	}) {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err == nil && n >= 1 && n <= 7 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return ""
	}
	ss := make([]string, 0, len(out))
	for _, n := range out {
		ss = append(ss, strconv.Itoa(n))
	}
	return strings.Join(ss, ",")
}

// weekDaysSet 解析 "1,3,5" → map 星期集合
func weekDaysSet(s string) map[int]bool {
	m := map[int]bool{}
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n >= 1 && n <= 7 {
			m[n] = true
		}
	}
	return m
}

// isWeekDayMatch 今天（1=周一…7=周日）是否命中按周执行配置；配置为空视为每天执行
func isWeekDayMatch(days string) bool {
	set := weekDaysSet(days)
	if len(set) == 0 {
		return true
	}
	// time.Weekday(): Sunday=0 … Saturday=6 → 转 1=周一 … 7=周日
	w := (int(time.Now().Weekday()) + 6) % 7
	wd := w + 1
	return set[wd]
}

// weekDaysLabel 把 "1,3,5" 转可读文案 "周一/周三/周五"
func weekDaysLabel(s string) string {
	if s == "" {
		return ""
	}
	names := map[int]string{1: "周一", 2: "周二", 3: "周三", 4: "周四", 5: "周五", 6: "周六", 7: "周日"}
	var out []string
	for _, n := range strings.Split(s, ",") {
		if d, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			if label, ok := names[d]; ok {
				out = append(out, label)
			}
		}
	}
	return strings.Join(out, "/")
}

func ListTasks(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	scope := deptScopeIDs(c)
	var list []models.Task
	q := db.DB.Order("created_at asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	// 部门内员工互相可见：执行者同样可见本部门全部任务（写权限仍按负责人约束）
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// v0.9.2 排序：先算瞬态标记（overdue / due_today / soon），
	// 再在内存里按「正序」排——需要马上处理 / 即将逾期的排最前，按截止时间近→远，
	// 已完成的任务沉底。注意 overdue 等是 gorm:"-" 瞬态字段不落库，
	// 不能写进 SQL WHERE/ORDER（会导致任务接口 500 → 插件/首页拿不到任务）。
	for i := range list {
		list[i].Overdue = isOverdue(list[i])
		list[i].DueToday = isDueToday(list[i])
		list[i].DueThisMonth = isDueThisMonth(list[i])
		list[i].SoonOverdue = isSoonOverdue(list[i])
	}
	now := time.Now()
	nowStr := now.Format("2006-01-02T15:04")
	sort.SliceStable(list, func(a, b int) bool {
		ta, tb := list[a], list[b]
		doneA, doneB := ta.Status == models.TaskStatusDone, tb.Status == models.TaskStatusDone
		if doneA != doneB {
			return !doneA // 未完成排前面，已完成沉底
		}
		if doneA && doneB {
			return ta.CompletedAt.After(tb.CompletedAt) // 已完成的按完成时间新→旧
		}
		// 未完成：优先「需要马上处理」的（逾期 / 即将逾期），再按截止时间正序
		actA, actB := ta.Overdue || ta.SoonOverdue, tb.Overdue || tb.SoonOverdue
		if actA != actB {
			return actA
		}
		keyA, keyB := taskUrgencyKey(ta, nowStr), taskUrgencyKey(tb, nowStr)
		if keyA != keyB {
			return keyA < keyB
		}
		return ta.CreatedAt.Before(tb.CreatedAt)
	})
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
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Shift     string   `json:"shift"`
	Time      string   `json:"time"`
	Deadline  string   `json:"deadline"`
	Assignee  string   `json:"assignee"`  // 兼容旧版单人字段（也可填多个姓名，用顿号/分号分隔）
	Assignees []string `json:"assignees"` // 单人/多人负责人姓名数组（优先于 assignee）
	WeekDays  string   `json:"week_days"` // 按周执行：勾选的星期，如 "1,3,5"（1=周一…7=周日）
	Priority  string   `json:"priority"`
	Note      string   `json:"note"`
	DeptID    uint     `json:"dept_id"`
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
	typ := req.Type
	if typ == "" {
		typ = models.TaskTypeDaily
	}
	t := models.Task{
		Title:    req.Title,
		Type:     typ,
		Shift:    req.Shift,
		Time:     req.Time,
		Deadline: normalizeTaskDeadline(typ, req.Deadline),
		WeekDays: normalizeWeekDays(req.WeekDays),
		Priority: req.Priority,
		Note:     req.Note,
		DeptID:   deptID,
		Status:   models.TaskStatusTodo,
	}
	// 负责人：支持单人/多人（优先 assignees 数组）
	applyAssignees(&t, assigneeNamesFrom(req.Assignee, req.Assignees), deptID, cl.Role == models.RoleSuperAdmin)
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
	t.WeekDays = normalizeWeekDays(req.WeekDays)
	// 负责人：支持单人/多人（优先 assignees 数组）
	applyAssignees(&t, assigneeNamesFrom(req.Assignee, req.Assignees), deptID, cl.Role == models.RoleSuperAdmin)
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
	if cl.Role == models.RoleExecutor && !canAssigneeOperate(t, cl.UserID) {
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
		if cl.Role == models.RoleExecutor && !canAssigneeOperate(t, cl.UserID) {
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
	fillCompletionNames(list)
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
	fillCompletionNames(list)
	c.JSON(http.StatusOK, list)
}

// fillCompletionNames 把完成记录的 UserName 补全为「姓名（工号）」，
// 按 UserID 关联用户表；历史记录（UserID 可能已不存在）保留原 UserName。
func fillCompletionNames(list []models.TaskCompletion) {
	for i := range list {
		if list[i].UserID == 0 {
			continue
		}
		var u models.User
		if err := db.DB.First(&u, list[i].UserID).Error; err == nil && u.ID != 0 {
			list[i].UserName = fmt.Sprintf("%s（%s）", u.Name, u.Username)
		}
	}
}
