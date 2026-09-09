package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// allowedOrigins 由 CORS_ORIGINS 环境变量（逗号分隔）初始化，为空时视为「未配置白名单」。
var allowedOrigins = loadOrigins()

func loadOrigins() map[string]bool {
	m := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			m[strings.TrimRight(o, "/")] = true
		}
	}
	return m
}

// CORS 允许浏览器插件（chrome-extension://）与 Web/PWA 跨域调用 API。
// 使用 Bearer Token 鉴权（无 cookie）。
//
// 相比旧的默认 `*`，这里做了受控收紧，避免任意站点在持有 token 的情况下读取响应：
//   - Origin 为 chrome-extension://（浏览器插件）或为空/同源（curl、同站、后端跳转）：放行 *；
//   - 否则若命中 CORS_ORIGINS 白名单，回显该 Origin；
//   - 未命中白名单的跨站 Origin 不授予 Access-Control-Allow-Origin（浏览器会拦截）。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allow := ""
		if origin == "" || strings.HasPrefix(origin, "chrome-extension://") ||
			strings.HasPrefix(origin, "moz-extension://") {
			// 插件 / 同源 / 无 Origin（curl、服务端调用）：放行
			allow = "*"
		} else if len(allowedOrigins) > 0 && allowedOrigins[strings.TrimRight(origin, "/")] {
			// 命中配置的白名单来源：回显该来源
			allow = origin
		} else if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") {
			// 本地开发（前端 dev server 跨源调后端）放行；生产无此 Origin，不影响安全
			allow = origin
		}
		if allow != "" {
			c.Header("Access-Control-Allow-Origin", allow)
			c.Header("Vary", "Origin")
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
