package handlers

// workspace_ext.go —— 工作台能力补齐（v0.16.0）
//
// 背景：工作日志与交接接力此前"过于简约"——日志只能看单日、无检索无统计无附件；
// 交接只有 pending/in_progress/done 三态与一个覆盖式备注，没有处理轨迹、不能退回、
// 不能催办、无优先级与截止时间。本文件补齐这些缺口。
//
// 设计约束（重要）：
//  1. 不修改任何既有接口的响应结构 —— 日志/交接列表仍返回数组，仅新增可选查询参数；
//     总数通过 X-Total-Count 响应头给出，老调用方读不到也没影响。
//  2. 附件与知识库附件共用同一存储目录（kAttachmentDir），以便随既有备份/还原
//     链路一起打包（backup.go 打包的是整个 workspace_attachments 目录），不产生数据孤岛。
//  3. 所有新增列均可空/有默认值，AutoMigrate 对存量数据无破坏。

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
)

// maxWSPageSize 单页上限，防止一次性拉爆
const maxWSPageSize = 200

// ---------- 通用小工具 ----------

// parseLimitParam 解析分页大小；<=0 或非法返回 0（表示不分页）
func parseLimitParam(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	if n > maxWSPageSize {
		n = maxWSPageSize
	}
	return n
}

// parseOffsetParam 解析分页偏移；非法返回 0
func parseOffsetParam(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseFlexibleTime 兼容前端可能传来的几种时间写法
func parseFlexibleTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// recordHandoverEvent 追加一条交接时间线事件（写失败不阻断主流程）
func recordHandoverEvent(handoverID uint, action string, actorID uint, actorName, note string) {
	_ = db.DB.Create(&models.WorkHandoverEvent{
		HandoverID: handoverID,
		Action:     action,
		ActorID:    actorID,
		ActorName:  actorName,
		Note:       note,
	}).Error
}

// ==================== 工作日志：共用查询 / 统计 / 导出 ====================

// buildWorkLogQuery 构造「当前用户可见 + 全部可选筛选」的日志查询。
// ListWorkLogs / WorkLogStats / ExportWorkLogs 共用，避免三处过滤器各写一遍导致口径漂移。
func buildWorkLogQuery(c *gin.Context) *gorm.DB {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.WorkLog{})

	if date := strings.TrimSpace(c.Query("date")); date != "" {
		q = q.Where("log_date = ?", date)
	}
	if month := strings.TrimSpace(c.Query("month")); month != "" {
		q = q.Where("log_date LIKE ?", month+"-%")
	}
	// v0.16.0：日期区间 / 按人 / 关键词
	if from := strings.TrimSpace(c.Query("from")); from != "" {
		q = q.Where("log_date >= ?", from)
	}
	if to := strings.TrimSpace(c.Query("to")); to != "" {
		q = q.Where("log_date <= ?", to)
	}
	if owner := strings.TrimSpace(c.Query("owner_id")); owner != "" {
		if oid, err := strconv.ParseUint(owner, 10, 64); err == nil && oid > 0 {
			q = q.Where("owner_id = ?", oid)
		}
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(title LIKE ? OR done LIKE ? OR pending LIKE ?)", like, like, like)
	}
	if cl == nil {
		return q.Where("1 = 0")
	}
	if c.Query("mine") == "1" {
		// 我写的日志：只取自己
		q = q.Where("owner_id = ?", cl.UserID)
	} else {
		// 共享视图：本人 + 同部门共享（部门日志本）
		q = scopeVisibleQ(c, q)
	}
	return q
}

// hydrateWorkLogs 回填日志的瞬态字段：附件数、已转交接单数（供前端显示角标）
func hydrateWorkLogs(list []models.WorkLog) {
	if len(list) == 0 {
		return
	}
	ids := make([]uint, 0, len(list))
	for _, l := range list {
		ids = append(ids, l.ID)
	}
	type row struct {
		RefID uint
		N     int
	}
	var rows []row
	db.DB.Model(&models.WSFileAttachment{}).
		Select("ref_id, COUNT(*) AS n").
		Where("module = ? AND ref_id IN ?", wsModuleLog, ids).
		Group("ref_id").Scan(&rows)
	byID := make(map[uint]int, len(rows))
	for _, r := range rows {
		byID[r.RefID] = r.N
	}
	// 由本篇日志转出的交接单数量（只统计未删除的）
	var hvRows []row
	db.DB.Model(&models.WorkHandover{}).
		Select("source_log_id AS ref_id, COUNT(*) AS n").
		Where("source_log_id IN ?", ids).
		Group("source_log_id").Scan(&hvRows)
	hvByID := make(map[uint]int, len(hvRows))
	for _, r := range hvRows {
		hvByID[r.RefID] = r.N
	}
	for i := range list {
		list[i].AttachCount = byID[list[i].ID]
		list[i].HandoverCount = hvByID[list[i].ID]
	}
}

