package wecompush

import "github.com/gin-gonic/gin"

// RegisterRoutes 把企微推送的全部接口注册到给定路由组（调用方需保证该组已登录鉴权）。
// 所有路径以 /wecom-push 为前缀，与 GZT 既有 /api/tasks（排班任务）等完全隔离，零冲突。
func RegisterRoutes(rg *gin.RouterGroup, h *H) {
	rg.GET("/summary", h.Summary)
	rg.GET("/cli-status", h.CLIStatusHandler)
	rg.GET("/groups", h.ListGroups)
	rg.GET("/fields", h.ListFields)
	rg.PUT("/settings", h.UpdateSettings)
	rg.GET("/settings/full", h.AllSettings)
	rg.POST("/notify/test", h.SendTestNotify)

	// 重新授权（扫码 / 手动 Bot ID + Secret）
	rg.POST("/auth/start", h.StartWecomAuth)
	rg.GET("/auth/status", h.WecomAuthStatus)
	rg.GET("/auth/qr", h.AuthQR)
	rg.POST("/auth/cancel", h.CancelWecomAuth)
	rg.POST("/auth/manual", h.WecomAuthManual)

	rg.GET("/tasks", h.ListTasks)
	rg.POST("/tasks", h.CreateTask)
	rg.PUT("/tasks/:id", h.UpdateTask)
	rg.DELETE("/tasks/:id", h.DeleteTask)
	rg.POST("/tasks/:id/toggle", h.ToggleTask)
	rg.POST("/tasks/:id/run", h.RunTask)
	rg.GET("/tasks/:id/sql", h.PreviewSQL)

	rg.GET("/logs", h.ListLogs)
}
