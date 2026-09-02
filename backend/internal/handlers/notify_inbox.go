package handlers

import (
	"strconv"
	"strings"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ===================== 站内通知 =====================

// claimsName 取操作人的姓名（Claims 无 name 字段，按 uid 查用户表）
func claimsName(cl *models.Claims) string {
	if cl == nil {
		return ""
	}
	var u models.User
	if err := db.DB.First(&u, cl.UserID).Error; err == nil && u.Name != "" {
		return u.Name
	}
	return cl.Username
}

// notifyUser 给指定用户发一条站内通知（actorID/actorName 为操作人）
func notifyUser(userID uint, kind, title, content string, actorID uint, actorName string) {
	if userID == 0 || actorID == userID {
		return // 无人可发 / 自己操作自己不发
	}
	_ = db.DB.Create(&models.Notification{
		UserID:    userID,
		Kind:      kind,
		Title:     title,
		Content:   content,
		ActorID:   actorID,
		ActorName: actorName,
	}).Error
}

// notifyPeopleByName 按姓名给系统内用户发通知（匹配不到的用户跳过）
func notifyPeopleByName(names []string, kind, titleFmt, contentFmt string, actorID uint, actorName string) {
	if actorID == 0 {
		return
	}
	seen := map[uint]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var users []models.User
		db.DB.Where("name = ?", name).Find(&users)
		for _, u := range users {
			if seen[u.ID] || u.ID == actorID {
				continue
			}
			seen[u.ID] = true
			_ = db.DB.Create(&models.Notification{
				UserID:    u.ID,
				Kind:      kind,
				Title:     titleFmt,
				Content:   contentFmt,
				ActorID:   actorID,
				ActorName: actorName,
			}).Error
		}
	}
}

// ListNotifications GET /api/notifications 我的通知（倒序）
func ListNotifications(c *gin.Context) {
	cl := currentClaims(c)
	var list []models.Notification
	if err := db.DB.Where("user_id = ?", cl.UserID).Order("created_at desc, id desc").Find(&list).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

// UnreadNotificationCount GET /api/notifications/unread-count 未读数
func UnreadNotificationCount(c *gin.Context) {
	cl := currentClaims(c)
	var n int64
	db.DB.Model(&models.Notification{}).Where("user_id = ? AND read = ?", cl.UserID, false).Count(&n)
	c.JSON(200, gin.H{"unread": n})
}

// MarkNotificationRead POST /api/notifications/:id/read 单条已读
func MarkNotificationRead(c *gin.Context) {
	cl := currentClaims(c)
	id, _ := strconv.Atoi(c.Param("id"))
	db.DB.Model(&models.Notification{}).Where("id = ? AND user_id = ?", id, cl.UserID).Update("read", true)
	c.JSON(200, gin.H{"ok": true})
}

// MarkAllNotificationsRead POST /api/notifications/read-all 全部已读
func MarkAllNotificationsRead(c *gin.Context) {
	cl := currentClaims(c)
	db.DB.Model(&models.Notification{}).Where("user_id = ? AND read = ?", cl.UserID, false).Update("read", true)
	c.JSON(200, gin.H{"ok": true})
}