// ListWorkLogsStats GET /workspace/logs/stats
// 返回当前筛选范围内的统计：总篇数、参与人数、含遗留待办的篇数、按人分布、今日未填写的人。
func WorkLogStats(c *gin.Context) {
	q := buildWorkLogQuery(c)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 参与人数（去重 owner）
	type cntRow struct {
		OwnerID uint
		Name    string
		N       int
	}
	var byOwner []cntRow
	if err := buildWorkLogQuery(c).
		Select("owner_id, owner_name AS name, COUNT(*) AS n").
		Group("owner_id, owner_name").Order("n DESC").Limit(20).Scan(&byOwner).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var withPending int64
	_ = buildWorkLogQuery(c).Where("TRIM(COALESCE(pending, '')) <> ''").Count(&withPending).Error

	var edited int64
	_ = buildWorkLogQuery(c).Where("edit_count > 0").Count(&edited).Error

	// 今日未填写名单（仅统计未冻结、未休假的人员，避免把休假的人算成"没写"）
	today := time.Now().Format("2006-01-02")
	var wroteToday []uint
	_ = db.DB.Model(&models.WorkLog{}).Where("log_date = ?", today).Pluck("owner_id", &wroteToday).Error
	wroteSet := make(map[uint]bool, len(wroteToday))
	for _, id := range wroteToday {
		wroteSet[id] = true
	}
	var users []models.User
	_ = db.DB.Select("id, name, emp_no, dept_id").
		Where("frozen = ? AND on_leave = ?", false, false).Find(&users).Error
	missing := make([]gin.H, 0, 20)
	for _, u := range users {
		if wroteSet[u.ID] {
			continue
		}
		missing = append(missing, gin.H{"user_id": u.ID, "name": u.Name, "emp_no": u.EmpNo})
		if len(missing) >= 50 {
			break
		}
	}

	owners := make([]gin.H, 0, len(byOwner))
	for _, r := range byOwner {
		owners = append(owners, gin.H{"user_id": r.OwnerID, "name": r.Name, "count": r.N})
	}

	c.JSON(http.StatusOK, gin.H{
		"total":        total,
		"people":       len(byOwner),
		"with_pending": withPending,
		"edited":       edited,
		"today":        today,
		"by_owner":     owners,
		"missing_today": missing,
	})
}

