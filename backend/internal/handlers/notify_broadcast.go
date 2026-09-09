package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// BroadcastNotification POST /api/notifications/broadcast 给某部门（含子孙）全部成员发广播通知。
// - 超管可指定任意 dept_id（必选）；
// - 部门管理员只能发给本人可管理的部门（dept_id 为空则默认本人部门）；
// 冻结账号不接收。广播含发送者本人（区别于 notifyUser 的"自己操作自己不发"）。
func BroadcastNotification(c *gin.Context) {
	var req struct {
		DeptID  uint   `json:"dept_id"` // 0=默认本人部门（部门管理员）
		Title   string `json:"title"`
		Content string `json:"content"`
		Link    string `json:"link"` // 附带超链接（可选，仅 http/https）
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

	// 确定目标部门集合（含子孙）
	var ids []uint
	if canManageAny(c) {
		// 超管：必须显式选择部门
		if req.DeptID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请选择部门"})
			return
		}
		ids = descendantDeptIDs(req.DeptID)
	} else {
		// 部门管理员：默认本人部门，或可管理范围内的指定部门
		if req.DeptID == 0 {
			ids = descendantDeptIDs(cl.DeptID)
		} else {
			if !canManageDept(c, req.DeptID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权给该部门发通知"})
				return
			}
			ids = descendantDeptIDs(req.DeptID)
		}
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
	now := time.Now()
	rows := make([]models.Notification, 0, len(users))
	for _, u := range users {
		rows = append(rows, models.Notification{
			UserID:    u.ID,
			Kind:      "broadcast",
			Title:     req.Title,
			Content:   req.Content,
			Link:      req.Link,
			ActorID:   cl.UserID,
			ActorName: claimsName(cl),
			Read:      false,
			CreatedAt: now,
		})
	}
	if err := db.DB.Create(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	deptName := ""
	var d models.Department
	if err := db.DB.First(&d, ids[0]).Error; err == nil {
		deptName = d.Name
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("广播通知「%s」给 %s（%d 人）", req.Title, deptName, len(users)))
	c.JSON(http.StatusOK, gin.H{"sent": len(users), "dept_name": deptName})
}

// canManageAny 判断当前用户是否超管（可管理任意部门）。用于广播的目标部门约束。
func canManageAny(c *gin.Context) bool {
	cl := currentClaims(c)
	if cl == nil {
		return false
	}
	return cl.Role == models.RoleSuperAdmin
}
