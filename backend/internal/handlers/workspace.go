package handlers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/config"
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

// ==================== 知识库：变更 / 协作日志（v0.8.0） ====================

// recordKnowledgeLog 写一条知识条目变更日志（操作人/操作时间/操作前后内容）
func recordKnowledgeLog(entryID uint, cl *models.Claims, action, detail string) {
	if cl == nil {
		return
	}
	_ = db.DB.Create(&models.KnowledgeChangeLog{
		EntryID:      entryID,
		Action:       action,
		OperatorID:   cl.UserID,
		OperatorName: cl.Username,
		DeptID:       cl.DeptID,
		Detail:       detail,
	}).Error
}

// clipRunes 截断长文本用于日志明细（中文按字符计）
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ListKnowledgeHistory GET /workspace/knowledge/:id/history
// 知识条目变更/协作日志（按时间倒序）：完整记录谁、何时、改了什么（含修改前/后内容）
func ListKnowledgeHistory(c *gin.Context) {
	if _, ok := loadVisibleKnowledge(c); !ok {
		return
	}
	var list []models.KnowledgeChangeLog
	db.DB.Where("entry_id = ?", c.Param("id")).Order("id desc").Find(&list)
	c.JSON(http.StatusOK, list)
}

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
		// 编辑期（条目尚未保存）上传到中转缓存的附件 id：内嵌图片 + 下方「附件」列表文件
		TempAttachmentIDs []uint `json:"temp_attachment_ids"`
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
	// 转正编辑期中转缓存的附件（内嵌图片 + 显式上传文件），改写正文里的临时图片地址
	if len(req.TempAttachmentIDs) > 0 || strings.Contains(req.Content, "data-temp-id=") {
		if adopted := adoptTempAttachments(req.Content, entry.ID, req.TempAttachmentIDs, cl); adopted != req.Content {
			entry.Content = adopted
			db.DB.Model(&entry).Update("content", adopted)
			recordKnowledgeLog(entry.ID, cl, "attachment_adopt", "新建时转正编辑期中转缓存的附件（图片随正文保存，文件进入附件列表）")
		}
	}
	addLog(c, cl.UserID, cl.Username, "新增知识库: "+entry.Title)
	recordKnowledgeLog(entry.ID, cl, "create",
		fmt.Sprintf("创建条目：标题「%s」、分类「%s」、可见范围「%s」。正文：%s",
			entry.Title, entry.Category, scopeLabelShort(entry.Scope), clipRunes(entry.Content, 200)))
	c.JSON(http.StatusOK, entry)
}