// ExportWorkLogs GET /workspace/logs/export?format=csv
// 按当前筛选导出。带 UTF-8 BOM，Excel 直接双击打开不乱码。
func ExportWorkLogs(c *gin.Context) {
	var list []models.WorkLog
	if err := buildWorkLogQuery(c).Order("log_date desc, id desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var buf strings.Builder
	buf.WriteString("\uFEFF") // BOM
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"日期", "主题", "记录人", "可见范围", "今天做了什么", "还没做完的", "编辑次数", "最后修改时间"})
	for _, l := range list {
		scope := "仅自己可见"
		if l.Scope == models.ScopeDepartment {
			scope = "同部门共享"
		}
		_ = w.Write([]string{
			l.LogDate, l.Title, l.OwnerName, scope,
			l.Done, l.Pending,
			strconv.Itoa(l.EditCount), l.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	w.Flush()

	name := fmt.Sprintf("工作日志_%s.csv", time.Now().Format("20060102"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="logs.csv"; filename*=UTF-8''%s`, url.QueryEscape(name)))
	c.String(http.StatusOK, buf.String())
}

// LogToHandover POST /workspace/logs/:id/handover
// 把一篇工作日志「还没做完的」一键转成交接单 —— 原来是两件互不相干的事：
// 人在日志里写下待办，却要手工到交接接力里重打一遍。这里打通，并记录 SourceLogID 便于双向追溯。
func LogToHandover(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var lg models.WorkLog
	if err := db.DB.First(&lg, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "日志不存在"})
		return
	}
	// 只有日志本人（或超管）能把它转出去
	if lg.OwnerID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅日志作者可转交接"})
		return
	}

	var req struct {
		Title       string `json:"title"`        // 留空则自动取日志主题
		Todo        string `json:"todo"`         // 留空则取日志的「还没做完的」
		AssigneeIDs []uint `json:"assignee_ids"` // 接收人（必填）
		AssigneeID  uint   `json:"assignee_id"`  // 兼容单选
		Priority    string `json:"priority"`
		DueAt       string `json:"due_at"`
		Note        string `json:"note"` // 交接说明（可选）
	}
	_ = c.ShouldBindJSON(&req)

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = strings.TrimSpace(lg.Title)
	}
	if title == "" {
		title = "工作日志待办交接（" + lg.LogDate + "）"
	}
	todo := strings.TrimSpace(req.Todo)
	if todo == "" {
		todo = strings.TrimSpace(lg.Pending)
	}
	if todo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该日志没有「还没做完的」内容，请手工填写要交接的事项"})
		return
	}

	// 合并接收人并去重
	ids := append([]uint{}, req.AssigneeIDs...)
	if req.AssigneeID != 0 {
		ids = append(ids, req.AssigneeID)
	}
	seen := map[uint]bool{}
	cleaned := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择至少一个接收人"})
		return
	}
	for _, id := range cleaned {
		if id == cl.UserID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "不能交接给自己"})
			return
		}
	}
	var us []models.User
	if err := db.DB.Where("id IN ?", cleaned).Find(&us).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(us) != len(cleaned) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "部分接收人不存在，请刷新后重试"})
		return
	}
	// 按请求顺序排列姓名
	idOrder := make(map[uint]int, len(cleaned))
	for i, id := range cleaned {
		idOrder[id] = i
	}
	sort.SliceStable(us, func(i, j int) bool { return idOrder[us[i].ID] < idOrder[us[j].ID] })
	orderedNames := make([]string, 0, len(us))
	for _, u := range us {
		orderedNames = append(orderedNames, u.Name)
	}
	idsJSON, _ := json.Marshal(cleaned)
	namesJSON, _ := json.Marshal(orderedNames)

	pri := models.HandoverNormal
	if req.Priority == models.HandoverUrgent {
		pri = models.HandoverUrgent
	}
	var due *time.Time
	if s := strings.TrimSpace(req.DueAt); s != "" {
		if t, ok := parseFlexibleTime(s); ok {
			due = &t
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "截止时间格式不正确"})
			return
		}
	}

	fromProgress := strings.TrimSpace(lg.Done)
	if note := strings.TrimSpace(req.Note); note != "" {
		if fromProgress != "" {
			fromProgress += "\n"
		}
		fromProgress += "（交接说明）" + note
	}
	if fromProgress == "" {
		fromProgress = "（源自 " + lg.LogDate + " 的工作日志，未填写已完成事项）"
	}

	hv := models.WorkHandover{
		Title:         title,
		FromProgress:  fromProgress,
		Todo:          todo,
		Scope:         models.ScopeDepartment,
		SenderID:      cl.UserID,
		SenderName:    cl.Username,
		AssigneeID:    cleaned[0],
		AssigneeName:  orderedNames[0],
		AssigneeIDs:   string(idsJSON),
		AssigneeNames: string(namesJSON),
		DeptID:        cl.DeptID,
		Status:        models.HandoverPending,
		Priority:      pri,
		DueAt:         due,
		SourceLogID:   lg.ID,
	}
	if err := db.DB.Create(&hv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	recordHandoverEvent(hv.ID, "create", cl.UserID, cl.Username, "由工作日志「"+lg.Title+"」转出")
	// 通知接收人
	for _, u := range us {
		db.DB.Create(&models.Notification{
			UserID: u.ID, Kind: "user", Title: "你收到一份新的工作交接",
			Content: "来自 " + cl.Username + "：「" + hv.Title + "」请到「工作台-交接接力」查看并继续处理。",
			ActorID: cl.UserID, ActorName: cl.Username,
		})
	}
	addLog(c, cl.UserID, cl.Username, "日志["+lg.Title+"]转交接["+hv.Title+"]")
	c.JSON(http.StatusOK, hv)
}

// ==================== 交接接力：共用查询 / 统计 / 时间线 / 催办 ====================

// buildHandoverQuery 构造「当前用户相关（或超管全部）+ 全部可选筛选」的交接查询
func buildHandoverQuery(c *gin.Context) *gorm.DB {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.WorkHandover{})
	q = handoverVisibleQ(c, q)
	if cl == nil {
		return q.Where("1 = 0")
	}
	switch c.Query("role") {
	case "inbox":
		uidStr := fmt.Sprintf("%d", cl.UserID)
		q = q.Where("assignee_id = ? OR assignee_ids LIKE ? OR assignee_ids LIKE ? OR assignee_ids LIKE ?",
			cl.UserID,
			"%"+uidStr+",%",
			"["+uidStr+",%",
			"%"+uidStr+"]%")
	case "outbox":
		q = q.Where("sender_id = ?", cl.UserID)
	}
	if st := strings.TrimSpace(c.Query("status")); st != "" && st != "all" {
		q = q.Where("status = ?", st)
	}
	if pri := strings.TrimSpace(c.Query("priority")); pri != "" && pri != "all" {
		q = q.Where("priority = ?", pri)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(title LIKE ? OR from_progress LIKE ? OR todo LIKE ?)", like, like, like)
	}
	// v0.16.0：仅看逾期（未完成且已过期望完成时间）
	if c.Query("overdue") == "1" {
		q = q.Where("due_at IS NOT NULL AND due_at < ? AND status <> ?", time.Now(), models.HandoverDone)
	}
	return q
}

// hydrateHandovers 回填瞬态字段：附件数、时间线条数、是否逾期；并补全接收人姓名
func hydrateHandovers(list []models.WorkHandover) {
	if len(list) == 0 {
		return
	}
	ids := make([]uint, 0, len(list))
	for _, h := range list {
		ids = append(ids, h.ID)
	}
	type row struct {
		RefID uint
		N     int
	}
	attByID := map[uint]int{}
	var attRows []row
	db.DB.Model(&models.WSFileAttachment{}).Select("ref_id, COUNT(*) AS n").
		Where("module = ? AND ref_id IN ?", wsModuleHandover, ids).Group("ref_id").Scan(&attRows)
	for _, r := range attRows {
		attByID[r.RefID] = r.N
	}
	evByID := map[uint]int{}
	var evRows []row
	db.DB.Model(&models.WorkHandoverEvent{}).Select("handover_id AS ref_id, COUNT(*) AS n").
		Where("handover_id IN ?", ids).Group("handover_id").Scan(&evRows)
	for _, r := range evRows {
		evByID[r.RefID] = r.N
	}
	now := time.Now()
	for i := range list {
		hv := &list[i]
		hv.AttachCount = attByID[hv.ID]
		hv.EventCount = evByID[hv.ID]
		hv.Overdue = hv.DueAt != nil && hv.Status != models.HandoverDone && hv.DueAt.Before(now)
	}
	hydrateHandoverNames(&list)
}

// HandoverStats GET /workspace/handovers/stats
// 返回与当前 role 范围一致的计数，供概览卡片使用。
func HandoverStats(c *gin.Context) {
	now := time.Now()
	count := func(extra func(*gorm.DB) *gorm.DB) int64 {
		q := buildHandoverQuery(c)
		if extra != nil {
			q = extra(q)
		}
		var n int64
		_ = q.Count(&n).Error
		return n
	}
	c.JSON(http.StatusOK, gin.H{
		"total":       count(nil),
		"pending":     count(func(q *gorm.DB) *gorm.DB { return q.Where("status = ?", models.HandoverPending) }),
		"in_progress": count(func(q *gorm.DB) *gorm.DB { return q.Where("status = ?", models.HandoverInProgress) }),
		"done":        count(func(q *gorm.DB) *gorm.DB { return q.Where("status = ?", models.HandoverDone) }),
		"returned":    count(func(q *gorm.DB) *gorm.DB { return q.Where("status = ?", models.HandoverReturned) }),
		"urgent":      count(func(q *gorm.DB) *gorm.DB { return q.Where("priority = ? AND status <> ?", models.HandoverUrgent, models.HandoverDone) }),
		"overdue": count(func(q *gorm.DB) *gorm.DB {
			return q.Where("due_at IS NOT NULL AND due_at < ? AND status <> ?", now, models.HandoverDone)
		}),
		"server_time": now.Format("2006-01-02 15:04:05"),
	})
}

// ListHandoverEvents GET /workspace/handovers/:id/events
// 处理时间线：谁在什么时候接手 / 更新进度 / 完成 / 退回 / 催办。
func ListHandoverEvents(c *gin.Context) {
	var hv models.WorkHandover
	if err := db.DB.First(&hv, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "交接不存在"})
		return
	}
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	if !handoverIsAssignee(hv, cl.UserID) && hv.SenderID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该交接"})
		return
	}
	var events []models.WorkHandoverEvent
	if err := db.DB.Where("handover_id = ?", hv.ID).Order("id asc").Find(&events).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, events)
}

// UrgeHandover POST /workspace/handovers/:id/urge
// 催办：发送人（或超管）对久未接手的交接提醒接收人。记录次数与时间，并追加时间线事件。
func UrgeHandover(c *gin.Context) {
	var hv models.WorkHandover
	if err := db.DB.First(&hv, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "交接不存在"})
		return
	}
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	if hv.SenderID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅发送人可催办"})
		return
	}
	if hv.Status == models.HandoverDone {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该交接已完成，无需催办"})
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)

	now := time.Now()
	hv.UrgeCount++
	hv.LastUrgeAt = &now
	if err := db.DB.Save(&hv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "请尽快处理这份交接"
	}
	recordHandoverEvent(hv.ID, "urge", cl.UserID, cl.Username, note)

	// 通知全部接收人
	for _, uid := range handoverAssigneeIDs(hv) {
		db.DB.Create(&models.Notification{
			UserID:  uid,
			Kind:    "user",
			Title:   "催办：交接「" + hv.Title + "」待你处理",
			Content: cl.Username + " 提醒你尽快处理这份交接。" + note,
			ActorID: cl.UserID, ActorName: cl.Username,
		})
	}
	addLog(c, cl.UserID, cl.Username, "催办交接["+hv.Title+"]（第 "+strconv.Itoa(hv.UrgeCount)+" 次）")
	c.JSON(http.StatusOK, hv)
}

// ==================== 工作台通用附件（日志 / 交接） ====================

const (
	wsModuleLog      = "log"
	wsModuleHandover = "handover"
)

// canAccessWSRef 判断当前用户能否访问某个日志/交接（读权限）
func canAccessWSRef(c *gin.Context, module string, refID uint) bool {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return false
	}
	if cl.Role == models.RoleSuperAdmin {
		return true
	}
	switch module {
	case wsModuleLog:
		var lg models.WorkLog
		if err := db.DB.First(&lg, refID).Error; err != nil {
			return false
		}
		if lg.OwnerID == cl.UserID {
			return true
		}
		return lg.Scope == models.ScopeDepartment && lg.DeptID == cl.DeptID
	case wsModuleHandover:
		var hv models.WorkHandover
		if err := db.DB.First(&hv, refID).Error; err != nil {
			return false
		}
		return handoverIsAssignee(hv, cl.UserID) || hv.SenderID == cl.UserID
	}
	return false
}

// canWriteWSRef 判断当前用户能否给某个日志/交接加附件（写权限）
func canWriteWSRef(c *gin.Context, module string, refID uint) bool {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return false
	}
	if cl.Role == models.RoleSuperAdmin {
		return true
	}
	switch module {
	case wsModuleLog:
		var lg models.WorkLog
		if err := db.DB.First(&lg, refID).Error; err != nil {
			return false
		}
		return lg.OwnerID == cl.UserID
	case wsModuleHandover:
		var hv models.WorkHandover
		if err := db.DB.First(&hv, refID).Error; err != nil {
			return false
		}
		// 交接的参与者（发送人与任一接收人）都可补充附件材料
		return handoverIsAssignee(hv, cl.UserID) || hv.SenderID == cl.UserID
	}
	return false
}

// ListWSAttachments GET /workspace/attachments?module=log&ref_id=1
func ListWSAttachments(c *gin.Context) {
	module := strings.TrimSpace(c.Query("module"))
	refID, err := strconv.ParseUint(strings.TrimSpace(c.Query("ref_id")), 10, 64)
	if err != nil || refID == 0 || (module != wsModuleLog && module != wsModuleHandover) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if !canAccessWSRef(c, module, uint(refID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该附件列表"})
		return
	}
	var list []models.WSFileAttachment
	if err := db.DB.Where("module = ? AND ref_id = ?", module, refID).Order("id asc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// UploadWSAttachment POST /workspace/attachments （multipart：module / ref_id / file）
func UploadWSAttachment(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(maxAttachSize); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上传体积超过限制（单文件 ≤ 100MB）"})
		return
	}
	module := strings.TrimSpace(c.PostForm("module"))
	refID, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("ref_id")), 10, 64)
	if err != nil || refID == 0 || (module != wsModuleLog && module != wsModuleHandover) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if !canWriteWSRef(c, module, uint(refID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权为该内容添加附件"})
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要上传的文件"})
		return
	}
	if file.Size > maxAttachSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单个附件不能超过 100MB"})
		return
	}
	cl := middleware.GetClaims(c)
	origName := filepath.Base(file.Filename)
	ext := strings.ToLower(filepath.Ext(origName))
	mimeT := mime.TypeByExtension(ext)
	if mimeT == "" {
		mimeT = "application/octet-stream"
	}
	dir := kAttachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "目录创建失败: " + err.Error()})
		return
	}
	// 存储名含纳秒串前缀 w，既保证唯一也便于人工识别来源；下载以它为 key，杜绝按自增 ID 枚举
	stored := fmt.Sprintf("w%s%d_%d%s", module[:1], refID, time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, stored)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败: " + err.Error()})
		return
	}
	att := models.WSFileAttachment{
		Module: module, RefID: uint(refID), FileName: origName, StoredName: stored,
		Mime: mimeT, Size: file.Size, OwnerID: cl.UserID, OwnerName: cl.Username,
	}
	if err := db.DB.Create(&att).Error; err != nil {
		_ = os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if module == wsModuleHandover {
		recordHandoverEvent(uint(refID), "note", cl.UserID, cl.Username, "上传附件："+origName)
	}
	addLog(c, cl.UserID, cl.Username, "工作台附件上传("+module+")："+origName)
	c.JSON(http.StatusOK, att)
}

// DownloadWSAttachment GET /workspace/attachments/:key/download
// :key 为不可猜的 stored_name（含纳秒串），图片/PDF 内联预览，其余触发下载。
func DownloadWSAttachment(c *gin.Context) {
	key := filepath.Base(c.Param("key"))
	var att models.WSFileAttachment
	if err := db.DB.Where("stored_name = ?", key).First(&att).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	p := filepath.Join(kAttachmentDir(), filepath.Base(att.StoredName))
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件文件不存在"})
		return
	}
	disp := "attachment"
	if strings.HasPrefix(att.Mime, "image/") || att.Mime == "application/pdf" {
		disp = "inline"
	}
	enc := url.QueryEscape(att.FileName)
	c.Header("Content-Type", att.Mime)
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disp, enc, enc))
	c.File(p)
}

// DeleteWSAttachment DELETE /workspace/attachments/:id
// 仅上传者、所属内容创建者（发送人/日志本人）或超管可删。
func DeleteWSAttachment(c *gin.Context) {
	var att models.WSFileAttachment
	if err := db.DB.First(&att, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	allowed := cl.Role == models.RoleSuperAdmin || att.OwnerID == cl.UserID
	if !allowed {
		switch att.Module {
		case wsModuleLog:
			var lg models.WorkLog
			if err := db.DB.First(&lg, att.RefID).Error; err == nil && lg.OwnerID == cl.UserID {
				allowed = true
			}
		case wsModuleHandover:
			var hv models.WorkHandover
			if err := db.DB.First(&hv, att.RefID).Error; err == nil && hv.SenderID == cl.UserID {
				allowed = true
			}
		}
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅上传者或内容创建者可删除"})
		return
	}
	_ = os.Remove(filepath.Join(kAttachmentDir(), filepath.Base(att.StoredName)))
	if err := db.DB.Delete(&att).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "工作台附件删除："+att.FileName)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// removeWSAttachments 级联清理某个日志/交接的全部附件（先删磁盘文件再删记录）
func removeWSAttachments(module string, refID uint) {
	var list []models.WSFileAttachment
	if err := db.DB.Where("module = ? AND ref_id = ?", module, refID).Find(&list).Error; err != nil {
		return
	}
	for _, a := range list {
		_ = os.Remove(filepath.Join(kAttachmentDir(), filepath.Base(a.StoredName)))
	}
	db.DB.Where("module = ? AND ref_id = ?", module, refID).Delete(&models.WSFileAttachment{})
}
