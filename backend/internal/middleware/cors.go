package middleware

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// CORS 允许浏览器插件（chrome-extension://）与 Web/PWA 跨域调用 API。
// 使用 Bearer Token 鉴权（无 cookie），故 AllowOrigin 可用 "*" 且不开启 credentials。
func CORS() gin.HandlerFunc {
	origins := os.Getenv("CORS_ORIGINS")
	return func(c *gin.Context) {
		if origins == "" {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			c.Header("Access-Control-Allow-Origin", origins)
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
