package handlers

import (
	"net/http"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============ 工作台：迷你知识库 / 工作日志 / 交接接力 ============
// 每类内容都有「可见范围」scope：private(仅创建者) / department(同部门共享)。
// 读写权限：创建者本人可改删自己的；共享内容同部门成员可读（超管可读全部）。

// scopeVisibleQ 追加「当前用户可见」的过滤条件到查询。
// 规则：private 仅本人；department 为同部门（及超管）。超管不设部门限制。
func scopeVisibleQ(c *gin.Context, q *gorm.DB) *gorm.DB {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return q.Where("1 = 0")
	}
	if cl.Role == models.RoleSuperAdmin {
		return q // 超管可见全部
	}
	// 本人(含所有 scope) 或 同部门共享
	return q.Where("(owner_id = ?) OR (scope = ? AND dept_id = ?)",
		cl.UserID, models.ScopeDepartment, cl.DeptID)
}

// ============================ 迷你知识库 ============================

// ListKnowledge 知识库列表（可见范围内），支持 ?kw 全文搜索、?category 分类、?mine 只看自己
func ListKnowledge(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	if kw := strings.TrimSpace(c.Query("kw")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("title LIKE ? OR content LIKE ? OR category LIKE ?", like, like, like)
	}
	if cat := strings.TrimSpace(c.Query("category")); cat != "" {
		q = q.Where("category = ?", cat)
	}
	if c.Query("mine") == "1" && cl != nil {
		q = q.Where("owner_id = ?", cl.UserID)
	}
	var list []models.KnowledgeEntry
	if err := q.Order("updated_at desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ListKnowledgeCategories 当前可见范围内出现的全部分类（去重，用于筛选下拉）
func ListKnowledgeCategories(c *gin.Context) {
	q := db.DB.Model(&models.KnowledgeEntry{}).Select("DISTINCT category")
	q = scopeVisibleQ(c, q)
	var cats []string
	if err := q.Pluck("category", &cats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cats)
}

// CreateKnowledge 新建知识条目
func CreateKnowledge(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var req struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Content  string `json:"content"`
		Scope    string `json:"scope"` // private | department
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题不能为空"})
		return
	}
	if req.Scope != string(models.ScopePrivate) {
		req.Scope = string(models.ScopeDepartment) // 默认同部门共享
	}
	entry := models.KnowledgeEntry{
		Title:     req.Title,
		Category:  strings.TrimSpace(req.Category),
		Content:   req.Content,
		Scope:     models.WorkspaceScope(req.Scope),
		OwnerID:   cl.UserID,
		OwnerName: cl.Username,
		DeptID:    cl.DeptID,
	}
	if err := db.DB.Create(&entry).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "新增知识库: "+entry.Title)
	c.JSON(http.StatusOK, entry)
}

// loadOwnedKnowledge 加载一条且必须是当前用户本人创建（编辑/删除前校验）
func loadOwnedKnowledge(c *gin.Context) (*models.KnowledgeEntry, bool) {
	var entry models.KnowledgeEntry
	if err := db.DB.First(&entry, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return nil, false
	}
	cl := middleware.GetClaims(c)
	if entry.OwnerID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅创建者可编辑或删除"})
		return nil, false
	}
	return &entry, true
}

// UpdateKnowledge 更新知识条目（仅创建者）
func UpdateKnowledge(c *gin.Context) {
	entry, ok := loadOwnedKnowledge(c)
	if !ok {
		return
	}
	var req struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Content  string `json:"content"`
		Scope    string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题不能为空"})
		return
	}
	entry.Title = strings.TrimSpace(req.Title)
	entry.Category = strings.TrimSpace(req.Category)
	entry.Content = req.Content
	if req.Scope == string(models.ScopePrivate) || req.Scope == string(models.ScopeDepartment) {
		entry.Scope = models.WorkspaceScope(req.Scope)
	}
	if err := db.DB.Save(entry).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, middleware.GetClaims(c).UserID, middleware.GetClaims(c).Username, "更新知识库: "+entry.Title)
	c.JSON(http.StatusOK, entry)
}

