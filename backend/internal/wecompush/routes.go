package wecompush

import "github.com/gin-gonic/gin"

// RegisterRoutes 把企微推送的全部接口注册到给定路由组（调用方需保证该组已登录鉴权）。
// 所有路径以 /wecom-push 为前缀，与 GZT 既有 /api/tasks（排班任务）等完全隔离，零冲突。
//
// 访问控制：除 /access（任何登录用户可读，前端据此决定导航项是否显示）外，
// 其余接口统一走 AccessGuard —— 默认仅管理员（超管 + 部门管理员），
// 白名单可由超级管理员在设置页调整。
func RegisterRoutes(rg *gin.RouterGroup, h *H) {
	// 权限查询：不上闸门（见 GetAccess 注释）
	rg.GET("/access", h.GetAccess)
	rg.PUT("/access", h.UpdateAccess)

	// 以下全部接口：必须通过模块访问闸门
	g := rg.Group("")
	g.Use(h.AccessGuard())

	g.GET("/summary", h.Summary)
	g.GET("/cli-status", h.CLIStatusHandler)
	g.GET("/groups", h.ListGroups)
	g.GET("/fields", h.ListFields)
	g.PUT("/settings", h.UpdateSettings)
	g.GET("/settings/full", h.AllSettings)
	g.POST("/notify/test", h.SendTestNotify)

	// 重新授权（扫码 / 手动 Bot ID + Secret）
	g.POST("/auth/start", h.StartWecomAuth)
	g.GET("/auth/status", h.WecomAuthStatus)
	g.GET("/auth/qr", h.AuthQR)
	g.POST("/auth/cancel", h.CancelWecomAuth)
	g.POST("/auth/manual", h.WecomAuthManual)

	g.GET("/tasks", h.ListTasks)
	g.POST("/tasks", h.CreateTask)
	g.PUT("/tasks/:id", h.UpdateTask)
	g.DELETE("/tasks/:id", h.DeleteTask)
	g.POST("/tasks/:id/toggle", h.ToggleTask)
	g.POST("/tasks/:id/run", h.RunTask)
	g.GET("/tasks/:id/sql", h.PreviewSQL)

	g.GET("/logs", h.ListLogs)
	g.GET("/logs/export", h.ExportLogs) // v0.41.1：运行日志导出（xlsx/csv，可按状态分表）
	g.GET("/files/:name", h.DownloadFile) // v0.40.8：运行日志附件下载
}
