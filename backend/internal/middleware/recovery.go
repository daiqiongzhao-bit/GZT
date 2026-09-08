package middleware

import (
	"runtime/debug"

	"shiftworkbench/internal/logger"

	"github.com/gin-gonic/gin"
)

// Recovery 替代 gin.Recovery：在捕获到 panic 时记录 ERROR 级系统日志（含完整堆栈），
// 并返回 500，避免进程崩溃且无痕。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				stack := string(debug.Stack())
				logger.ErrorDetail("server",
					"请求处理发生 panic: %v | %s %s",
					[]interface{}{err, c.Request.Method, c.Request.URL.Path}, stack)
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(500, gin.H{"error": "服务器内部错误，请稍后重试或联系管理员"})
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}

// ErrorLogger 记录响应状态码 >= 500 的请求到系统日志（便于排查线上异常）。
// 注意：仅记录元信息（方法/路径/状态/耗时），不记录响应体，避免泄露敏感内容。
func ErrorLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() >= 500 {
			logger.Error("handler",
				"请求返回 %d | %s %s", c.Writer.Status(), c.Request.Method, c.Request.URL.Path)
		}
	}
}