// DeleteKnowledge 删除知识条目（仅创建者）
func DeleteKnowledge(c *gin.Context) {
	entry, ok := loadOwnedKnowledge(c)
	if !ok {
		return
	}
	if err := db.DB.Delete(entry).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, middleware.GetClaims(c).UserID, middleware.GetClaims(c).Username, "删除知识库: "+entry.Title)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================ 工作日志 ============================

// ListWorkLogs 指定日期/月的工作日志。个人日志默认仅自己可见（private）；共享可见同部门。
// 查询：?date=YYYY-MM-DD 精确某天，或 ?month=YYYY-MM 按月取（供日历高亮）。
func ListWorkLogs(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.WorkLog{})
	if date := strings.TrimSpace(c.Query("date")); date != "" {
		q = q.Where("log_date = ?", date)
	}
	if month := strings.TrimSpace(c.Query("month")); month != "" {
		q = q.Where("log_date LIKE ?", month+"-%")
	}
	if onlyMine := c.Query("mine") == "1"; onlyMine {
		// 我写的日志：可能我自己看（含共享给我的？日志定位偏个人，mine=1 只取自己）
		q = q.Where("owner_id = ?", cl.UserID)
	} else {
		// 共享视图：本人 + 同部门共享（部门日志本）
		q = scopeVisibleQ(c, q)
	}
	var list []models.WorkLog
	if err := q.Order("log_date desc, id desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// CreateWorkLog 新增工作日志（默认本人 private，可共享）
func CreateWorkLog(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var req struct {
		LogDate string `json:"log_date"`
		Title   string `json:"title"`
		Done    string `json:"done"`    // 今天做了什么
		Pending string `json:"pending"` // 还没做完的
		Scope   string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.LogDate = strings.TrimSpace(req.LogDate)
	if req.LogDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择日期"})
		return
	}
	if req.Scope != string(models.ScopeDepartment) {
		req.Scope = string(models.ScopePrivate) // 日志默认私密，可手动共享
	}
	log := models.WorkLog{
		LogDate:   req.LogDate,
		Title:     strings.TrimSpace(req.Title),
		Done:      req.Done,
		Pending:   req.Pending,
		Scope:     models.WorkspaceScope(req.Scope),
		OwnerID:   cl.UserID,
		OwnerName: cl.Username,
		DeptID:    cl.DeptID,
	}
	if err := db.DB.Create(&log).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, log)
}

// loadOwnedLog 编辑/删除前校验本人
func loadOwnedLog(c *gin.Context) (*models.WorkLog, bool) {
	var log models.WorkLog
	if err := db.DB.First(&log, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "日志不存在"})
		return nil, false
	}
	cl := middleware.GetClaims(c)
	if log.OwnerID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅本人可编辑或删除"})
		return nil, false
	}
	return &log, true
}

// UpdateWorkLog 更新日志（仅本人）
func UpdateWorkLog(c *gin.Context) {
	log, ok := loadOwnedLog(c)
	if !ok {
		return
	}
	var req struct {
		LogDate string `json:"log_date"`
		Title   string `json:"title"`
		Done    string `json:"done"`
		Pending string `json:"pending"`
		Scope   string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.LogDate) != "" {
		log.LogDate = strings.TrimSpace(req.LogDate)
	}
	log.Title = strings.TrimSpace(req.Title)
	log.Done = req.Done
	log.Pending = req.Pending
	if req.Scope == string(models.ScopePrivate) || req.Scope == string(models.ScopeDepartment) {
		log.Scope = models.WorkspaceScope(req.Scope)
	}
	if err := db.DB.Save(log).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, log)
}

