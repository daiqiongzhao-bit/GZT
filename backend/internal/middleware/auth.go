package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/session"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// ClaimsKey 上下文中当前用户键
const CtxUserKey = "claims"

// AuthRequired JWT 鉴权
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未提供有效的认证令牌"})
			return
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims := &models.Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return config.C.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "令牌无效或已过期"})
			return
		}
		// 令牌版本校验：用户 token_version 自增后，旧 token 立即失效（登出/强制下线）
		var u models.User
		if err := db.DB.Select("token_version, name").First(&u, claims.UserID).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
			return
		}
		if u.TokenVersion != claims.Version {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "令牌已失效，请重新登录"})
			return
		}
		c.Set(CtxUserKey, claims)
		// 会话跟踪：记录/刷新在线活跃（登录方式来自 token 的 client 声明）
		session.Track(tokenStr, claims.UserID, claims.Username, u.Name, claims.DeptID, string(claims.Client), c.ClientIP(), c.Request.UserAgent())
		c.Next()
	}
}

// GetClaims 从上下文取当前用户
func GetClaims(c *gin.Context) *models.Claims {
	v, ok := c.Get(CtxUserKey)
	if !ok {
		return nil
	}
	cl, ok := v.(*models.Claims)
	if !ok {
		return nil
	}
	return cl
}

// RequireRole 角色校验
func RequireRole(roles ...models.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := GetClaims(c)
		if cl == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			return
		}
		ok := false
		for _, r := range roles {
			if cl.Role == r {
				ok = true
				break
			}
		}
		// 超级管理员拥有所有权限
		if cl.Role == models.RoleSuperAdmin {
			ok = true
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		c.Next()
	}
}

// GenerateToken 签发 JWT（自动写入用户当前 token_version 与客户端类型）
func GenerateToken(cl *models.Claims, client models.ClientType) (string, error) {
	now := time.Now()
	// 取用户最新 token_version，使签发令牌绑定当前版本
	var u models.User
	version := cl.Version
	if err := db.DB.Select("token_version").First(&u, cl.UserID).Error; err == nil {
		version = u.TokenVersion
	}
	if client == "" {
		client = models.ClientWeb
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid":      cl.UserID,
		"username": cl.Username,
		"role":     cl.Role,
		"dept_id":  cl.DeptID,
		"ver":      version,
		"client":   string(client),
		"iat":      now.Unix(),
		"exp":      now.Add(72 * time.Hour).Unix(),
	})
	return token.SignedString(config.C.JWTSecret)
}

// Logout 当前用户登出：自增自身 token_version 使当前 token 失效
func Logout(c *gin.Context) {
	cl := GetClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	if err := db.DB.Model(&models.User{}).Where("id = ?", cl.UserID).
		Update("token_version", gorm.Expr("token_version + 1")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登出失败"})
		return
	}
	session.RemoveByUser(cl.UserID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ForceLogout 超管强制指定用户下线：自增目标用户 token_version
func ForceLogout(c *gin.Context) {
	cl := GetClaims(c)
	if cl == nil || cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅超级管理员可执行"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户ID无效"})
		return
	}
	if err := db.DB.Model(&models.User{}).Where("id = ?", id).
		Update("token_version", gorm.Expr("token_version + 1")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "强制下线失败"})
		return
	}
	session.RemoveByUser(uint(id))
	_ = db.DB.Create(&models.Log{UserID: cl.UserID, UserName: cl.Username, Action: "强制用户下线: " + strconv.Itoa(id)}).Error
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
