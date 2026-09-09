package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// BroadcastNotification POST /api/notifications/broadcast 给部门（可多选/全选，含子孙）全部成员发广播通知。
// - 超管：多选任意部门，或选 0=全部部门；
// - 部门管理员：多选本人可管理的部门（默认本人部门，含子部门）；
// 冻结账号不接收。广播含发送者本人（区别于 notifyUser 的"自己操作自己不发"）。
func BroadcastNotification(c *gin.Context) {
	var req struct {
		DeptIDs     []uint `json:"dept_ids"` // 多选部门；超管可含 0 表示全部
		DeptID      uint   `json:"dept_id"`  // 兼容旧单值
		Title       string `json:"title"`
		Content     string `json:"content"`
		Link        string `json:"link"` // 附带超链接（可选，仅 http/https）
		Attachments []struct {
			ID uint `json:"id"`
		} `json:"attachments"` // 临时附件 id（KnowledgeTempAttachment）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Link = sanitizeLink(req.Link)
	if req.Title == "" || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题与内容均必填"})
		return
	}
	cl := currentClaims(c)

	// 兼容：旧单值 dept_id 当作只有一个元素的多选
	if len(req.DeptIDs) == 0 && req.DeptID != 0 {
		req.DeptIDs = []uint{req.DeptID}
	}

	// 收集目标部门集合（含子孙），去重
	idset := map[uint]bool{}
	add := func(deptID uint) {
		for _, id := range descendantDeptIDs(deptID) {
			idset[id] = true
		}
	}
	if canManageAny(c) {
		hasAll := false
		for _, d := range req.DeptIDs {
			if d == 0 {
				hasAll = true
				break
			}
		}
		if hasAll {
			// 全部部门：从所有顶层部门展开
			var roots []models.Department
			db.DB.Where("parent_id = ? OR parent_id IS NULL", 0).Find(&roots)
			for _, r := range roots {
				add(r.ID)
			}
		} else {
			for _, d := range req.DeptIDs {
				add(d)
			}
		}
	} else {
		// 部门管理员：限定可管理范围
		if len(req.DeptIDs) == 0 {
			req.DeptIDs = []uint{cl.DeptID}
		}
		for _, d := range req.DeptIDs {
			target := d
			if target == 0 {
				target = cl.DeptID
			}
			if !canManageDept(c, target) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权给该部门发通知"})
				return
			}
			add(target)
		}
	}
	ids := make([]uint, 0, len(idset))
	for id := range idset {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择接收部门"})
		return
	}

	var users []models.User
	if err := db.DB.Where("dept_id IN ? AND frozen = ?", ids, false).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(users) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "所选部门无有效成员"})
		return
	}

	// 处理附件：把临时附件落库为永久通知附件
	attJSON := ""
	if len(req.Attachments) > 0 {
		attIDs := make([]uint, 0, len(req.Attachments))
		for _, a := range req.Attachments {
			if a.ID != 0 {
				attIDs = append(attIDs, a.ID)
			}
		}
		atts, err := persistBcastAttachments(attIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "附件处理失败: " + err.Error()})
			return
		}
		if b, err := json.Marshal(atts); err == nil {
			attJSON = string(b)
		}
	}

	now := time.Now()
	rows := make([]models.Notification, 0, len(users))
	for _, u := range users {
		rows = append(rows, models.Notification{
			UserID:      u.ID,
			Kind:        "broadcast",
			Title:       req.Title,
			Content:     req.Content,
			Link:        req.Link,
			Attachments: attJSON,
			ActorID:     cl.UserID,
			ActorName:   claimsName(cl),
			Read:        false,
			CreatedAt:   now,
		})
	}
	if err := db.DB.Create(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	deptName := deptNames(ids)
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("广播通知「%s」给 %s（%d 人）", req.Title, deptName, len(users)))
	c.JSON(http.StatusOK, gin.H{"sent": len(users), "dept_name": deptName, "attachments": len(req.Attachments)})
}

// persistBcastAttachments 把中转临时附件复制到通知附件永久目录，返回元数据列表。
// 仅复制、不删除临时文件（临时文件由既有 24h 回收逻辑清理）。
func persistBcastAttachments(tempIDs []uint) ([]models.NotifAttachment, error) {
	if len(tempIDs) == 0 {
		return nil, nil
	}
	var temps []models.KnowledgeTempAttachment
	db.DB.Where("id IN ?", tempIDs).Find(&temps)
	if len(temps) == 0 {
		return nil, nil
	}
	dir := notifyAttachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tDir := tempAttachmentDir()
	out := make([]models.NotifAttachment, 0, len(temps))
	for _, t := range temps {
		src := filepath.Join(tDir, filepath.Base(t.StoredName))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(dir, filepath.Base(t.StoredName))
		if err := copyFile(src, dst); err != nil {
			continue
		}
		out = append(out, models.NotifAttachment{
			FileName:   t.FileName,
			StoredName: t.StoredName,
			Mime:       t.Mime,
			Size:       t.Size,
		})
	}
	return out, nil
}

// deptNames 取部门 ID 集合里顶层部门的名字，用顿号连接，用于日志/提示。
func deptNames(ids []uint) string {
	var depts []models.Department
	db.DB.Where("id IN ?", ids).Find(&depts)
	names := []string{}
	for _, d := range depts {
		if d.ParentID == 0 {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 && len(depts) > 0 {
		names = append(names, depts[0].Name)
	}
	return strings.Join(names, "、")
}

// notifyAttachmentDir 返回通知附件永久存储目录（与数据库同盘，便于随数据目录一起备份/迁移）。
func notifyAttachmentDir() string {
	dir := filepath.Dir(config.C.DBPath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	if dir != "/" {
		dir = strings.TrimRight(dir, "/")
	}
	return filepath.Join(dir, "notify_attachments")
}

// DownloadNotifAttachment GET /api/notifications/attachments/:key/download
// 通知附件下载（接收人本人或超管可见）；stored_name 不可猜测。
func DownloadNotifAttachment(c *gin.Context) {
	key := filepath.Base(c.Param("key"))
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var notifs []models.Notification
	db.DB.Where("attachments LIKE ?", "%"+key+"%").Find(&notifs)
	allowed := cl.Role == models.RoleSuperAdmin
	if !allowed {
		for _, n := range notifs {
			if n.UserID == cl.UserID {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该附件"})
		return
	}
	// 从匹配通知里解析出文件名/类型
	var meta models.NotifAttachment
	found := false
	for _, n := range notifs {
		var list []models.NotifAttachment
		if err := json.Unmarshal([]byte(n.Attachments), &list); err != nil {
			continue
		}
		for _, a := range list {
			if a.StoredName == key {
				meta = a
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	p := filepath.Join(notifyAttachmentDir(), key)
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	if found && meta.Mime != "" {
		c.Header("Content-Type", meta.Mime)
	}
	if found && meta.FileName != "" {
		c.Header("Content-Disposition", "inline; filename*=UTF-8''"+url.QueryEscape(meta.FileName))
	}
	c.File(p)
}

// canManageAny 判断当前用户是否超管（可管理任意部门）。用于广播的目标部门约束。
func canManageAny(c *gin.Context) bool {
	cl := currentClaims(c)
	if cl == nil {
		return false
	}
	return cl.Role == models.RoleSuperAdmin
}