// DeleteWorkLog 删除日志（仅本人）
func DeleteWorkLog(c *gin.Context) {
	log, ok := loadOwnedLog(c)
	if !ok {
		return
	}
	if err := db.DB.Delete(log).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================ 交接接力 ============================

// handoverVisibleQ：本人相关（发送者或接收者）。非超管也仅能看与自己相关的交接。
func handoverVisibleQ(c *gin.Context, q *gorm.DB) *gorm.DB {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return q.Where("1 = 0")
	}
	if cl.Role == models.RoleSuperAdmin {
		return q // 超管可看全部交接，便于统筹
	}
	return q.Where("sender_id = ? OR assignee_id = ?", cl.UserID, cl.UserID)
}

// ListHandovers 交接列表。?role=inbox(我收到的) / outbox(我发出的) / 默认全部相关
func ListHandovers(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.WorkHandover{})
	q = handoverVisibleQ(c, q)
	switch c.Query("role") {
	case "inbox":
		q = q.Where("assignee_id = ?", cl.UserID)
	case "outbox":
		q = q.Where("sender_id = ?", cl.UserID)
	}
	if st := strings.TrimSpace(c.Query("status")); st != "" && st != "all" {
		q = q.Where("status = ?", st)
	}
	var list []models.WorkHandover
	if err := q.Order("id desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// CreateHandover 新建交接单（接收人为系统注册人员）
func CreateHandover(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var req struct {
		Title        string `json:"title"`
		FromProgress string `json:"from_progress"` // 我做到哪了
		Todo         string `json:"todo"`          // 需要接收人继续做的
		AssigneeID   uint   `json:"assignee_id"`   // 接收人(系统人员ID)
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "交接标题不能为空"})
		return
	}
	if req.AssigneeID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择接收人"})
		return
	}
	if req.AssigneeID == cl.UserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能交接给自己"})
		return
	}
	// 校验接收人是系统注册人员，并取其姓名
	var u models.User
	if err := db.DB.First(&u, req.AssigneeID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "接收人不存在"})
		return
	}
	hv := models.WorkHandover{
		Title:        req.Title,
		FromProgress: req.FromProgress,
		Todo:         req.Todo,
		SenderID:     cl.UserID,
		SenderName:   cl.Username,
		AssigneeID:   u.ID,
		AssigneeName: u.Name,
		DeptID:       cl.DeptID,
		Status:       models.HandoverPending,
	}
	if err := db.DB.Create(&hv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 写站内通知给接收人，确保"下个人能看到"
	db.DB.Create(&models.Notification{
		UserID: u.ID, Kind: "user", Title: "你收到一份新的工作交接",
		Content:   "来自 " + cl.Username + "：「" + hv.Title + "」请到「工作台-交接接力」查看并继续处理。",
		ActorID:   cl.UserID, ActorName: cl.Username,
	})
	addLog(c, cl.UserID, cl.Username, "创建交接→"+u.Name+": "+hv.Title)
	c.JSON(http.StatusOK, hv)
}

// UpdateHandoverStatus 接收人更新交接状态：in_progress(接手处理中) / done(完成并填备注)
func UpdateHandoverStatus(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var hv models.WorkHandover
	if err := db.DB.First(&hv, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "交接不存在"})
		return
	}
	// 仅发送人与接收人可操作；超管兜底
	if hv.AssigneeID != cl.UserID && hv.SenderID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅发送人或接收人可更新"})
		return
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	switch req.Status {
	case models.HandoverInProgress, models.HandoverDone, models.HandoverPending:
		hv.Status = req.Status
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效状态"})
		return
	}
	hv.Note = req.Note
	if req.Status == models.HandoverDone {
		now := time.Now()
		hv.CompletedAt = &now
	}
	if err := db.DB.Save(&hv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "交接["+hv.Title+"]状态→"+hv.Status)
	c.JSON(http.StatusOK, hv)
}

// DeleteHandover 删除交接（仅发送人；超管兜底）
func DeleteHandover(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var hv models.WorkHandover
	if err := db.DB.First(&hv, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "交接不存在"})
		return
	}
	if hv.SenderID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅发送人可删除"})
		return
	}
	if err := db.DB.Delete(&hv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