// loadOwnedKnowledge 加载一条且必须是当前用户本人创建（编辑/删除前校验）；
// 超级管理员可对任何条目进行编辑/删除（用于清理别人的共享知识）
func loadOwnedKnowledge(c *gin.Context) (*models.KnowledgeEntry, bool) {
	var entry models.KnowledgeEntry
	if err := db.DB.First(&entry, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return nil, false
	}
	cl := middleware.GetClaims(c)
	if entry.OwnerID != cl.UserID && cl.Role != "super_admin" {
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
	cl := middleware.GetClaims(c)
	var req struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Content  string `json:"content"`
		Scope    string `json:"scope"`
		// 编辑期（新建未保存时）上传到中转缓存的附件 id
		TempAttachmentIDs []uint `json:"temp_attachment_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题不能为空"})
		return
	}
	// 记录修改前的旧值（用于变更日志的「修改前/后内容」）
	oldTitle, oldCategory, oldContent, oldScope := entry.Title, entry.Category, entry.Content, entry.Scope
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
	// 转正编辑期中转缓存的附件（内嵌图片 + 显式上传文件）
	if len(req.TempAttachmentIDs) > 0 || strings.Contains(req.Content, "data-temp-id=") {
		if adopted := adoptTempAttachments(req.Content, entry.ID, req.TempAttachmentIDs, cl); adopted != req.Content {
			entry.Content = adopted
			db.DB.Model(&entry).Update("content", adopted)
		}
	}
	addLog(c, cl.UserID, cl.Username, "更新知识库: "+entry.Title)
	// 变更/协作日志：逐字段记录「修改前 → 修改后」
	var parts []string
	if oldTitle != entry.Title {
		parts = append(parts, fmt.Sprintf("标题：%q → %q", oldTitle, entry.Title))
	}
	if oldCategory != entry.Category {
		parts = append(parts, fmt.Sprintf("分类：%q → %q", oldCategory, entry.Category))
	}
	if oldScope != entry.Scope {
		parts = append(parts, fmt.Sprintf("可见范围：%s → %s", scopeLabelShort(oldScope), scopeLabelShort(entry.Scope)))
	}
	if oldContent != entry.Content {
		parts = append(parts, fmt.Sprintf("正文：\n【修改前】%s\n【修改后】%s",
			clipRunes(oldContent, 400), clipRunes(entry.Content, 400)))
	}
	if len(parts) == 0 {
		parts = append(parts, "内容未发生变化（仅刷新时间）")
	}
	recordKnowledgeLog(entry.ID, cl, "update", strings.Join(parts, "\n"))
	c.JSON(http.StatusOK, entry)
}

// DeleteKnowledge 删除知识条目（创建者本人 / 超级管理员）
func DeleteKnowledge(c *gin.Context) {
	entry, ok := loadOwnedKnowledge(c)
	if !ok {
		return
	}
	// 先删附件：DB 记录 + 磁盘文件，避免留下孤儿
	var atts []models.KnowledgeAttachment
	if err := db.DB.Where("entry_id = ?", entry.ID).Find(&atts).Error; err == nil && len(atts) > 0 {
		for _, a := range atts {
			if a.StoredName != "" {
				_ = os.Remove(filepath.Join(kAttachmentDir(), a.StoredName))
			}
		}
		if err := db.DB.Where("entry_id = ?", entry.ID).Delete(&models.KnowledgeAttachment{}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := db.DB.Delete(entry).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cl := middleware.GetClaims(c)
	addLog(c, cl.UserID, cl.Username, "删除知识库: "+entry.Title)
	recordKnowledgeLog(entry.ID, cl, "delete", fmt.Sprintf("删除条目：标题「%s」（条目及其附件一并删除）", entry.Title))
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
// 接收者包含主接收人 + AssigneeIDs JSON 数组中包含当前用户（多接收人兼容）
func handoverVisibleQ(c *gin.Context, q *gorm.DB) *gorm.DB {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return q.Where("1 = 0")
	}
	if cl.Role == models.RoleSuperAdmin {
		return q // 超管可看全部交接，便于统筹
	}
	uidStr := fmt.Sprintf("%d", cl.UserID)
	return q.Where("sender_id = ? OR assignee_id = ? OR assignee_ids LIKE ? OR assignee_ids LIKE ? OR assignee_ids LIKE ?",
		cl.UserID, cl.UserID,
		"%"+uidStr+",%",
		"["+uidStr+",%",
		"%"+uidStr+"]%",
	)
}

// ListHandovers 交接列表。?role=inbox(我收到的) / outbox(我发出的) / 默认全部相关
func ListHandovers(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.WorkHandover{})
	q = handoverVisibleQ(c, q)
	switch c.Query("role") {
	case "inbox":
		// 多接收人兼容：主接收人 OR JSON 数组里有当前用户 ID（SQLite 用 LIKE 匹配 "id":<UID> 形式）
		uidStr := fmt.Sprintf("%d", cl.UserID)
		q = q.Where("assignee_id = ? OR assignee_ids LIKE ? OR assignee_ids LIKE ? OR assignee_ids LIKE ?",
			cl.UserID,
			"%"+uidStr+",%",  // 数组中间：...,X,...
			"["+uidStr+",%",  // 数组开头：[X, ...
			"%"+uidStr+"]%")  // 数组结尾：... ,X]
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
	// 给前端回填 AssigneeNames（如果数据库里只填了 ID，没填姓名）
	hydrateHandoverNames(&list)
	c.JSON(http.StatusOK, list)
}

// hydrateHandoverNames 列表回填：AssigneeNames 为空时，根据 AssigneeIDs 查表补全
func hydrateHandoverNames(list *[]models.WorkHandover) {
	idSet := map[uint]bool{}
	for i := range *list {
		hv := &(*list)[i]
		if hv.AssigneeNames == "" && hv.AssigneeIDs != "" {
			for _, id := range handoverAssigneeIDs(*hv) {
				idSet[id] = true
			}
		}
	}
	if len(idSet) == 0 {
		return
	}
	ids := make([]uint, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	var us []models.User
	if err := db.DB.Select("id, name").Where("id IN ?", ids).Find(&us).Error; err != nil {
		return
	}
	nameByID := make(map[uint]string, len(us))
	for _, u := range us {
		nameByID[u.ID] = u.Name
	}
	for i := range *list {
		hv := &(*list)[i]
		if hv.AssigneeNames != "" {
			continue
		}
		allIDs := handoverAssigneeIDs(*hv)
		names := make([]string, 0, len(allIDs))
		for _, id := range allIDs {
			if n, ok := nameByID[id]; ok {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			nb, _ := json.Marshal(names)
			hv.AssigneeNames = string(nb)
		}
	}
}

// CreateHandover 新建交接单（接收人可为多人；同时给所有接收人发通知）
func CreateHandover(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var req struct {
		Title        string `json:"title"`
		FromProgress string `json:"from_progress"` // 我做到哪了
		Todo         string `json:"todo"`          // 需要接收人继续做的
		AssigneeID   uint   `json:"assignee_id"`   // 单接收人（兼容旧版/单选）
		AssigneeIDs  []uint `json:"assignee_ids"`  // 多接收人：系统人员 ID 数组
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
	// 合并单/多接收人，去重
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
	// 校验所有接收人都是系统注册人员，收集姓名
	var us []models.User
	if err := db.DB.Where("id IN ?", cleaned).Find(&us).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(us) != len(cleaned) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "部分接收人不存在，请刷新后重试"})
		return
	}
	// 按请求顺序排序
	nameByID := make(map[uint]string, len(us))
	idOrder := make(map[uint]int, len(cleaned))
	for i, id := range cleaned {
		idOrder[id] = i
	}
	sort.SliceStable(us, func(i, j int) bool { return idOrder[us[i].ID] < idOrder[us[j].ID] })
	orderedNames := make([]string, 0, len(us))
	for _, u := range us {
		orderedNames = append(orderedNames, u.Name)
		nameByID[u.ID] = u.Name
	}
	idsJSON, _ := json.Marshal(cleaned)
	namesJSON, _ := json.Marshal(orderedNames)
	hv := models.WorkHandover{
		Title:         req.Title,
		FromProgress:  req.FromProgress,
		Todo:          req.Todo,
		SenderID:      cl.UserID,
		SenderName:    cl.Username,
		AssigneeID:    cleaned[0],       // 主接收人（兼容）
		AssigneeName:  nameByID[cleaned[0]],
		AssigneeIDs:   string(idsJSON),  // 全员 ID（JSON 数组）
		AssigneeNames: string(namesJSON),// 全员姓名（JSON 数组）
		DeptID:        cl.DeptID,
		Status:        models.HandoverPending,
	}
	if err := db.DB.Create(&hv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 给每个接收人写一条站内通知
	for _, u := range us {
		db.DB.Create(&models.Notification{
			UserID: u.ID, Kind: "user", Title: "你收到一份新的工作交接",
			Content: "来自 " + cl.Username + "：「" + hv.Title + "」请到「工作台-交接接力」查看并继续处理。",
			ActorID: cl.UserID, ActorName: cl.Username,
		})
	}
	addLog(c, cl.UserID, cl.Username, "创建交接→"+strings.Join(orderedNames, "、")+": "+hv.Title)
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
	// 仅发送人与任一接收人可操作；超管兜底（兼容旧数据：AssigneeID 非 0 即视为单接收人）
	if !handoverIsAssignee(hv, cl.UserID) && hv.SenderID != cl.UserID && cl.Role != models.RoleSuperAdmin {
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

// handoverAssigneeIDs 返回交接的所有接收人 ID 列表（去重；含主接收人）
func handoverAssigneeIDs(hv models.WorkHandover) []uint {
	ids := make([]uint, 0, 4)
	if hv.AssigneeID != 0 {
		ids = append(ids, hv.AssigneeID)
	}
	if hv.AssigneeIDs == "" {
		return ids
	}
	var arr []uint
	if err := json.Unmarshal([]byte(hv.AssigneeIDs), &arr); err != nil {
		return ids
	}
	seen := map[uint]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, id := range arr {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// handoverIsAssignee 判断用户是否是该交接的接收人之一
func handoverIsAssignee(hv models.WorkHandover, userID uint) bool {
	for _, id := range handoverAssigneeIDs(hv) {
		if id == userID {
			return true
		}
	}
	return false
}

// ==================== 知识库：附件（图片/文档） ====================

// kAttachmentDir 返回知识附件存储目录（与数据库同盘，便于随数据目录一起备份/迁移）
func kAttachmentDir() string {
	dir := filepath.Dir(config.C.DBPath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	if dir != "/" {
		dir = strings.TrimRight(dir, "/")
	}
	return filepath.Join(dir, "workspace_attachments")
}

// kCanRead 判断当前用户能否读取某条知识（创建者本人或部门共享同部门或超管）
func kCanRead(cl *models.Claims, e *models.KnowledgeEntry) bool {
	if cl == nil {
		return false
	}
	if cl.Role == models.RoleSuperAdmin {
		return true
	}
	if e.OwnerID == cl.UserID {
		return true
	}
	return e.Scope == models.ScopeDepartment && e.DeptID == cl.DeptID
}

// loadVisibleKnowledge 加载一条对当前用户可见的知识（不存在或不可见→返回 false 并已响应）
func loadVisibleKnowledge(c *gin.Context) (*models.KnowledgeEntry, bool) {
	var e models.KnowledgeEntry
	if err := db.DB.First(&e, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "知识条目不存在"})
		return nil, false
	}
	cl := middleware.GetClaims(c)
	if !kCanRead(cl, &e) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该条目"})
		return nil, false
	}
	return &e, true
}

const maxAttachSize = 100 << 20 // 单附件上限 100MB（正式环境存储充足）

// ListKnowledgeAttachments GET /workspace/knowledge/:id/attachments 附件列表
func ListKnowledgeAttachments(c *gin.Context) {
	if _, ok := loadVisibleKnowledge(c); !ok {
		return
	}
	var list []models.KnowledgeAttachment
	db.DB.Where("entry_id = ?", c.Param("id")).Order("id asc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// UploadKnowledgeAttachment POST /workspace/knowledge/:id/attachments 上传附件
// 表单字段名 file。仅条目创建者或超管可传。
func UploadKnowledgeAttachment(c *gin.Context) {
	e, ok := loadVisibleKnowledge(c)
	if !ok {
		return
	}
	cl := middleware.GetClaims(c)
	if e.OwnerID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅条目创建者可上传附件"})
		return
	}
	// P0-1：先把多部分解析上限拉到 maxAttachSize，否则 32MB+ 的上传会被标准库直接截断
	if err := c.Request.ParseMultipartForm(maxAttachSize); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上传体积超过限制（单文件 ≤ 100MB）"})
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
	// 支持任意文件类型：图片预览，其余一律下载。MIME 按扩展名识别，识别不到回退二进制。
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
	stored := fmt.Sprintf("k%d_%d%s", e.ID, time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, stored)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败: " + err.Error()})
		return
	}
	att := models.KnowledgeAttachment{
		EntryID: e.ID, FileName: origName, StoredName: stored,
		Mime: mimeT, Size: file.Size, OwnerID: cl.UserID, OwnerName: cl.Username,
	}
	if err := db.DB.Create(&att).Error; err != nil {
		_ = os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = db.DB.Model(&models.KnowledgeEntry{}).Where("id = ?", e.ID).Update("updated_at", time.Now())
	addLog(c, cl.UserID, cl.Username, "知识["+e.Title+"]上传附件: "+origName)
	recordKnowledgeLog(e.ID, cl, "attachment_upload",
		fmt.Sprintf("新增附件「%s」（%d 字节，类型 %s）", origName, file.Size, mimeT))
	c.JSON(http.StatusOK, att)
}

// DownloadKnowledgeAttachment GET /workspace/knowledge/attachments/:aid/download
// 附件下载/预览。可见者均可读；图片以 inline 预览，其余以附件下载。
func DownloadKnowledgeAttachment(c *gin.Context) {
	var att models.KnowledgeAttachment
	if err := db.DB.First(&att, c.Param("aid")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	var e models.KnowledgeEntry
	if err := db.DB.First(&e, att.EntryID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属条目不存在"})
		return
	}
	// 附件下载对外公开：文件名不可猜测（stored_name 含纳秒随机串），
	// 这样富文本内嵌图片可用 <img src> 直接渲染（浏览器 img 请求不携带 Authorization）。
	p := filepath.Join(kAttachmentDir(), filepath.Base(att.StoredName))
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件文件不存在"})
		return
	}
	// 图片内嵌预览，其余触发下载（attachment 强制浏览器下载）
	isImage := strings.HasPrefix(att.Mime, "image/")
	disp := "attachment"
	if isImage {
		disp = "inline"
	}
	// RFC 5987 编码文件名，中文可正常显示
	enc := url.QueryEscape(att.FileName)
	c.Header("Content-Type", att.Mime)
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disp, "attachment", enc))
	c.File(p)
}

// DeleteKnowledgeAttachment DELETE /workspace/knowledge/attachments/:aid
// 仅条目创建者或上传者或超管可删
func DeleteKnowledgeAttachment(c *gin.Context) {
	var att models.KnowledgeAttachment
	if err := db.DB.First(&att, c.Param("aid")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	var e models.KnowledgeEntry
	_ = db.DB.First(&e, att.EntryID)
	cl := middleware.GetClaims(c)
	if cl.Role != models.RoleSuperAdmin && !(e.ID != 0 && e.OwnerID == cl.UserID) && att.OwnerID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅条目创建者或上传者可删除"})
		return
	}
	p := filepath.Join(kAttachmentDir(), filepath.Base(att.StoredName))
	if err := os.Remove(p); err != nil {
		// 文件可能已被手动清理，不阻断 DB 记录删除
	}
	db.DB.Delete(&att)
	addLog(c, cl.UserID, cl.Username, "删除知识附件: "+att.FileName)
	if e.ID != 0 {
		recordKnowledgeLog(e.ID, cl, "attachment_delete", fmt.Sprintf("删除附件「%s」", att.FileName))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ==================== 知识库：附件中转缓存（保存前上传） ====================

// tempAttachmentDir 返回中转缓存目录（与正式附件目录同级，便于转正时直接移动）
func tempAttachmentDir() string {
	dir := filepath.Dir(config.C.DBPath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	if dir != "/" {
		dir = strings.TrimRight(dir, "/")
	}
	return filepath.Join(dir, "workspace_temp")
}

// cleanupTempAttachments 回收 24h 前的孤儿中转文件（用户上传后未保存即关闭页面时产生）
func cleanupTempAttachments() {
	cutoff := time.Now().Add(-24 * time.Hour)
	var olds []models.KnowledgeTempAttachment
	db.DB.Where("created_at < ?", cutoff).Find(&olds)
	if len(olds) == 0 {
		return
	}
	tDir := tempAttachmentDir()
	for _, o := range olds {
		_ = os.Remove(filepath.Join(tDir, filepath.Base(o.StoredName)))
	}
	db.DB.Where("created_at < ?", cutoff).Delete(&models.KnowledgeTempAttachment{})
}

// UploadTempAttachment POST /workspace/temp-attachments
// 新建条目（尚无 id）时，富文本粘贴/选择的图片与文件先上传到中转缓存。
// 返回 download_url 可直接用于 <img src>（端点公开、文件名不可猜测）；
// 用户确认保存后由 CreateKnowledge/UpdateKnowledge 转正为正式附件。
func UploadTempAttachment(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// 懒清理：回收 24h 前的孤儿中转文件，避免无限堆积
	cleanupTempAttachments()

	if err := c.Request.ParseMultipartForm(maxAttachSize); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上传体积超过限制（单文件 ≤ 100MB）"})
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
	origName := filepath.Base(file.Filename)
	ext := strings.ToLower(filepath.Ext(origName))
	mimeT := mime.TypeByExtension(ext)
	if mimeT == "" {
		mimeT = "application/octet-stream"
	}
	dir := tempAttachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "目录创建失败: " + err.Error()})
		return
	}
	stored := fmt.Sprintf("t%d_%d%s", cl.UserID, time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, stored)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败: " + err.Error()})
		return
	}
	att := models.KnowledgeTempAttachment{
		FileName:   origName,
		StoredName: stored,
		Mime:       mimeT,
		Size:       file.Size,
		OwnerID:    cl.UserID,
		OwnerName:  cl.Username,
	}
	if err := db.DB.Create(&att).Error; err != nil {
		_ = os.Remove(dst)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":          att.ID,
		"file_name":   att.FileName,
		"mime":        att.Mime,
		"size":        att.Size,
		"download_url": "/api/workspace/temp-attachments/" + strconv.FormatUint(uint64(att.ID), 10) + "/download",
	})
}

// DownloadTempAttachment GET /workspace/temp-attachments/:id/download
// 中转缓存文件公开可读（文件名不可猜测）；仅用于编辑期预览，转正后即失效。
func DownloadTempAttachment(c *gin.Context) {
	var att models.KnowledgeTempAttachment
	if err := db.DB.First(&att, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	p := filepath.Join(tempAttachmentDir(), filepath.Base(att.StoredName))
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件文件不存在"})
		return
	}
	isImage := strings.HasPrefix(att.Mime, "image/")
	disp := "attachment"
	if isImage {
		disp = "inline"
	}
	enc := url.QueryEscape(att.FileName)
	c.Header("Content-Type", att.Mime)
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disp, "attachment", enc))
	c.File(p)
}

// adoptTempAttachments 把中转缓存里的附件「转正」为某知识条目的正式附件：
// 1) 扫描 content 中的 data-temp-id（富文本内嵌图片）
// 2) 合并显式传入的 temp_attachment_ids（编辑区下方「附件」列表里上传的文件）
// 统一移动文件 + 建 KnowledgeAttachment 记录，并把 content 里的临时图片地址改写为正式地址。
func adoptTempAttachments(content string, entryID uint, explicitTempIDs []uint, cl *models.Claims) string {
	re := regexp.MustCompile(`data-temp-id="(\d+)"`)
	seen := map[uint]bool{}
	var ids []uint
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if id, err := strconv.ParseUint(m[1], 10, 64); err == nil && !seen[uint(id)] {
			seen[uint(id)] = true
			ids = append(ids, uint(id))
		}
	}
	for _, id := range explicitTempIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return content
	}
	tDir := tempAttachmentDir()
	kDir := kAttachmentDir()
	_ = os.MkdirAll(kDir, 0o755)
	for _, tid := range ids {
		var t models.KnowledgeTempAttachment
		if err := db.DB.First(&t, tid).Error; err != nil {
			continue
		}
		src := filepath.Join(tDir, filepath.Base(t.StoredName))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(kDir, filepath.Base(t.StoredName))
		if err := os.Rename(src, dst); err != nil {
			if cpErr := copyFile(src, dst); cpErr == nil {
				_ = os.Remove(src)
			} else {
				continue
			}
		}
		att := models.KnowledgeAttachment{
			EntryID:    entryID,
			FileName:   t.FileName,
			StoredName: t.StoredName,
			Mime:       t.Mime,
			Size:       t.Size,
			OwnerID:    t.OwnerID,
			OwnerName:  t.OwnerName,
		}
		if err := db.DB.Create(&att).Error; err != nil {
			_ = os.Rename(dst, src) // 入库失败回滚文件
			continue
		}
		db.DB.Delete(&t)
		oldSrc := "/api/workspace/temp-attachments/" + strconv.FormatUint(uint64(tid), 10) + "/download"
		newSrc := "/api/workspace/knowledge_attachments/" + strconv.FormatUint(uint64(att.ID), 10) + "/download"
		content = strings.ReplaceAll(content, oldSrc, newSrc)
		content = strings.ReplaceAll(content, `data-temp-id="`+strconv.FormatUint(uint64(tid), 10)+`"`, "")
	}
	return content
}

// ==================== 知识库：导出（当前可见条目） ====================

// ExportKnowledge GET /workspace/knowledge/export
// 将当前用户可见的知识条目导出为带 BOM 的文本文件（.txt），按分类分组。
func ExportKnowledge(c *gin.Context) {
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	// 尊重列表筛选
	if kw := strings.TrimSpace(c.Query("kw")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("title LIKE ? OR content LIKE ? OR category LIKE ?", like, like, like)
	}
	if cat := strings.TrimSpace(c.Query("category")); cat != "" {
		q = q.Where("category = ?", cat)
	}
	var list []models.KnowledgeEntry
	if err := q.Order("category asc, updated_at desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var b strings.Builder
	b.WriteString("知识库导出 共 " + fmt.Sprintf("%d", len(list)) + " 条\n")
	b.WriteString("导出时间: " + time.Now().Format("2006-01-02 15:04") + "\n")
	b.WriteString(strings.Repeat("=", 40) + "\n\n")
	var lastCat string
	for _, k := range list {
		cat := k.Category
		if cat == "" {
			cat = "未分类"
		}
		if cat != lastCat {
			if lastCat != "" {
				b.WriteString("\n")
			}
			b.WriteString("【分类：" + cat + "】\n")
			lastCat = cat
		}
		b.WriteString("▪ " + k.Title + "\n")
		if k.Content != "" {
			b.WriteString(k.Content + "\n")
		}
		b.WriteString("  （" + scopeLabelShort(k.Scope) + " · 更新于 " + k.UpdatedAt.Format("2006-01-02") + "）\n\n")
	}
	fname := fmt.Sprintf("knowledge_export_%s.txt", time.Now().Format("20060102_1504"))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fname, url.QueryEscape(fname)))
	// 写 UTF-8 BOM，便于记事本/Excel 正确识别中文
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(b.String())...)
	c.Data(200, "text/plain; charset=utf-8", body)
}

func scopeLabelShort(s models.WorkspaceScope) string {
	if s == models.ScopePrivate {
		return "仅自己"
	}
	return "部门共享"
}

// ==================== 工作台：全库打包导出（zip，含附件，供跨环境迁移/备份） ====================

// ExportWorkspaceBundle GET /workspace/export/bundle
// 将当前用户可见的知识库条目 + 工作日志 + 交接（文本）汇总为一份 README.txt，
// 并把可见知识条目涉及的全部附件文件一并打包成 zip 下载。附件目录随数据目录一起走。
func ExportWorkspaceBundle(c *gin.Context) {
	cl := middleware.GetClaims(c)
	// 知识（含附件）
	var entries []models.KnowledgeEntry
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	q.Order("category asc, updated_at desc").Find(&entries)

	// 收集可见知识条目涉及的附件 ID（附件只随所属条目可见性走）
	visibleEntryIDs := make([]uint, 0, len(entries))
	visibleIDs := map[uint]bool{}
	for _, e := range entries {
		if !visibleIDs[e.ID] {
			visibleIDs[e.ID] = true
			visibleEntryIDs = append(visibleEntryIDs, e.ID)
		}
	}
	var atts []models.KnowledgeAttachment
	if len(visibleEntryIDs) > 0 {
		db.DB.Where("entry_id IN ?", visibleEntryIDs).Order("id asc").Find(&atts)
	}
	attByEntry := map[uint][]models.KnowledgeAttachment{}
	for _, a := range atts {
		attByEntry[a.EntryID] = append(attByEntry[a.EntryID], a)
	}

	// 日志（个人；共享同部门）——须按可见性过滤，避免导出他部门/他人私密日志
	var logs []models.WorkLog
	logQ := scopeVisibleQ(c, db.DB.Model(&models.WorkLog{}))
	logQ.Order("log_date desc, id desc").Find(&logs)
	// 交接（本人相关；超管全量）
	var handovers []models.WorkHandover
	hq := db.DB.Model(&models.WorkHandover{})
	hq = handoverVisibleQ(c, hq)
	hq.Order("id desc").Find(&handovers)

	var b strings.Builder
	b.WriteString("GZT 工作台数据导出\n")
	b.WriteString("导出时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	b.WriteString("导出人: " + cl.Username + "\n")
	b.WriteString(strings.Repeat("=", 40) + "\n\n")

	b.WriteString("◆ 迷你知识库（" + fmt.Sprintf("%d", len(entries)) + " 条）\n")
	var lastCat string
	for _, e := range entries {
		cat := e.Category
		if cat == "" {
			cat = "未分类"
		}
		if cat != lastCat {
			b.WriteString("\n【" + cat + "】\n")
			lastCat = cat
		}
		b.WriteString("▪ " + e.Title + "  [" + scopeLabelShort(e.Scope) + "] 更新" + e.UpdatedAt.Format("2006-01-02") + "\n")
		if e.Content != "" {
			b.WriteString(e.Content + "\n")
		}
		if as, ok := attByEntry[e.ID]; ok && len(as) > 0 {
			b.WriteString("  附件: ")
			for _, a := range as {
				b.WriteString(a.FileName + "  ")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("=", 40) + "\n◆ 工作日志（" + fmt.Sprintf("%d", len(logs)) + " 篇）\n")
	for _, l := range logs {
		b.WriteString("▪ " + l.LogDate + " " + l.Title + "  [" + scopeLabelShort(l.Scope) + "]\n")
		if l.Done != "" {
			b.WriteString("  ✅ " + l.Done + "\n")
		}
		if l.Pending != "" {
			b.WriteString("  ⏳ " + l.Pending + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("=", 40) + "\n◆ 交接接力（" + fmt.Sprintf("%d", len(handovers)) + " 条）\n")
	statusLabel := map[string]string{"pending": "待接手", "in_progress": "处理中", "done": "已完成"}
	for _, h := range handovers {
		b.WriteString("▪ " + h.Title + "  " + h.SenderName + " → " + h.AssigneeName + "  [" + statusLabel[h.Status] + "]\n")
		if h.FromProgress != "" {
			b.WriteString("  进展: " + h.FromProgress + "\n")
		}
		if h.Todo != "" {
			b.WriteString("  待做: " + h.Todo + "\n")
		}
		if h.Note != "" {
			b.WriteString("  备注: " + h.Note + "\n")
		}
		b.WriteString("\n")
	}

	// 打包 zip：README.txt + attachments/{entryID}_{filename}
	// 直接流向响应流（chunked），避免大附件时整包进内存。
	fname := fmt.Sprintf("workspace_bundle_%s.zip", time.Now().Format("20060102_1504"))
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fname, url.QueryEscape(fname)))
	c.Status(http.StatusOK)
	c.Writer.Flush()

	zw := zip.NewWriter(c.Writer)
	readme := append([]byte{0xEF, 0xBB, 0xBF}, []byte(b.String())...)
	rw, _ := zw.Create("README.txt")
	_, _ = rw.Write(readme)

	attDir := kAttachmentDir()
	for _, a := range atts {
		src := filepath.Join(attDir, filepath.Base(a.StoredName))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		fw, err := zw.Create("attachments/" + fmt.Sprintf("%d_%s", a.EntryID, a.FileName))
		if err != nil {
			continue
		}
		if err := copyFileToZip(fw, src); err != nil {
			continue
		}
	}
	zw.Close()
}

func copyFileToZip(w io.Writer, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}
