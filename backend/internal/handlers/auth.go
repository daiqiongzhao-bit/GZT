package handlers

import (
	"net/http"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/session"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type loginReq struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	ClientType string `json:"client_type"` // 可选：web/pwa/extension，默认 web
}

// Login 账号密码登录
func Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	clientIP := c.ClientIP()
	throttleKey := req.Username + "|" + clientIP
	if locked, left := checkLoginThrottle(throttleKey); locked {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "尝试次数过多，账号已临时锁定，请 " + left + " 秒后再试"})
		return
	}
	var user models.User
	if err := db.DB.Preload("Dept").Where("username = ?", req.Username).First(&user).Error; err != nil {
		recordLoginFailure(throttleKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		recordLoginFailure(throttleKey)
		addLog(c, user.ID, user.Name, "登录失败（密码错误）来自 "+clientIP)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if user.Frozen {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号已被冻结，请联系管理员"})
		return
	}
	resetLoginFailure(throttleKey)
	claims := &models.Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		DeptID:   user.DeptID,
	}
	client := models.ClientWeb
	if req.ClientType != "" {
		client = models.ClientType(req.ClientType)
	}
	token, err := middleware.GenerateToken(claims, client)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "令牌生成失败"})
		return
	}
	// 会话跟踪：登录即记录在线（登录方式 / IP / UA）
	session.Track(token, user.ID, user.Username, user.Name, user.DeptID, string(client), clientIP, c.Request.UserAgent())
	addLog(c, user.ID, user.Name, "登录系统")
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

// Me 当前用户信息
func Me(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var user models.User
	if err := db.DB.Preload("Dept").First(&user, cl.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, user)
}

type changePasswordReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword 自助修改密码（需验证旧密码）
func ChangePassword(c *gin.Context) {
	cl := middleware.GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少 6 位"})
		return
	}
	var user models.User
	if err := db.DB.First(&user, cl.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前密码不正确"})
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err := db.DB.Model(&user).Update("password_hash", string(hash)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, user.ID, user.Name, "修改个人密码")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
