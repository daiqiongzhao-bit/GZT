package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/handlers"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/service"

	"github.com/gin-gonic/gin"
)

//go:embed all:web/dist/**
var webFS embed.FS

func main() {
	config.Init()
	if err := db.Init(); err != nil {
		panic("数据库初始化失败: " + err.Error())
	}
	service.Seed()
	// 应用系统配置的时区（默认 Asia/Shanghai，可在设置中修改）
	var st models.Setting
	db.DB.FirstOrCreate(&st, models.Setting{ID: 1})
	if st.Timezone == "" {
		st.Timezone = "Asia/Shanghai"
		db.DB.Save(&st)
	}
	if loc, err := time.LoadLocation(st.Timezone); err == nil {
		time.Local = loc
	}
	go handlers.StartBackupScheduler()
	go handlers.StartNotifyScheduler()
	go handlers.StartLogRetentionScheduler()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS())

	api := r.Group("/api")
	{
		// 公开
		api.POST("/auth/login", handlers.Login)
		api.GET("/settings", handlers.GetSetting) // 企业信息公开可读（登录页展示）
		api.GET("/settings/logo", handlers.GetLogo) // 企业 Logo 公开可读（登录页展示）
		api.GET("/version", func(c *gin.Context) {
			c.JSON(200, gin.H{"version": config.C.AppVersion})
		})

		// 需登录
		auth := api.Group("")
		auth.Use(middleware.AuthRequired())
		{
			auth.GET("/auth/me", handlers.Me)
			auth.POST("/auth/change-password", handlers.ChangePassword)
			auth.POST("/auth/unlock", middleware.RequireRole(models.RoleSuperAdmin), handlers.UnlockLogin)
			auth.GET("/dashboard", handlers.Dashboard)
			auth.GET("/logs", handlers.ListLogs)
			auth.GET("/notifications", handlers.ListNotifications)
			auth.GET("/notifications/unread-count", handlers.UnreadNotificationCount)
			auth.POST("/notifications/:id/read", handlers.MarkNotificationRead)
			auth.POST("/notifications/read-all", handlers.MarkAllNotificationsRead)

			// 部门（仅超管写）
			auth.GET("/departments", handlers.ListDepartments)
			auth.POST("/departments", middleware.RequireRole(models.RoleSuperAdmin), handlers.CreateDepartment)
			auth.DELETE("/departments/:id", middleware.RequireRole(models.RoleSuperAdmin), handlers.DeleteDepartment)

			// 部门班次定义（部门管/超管可改本部门）
			auth.GET("/shift-configs", handlers.ListShiftConfigs)
			auth.POST("/shift-configs", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.UpsertShiftConfig)
			auth.DELETE("/shift-configs/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DeleteShiftConfig)

			// 鉴权相关：登出、超管强制下线、在线会话
			auth.POST("/logout", middleware.AuthRequired(), middleware.Logout)
			auth.POST("/users/:id/force-logout", middleware.RequireRole(models.RoleSuperAdmin), middleware.ForceLogout)
			auth.GET("/sessions", middleware.RequireRole(models.RoleSuperAdmin), handlers.GetSessions)

			// 人员
			auth.GET("/users", handlers.ListUsers)
			auth.POST("/users", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.CreateUser)
			auth.PUT("/users/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.UpdateUser)
			auth.POST("/users/:id/reset-password", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ResetPassword)
			auth.DELETE("/users/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DeleteUser)
			auth.POST("/users/import", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ImportUsers)

			// 班表
		auth.GET("/schedules", handlers.ListSchedules)
		auth.POST("/schedules", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.CreateSchedule)
		auth.PUT("/schedules/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.UpdateSchedule)
		auth.DELETE("/schedules/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DeleteSchedule)

			// 任务
			auth.GET("/tasks", handlers.ListTasks)
			auth.GET("/tasks/counts", handlers.TaskCounts)
			auth.POST("/tasks", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.CreateTask)
			auth.PUT("/tasks/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.UpdateTask)
			auth.POST("/tasks/:id/toggle", handlers.ToggleTask)
			auth.GET("/tasks/:id/completions", handlers.ListTaskCompletions)
			auth.DELETE("/tasks/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DeleteTask)
			auth.POST("/tasks/batch-delete", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.BatchDeleteTasks)

			// 任务完成记录审计（部门管/超管可读本部门）
			auth.GET("/completions", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ListCompletions)

			// 批量操作 / 导入导出（部门管/超管）
			auth.POST("/tasks/batch", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.BatchTasks)
			auth.POST("/tasks/import", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ImportTasksCSV)
			auth.POST("/schedules/import", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ImportSchedulesCSV)
			auth.GET("/schedules/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportSchedulesCSV)
			auth.GET("/tasks/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportTasksCSV)
			auth.GET("/logs/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportLogsCSV)

			// 模板管理（管理员可查看下载，超管可修改）
			auth.GET("/templates", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ListTemplates)
			auth.GET("/templates/task-sample", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DownloadTaskSample)
			auth.GET("/templates/schedule-sample", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DownloadScheduleSample)
			auth.GET("/templates/schedule-template", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DownloadScheduleTemplateXLSX)
			auth.GET("/templates/task-template", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DownloadTaskTemplateXLSX)
			auth.GET("/templates/user-template", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DownloadUserTemplateXLSX)
			auth.GET("/templates/:id/download", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DownloadTemplate)
			auth.POST("/templates", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpsertTemplate)
			auth.DELETE("/templates/:id", middleware.RequireRole(models.RoleSuperAdmin), handlers.DeleteTemplate)

			// Webhook
			auth.GET("/webhooks", handlers.ListWebhooks)
			auth.POST("/webhooks", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.CreateWebhook)
			auth.PUT("/webhooks/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.UpdateWebhook)
			auth.POST("/webhooks/notify", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.NotifyTodayHandler)
			auth.DELETE("/webhooks/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DeleteWebhook)
			auth.POST("/webhooks/test", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.TestWebhook)

			// 邮件通知配置（仅超管）
			auth.POST("/settings/smtp", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateSMTP)
			auth.POST("/settings/test-email", middleware.RequireRole(models.RoleSuperAdmin), handlers.TestEmail)

			// 设置（仅超管写）
			auth.POST("/settings", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateSetting)
			auth.POST("/settings/logo", middleware.RequireRole(models.RoleSuperAdmin), handlers.UploadLogo)
			auth.DELETE("/settings/logo", middleware.RequireRole(models.RoleSuperAdmin), handlers.DeleteLogo)
			auth.POST("/settings/log-retention", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateLogRetention)
			auth.POST("/settings/timezone", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateTimezone)

			// 系统备份与还原（仅超管）
			auth.GET("/backups", middleware.RequireRole(models.RoleSuperAdmin), handlers.ListBackupsHandler)
			auth.POST("/backups", middleware.RequireRole(models.RoleSuperAdmin), handlers.CreateBackupHandler)
			auth.GET("/backups/:id/download", middleware.RequireRole(models.RoleSuperAdmin), handlers.DownloadBackupHandler)
			auth.POST("/backups/:id/restore", middleware.RequireRole(models.RoleSuperAdmin), handlers.RestoreBackupHandler)
			auth.DELETE("/backups/:id", middleware.RequireRole(models.RoleSuperAdmin), handlers.DeleteBackupHandler)
			auth.GET("/backup-config", middleware.RequireRole(models.RoleSuperAdmin), handlers.GetBackupConfigHandler)
			auth.POST("/backup-config", middleware.RequireRole(models.RoleSuperAdmin), handlers.SaveBackupConfigHandler)
		}
	}

	// 前端静态资源（embed）
	sub, err := fs.Sub(webFS, "web/dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(sub))
		r.NoRoute(func(c *gin.Context) {
			reqPath := c.Request.URL.Path
			// API 未命中返回 404 JSON
			if strings.HasPrefix(reqPath, "/api/") {
				c.JSON(404, gin.H{"error": "接口不存在"})
				return
			}
			// 静态文件存在则直接服务（避免目录重定向）
			rel := strings.TrimPrefix(reqPath, "/")
			if f, e := sub.Open(rel); e == nil {
				if fi, fe := f.Stat(); fe == nil && !fi.IsDir() {
					f.Close()
					fileServer.ServeHTTP(c.Writer, c.Request)
					return
				}
				f.Close()
			}
			// SPA 回退：返回 index.html
			idx, ie := sub.Open("index.html")
			if ie != nil {
				c.JSON(404, gin.H{"error": "未找到资源"})
				return
			}
			defer idx.Close()
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusOK)
			io.Copy(c.Writer, idx)
		})
	}

	r.Run(":" + config.C.Port)
}
