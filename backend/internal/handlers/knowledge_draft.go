package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// draftKey 把请求参数收敛成唯一的草稿定位键（按用户隔离）。
// 已有条目走 entry_id；新建条目走前端生成的 client_draft_id。
type draftLocator struct {
	EntryID       uint
	ClientDraftID string
}

// parseDraftLocator 从请求体或查询参数解析定位键，并做基本校验。
func parseDraftLocator(c *gin.Context) (draftLocator, bool) {
	var loc draftLocator
	// 优先取查询参数（GET / DELETE 用），其次取请求体（PUT 用）
	entryID := uint(0)
	if v := c.Query("entry_id"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &entryID)
	}
	clientID := strings.TrimSpace(c.Query("client_draft_id"))
	if entryID == 0 && clientID == "" {
		// PUT 可能走 body
		var b struct {
			EntryID       uint   `json:"entry_id"`
			ClientDraftID string `json:"client_draft_id"`
		}
		if c.Request.Method == "PUT" {
			_ = c.ShouldBindJSON(&b)
			entryID = b.EntryID
			clientID = strings.TrimSpace(b.ClientDraftID)
		}
	}
	loc.EntryID = entryID
	loc.ClientDraftID = clientID
	if entryID == 0 && clientID == "" {
		return loc, false
	}
	return loc, true
}

// findDraft 按 (user_id, 定位键) 查唯一草稿。
func findDraft(cl *models.Claims, loc draftLocator) *models.KnowledgeDraft {
	q := db.DB.Where("user_id = ?", cl.UserID)
	if loc.EntryID > 0 {
		q = q.Where("entry_id = ?", loc.EntryID)
	} else {
		q = q.Where("entry_id = 0 AND client_draft_id = ?", loc.ClientDraftID)
	}
	var d models.KnowledgeDraft
	if err := q.First(&d).Error; err != nil {
		return nil
	}
	return &d
}

// draftScopeQuery 组装定位草稿的 WHERE 子句（供 GET/DELETE 复用）。
func draftScopeQuery(q *gorm.DB, cl *models.Claims, loc draftLocator) *gorm.DB {
	qq := q.Where("user_id = ?", cl.UserID)
	if loc.EntryID > 0 {
		qq = qq.Where("entry_id = ?", loc.EntryID)
	} else {
		qq = qq.Where("entry_id = 0 AND client_draft_id = ?", loc.ClientDraftID)
	}
	return qq
}

// SaveKnowledgeDraft PUT /api/workspace/knowledge/draft
// 自动保存草稿（双写中的服务端一侧）。按 (user_id, entry_id|client_draft_id) upsert。
func SaveKnowledgeDraft(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var req struct {
		EntryID       uint     `json:"entry_id"`
		ClientDraftID string   `json:"client_draft_id"`
		Kind          string   `json:"kind"`
		Content       string   `json:"content"`
		Title         string   `json:"title"`
		Category      string   `json:"category"`
		Scope         string   `json:"scope"`
		Tags          []string `json:"tags"`
		ParentID      uint     `json:"parent_id"`
		Status        string   `json:"status"`
		EditorIDs     []uint   `json:"editor_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.ClientDraftID = strings.TrimSpace(req.ClientDraftID)
	req.Kind = normalizeKind(req.Kind) // 空/未知回落 doc，与服务端条目口径一致

	// 定位键：已有条目用 entry_id，新建用 client_draft_id（必填）
	if req.EntryID == 0 && req.ClientDraftID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 entry_id 或 client_draft_id"})
		return
	}

	// 已有条目：必须对该条目可编辑，读者不能往他人条目塞草稿
	if req.EntryID > 0 {
		var entry models.KnowledgeEntry
		if err := db.DB.First(&entry, req.EntryID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
			return
		}
		if !kCanEdit(cl, &entry) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权编辑该条目，无法保存草稿"})
			return
		}
	}

	now := time.Now()
	draft := models.KnowledgeDraft{
		UserID:        cl.UserID,
		EntryID:       req.EntryID,
		ClientDraftID: req.ClientDraftID,
		Kind:          req.Kind,
		Content:       req.Content,
		Title:         req.Title,
		Category:      strings.TrimSpace(req.Category),
		Scope:         normKbScope(cl, req.Scope),
		Tags:          normTags(req.Tags),
		ParentID:      req.ParentID,
		Status:        req.Status,
		EditorIDs:     normIDList(req.EditorIDs),
		SavedAt:       now,
	}
	// upsert：同一用户同一目标只有一份草稿（自动保存会高频覆盖）
	if existing := findDraft(cl, draftLocator{EntryID: req.EntryID, ClientDraftID: req.ClientDraftID}); existing != nil {
		draft.ID = existing.ID
		draft.CreatedAt = existing.CreatedAt
		if err := db.DB.Model(&models.KnowledgeDraft{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"kind": req.Kind, "content": req.Content, "title": req.Title, "category": draft.Category,
			"scope": draft.Scope, "tags": draft.Tags, "parent_id": req.ParentID,
			"status": req.Status, "editor_ids": draft.EditorIDs, "saved_at": now,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if err := db.DB.Create(&draft).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "saved_at": now})
}

// GetKnowledgeDraft GET /api/workspace/knowledge/draft?entry_id=..&client_draft_id=..
// 取当前用户的草稿（恢复时前端聚合本地 + 服务端，取最新）。
func GetKnowledgeDraft(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	loc, ok := parseDraftLocator(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 entry_id 或 client_draft_id"})
		return
	}
	d := findDraft(cl, loc)
	if d == nil {
		c.JSON(http.StatusOK, gin.H{"draft": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"draft": draftToDTO(d)})
}

// DeleteKnowledgeDraft DELETE /api/workspace/knowledge/draft?entry_id=..&client_draft_id=..
// 保存成功后清理（双向清理的其中一侧；前端负责清本地 IndexedDB）。
func DeleteKnowledgeDraft(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	loc, ok := parseDraftLocator(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 entry_id 或 client_draft_id"})
		return
	}
	if err := draftScopeQuery(db.DB, cl, loc).Delete(&models.KnowledgeDraft{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// draftToDTO 把存储行转成前端可直接回填的草稿对象（tags / editor_ids 还原成数组）。
func draftToDTO(d *models.KnowledgeDraft) gin.H {
	var tags []string
	_ = json.Unmarshal([]byte(d.Tags), &tags)
	var editorIDs []uint
	_ = json.Unmarshal([]byte(d.EditorIDs), &editorIDs)
	return gin.H{
		"id":              d.ID,
		"user_id":         d.UserID,
		"entry_id":        d.EntryID,
		"client_draft_id": d.ClientDraftID,
		"kind":            d.Kind,
		"content":         d.Content,
		"title":           d.Title,
		"category":        d.Category,
		"scope":           d.Scope,
		"tags":            tags,
		"parent_id":       d.ParentID,
		"status":          d.Status,
		"editor_ids":      editorIDs,
		"saved_at":        d.SavedAt,
	}
}
