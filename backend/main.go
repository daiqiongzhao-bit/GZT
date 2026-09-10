package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/handlers"
	"shiftworkbench/internal/logger"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/service"

	"github.com/gin-gonic/gin"
)

//go:embed all:web/dist/**
var webFS embed.FS

func main() {
	if err := config.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败(安全校验):", err)
		os.Exit(1)
	}

	// 运行日志：默认写在数据库同目录的 runtime.log（即使数据库损坏也可排查）；
	// 可用环境变量 LOG_FILE 覆盖。该文件随数据卷持久化（NAS 上为 /volume1/docker/gzt/data）。
	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = filepath.Join(filepath.Dir(config.C.DBPath), "runtime.log")
	}
	logger.Init(logFile)

	if err := db.Init(); err != nil {
		logger.Fatal("server", "数据库初始化失败: %v", err)
	}
	// 知识库升级初始化：FTS5 全文索引、系统模板预置、引用关系重建（仅知识库模块）
	handlers.InitKnowledge()
	logger.Info("server", "知识库升级模块初始化完成（FTS5 / 模板 / 双向链接）")
	// 将系统日志同时落业务库（供界面「运行日志」查看）；DB 异常不影响主流程。
	logger.RegisterDBSink(func(lvl logger.Level, source, message, detail string) {
		if db.DB == nil {
			return
		}
		_ = db.DB.Create(&models.SystemLog{Level: string(lvl), Source: source, Message: message, Detail: detail}).Error
	})
	service.Seed()
	// 手册随版本种入知识库（v0.16.0）：每版本仅首次启动种入一次，
	// 管理员删除后同版本内不再重建，升级到新版本时会重新补齐。
	if pdf, err := fs.ReadFile(webFS, "web/dist/manual.pdf"); err == nil {
		if r := handlers.SeedManualToKnowledge(pdf); r.Seeded {
			logger.Info("server", "操作手册已种入知识库：%s（条目 #%d，%d 字节）", r.Title, r.EntryID, r.Size)
		} else {
			logger.Info("server", "操作手册未种入知识库：%s", r.Reason)
		}
	} else {
		logger.Info("server", "未找到内嵌操作手册（web/dist/manual.pdf），跳过种入")
	}
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
	// v0.13.0：历史广播统计回填（幂等）+ 定时广播调度器 + VAPID 密钥准备（Web Push）
	handlers.BackfillBroadcastIDs()
	_ = handlers.EnsurePushKeys()
	go handlers.StartScheduledBroadcastScheduler()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.Recovery(), gin.Logger(), middleware.ErrorLogger(), middleware.CORS())

	// 健康检查（供 Docker healthcheck / 群晖 Container Manager 探测，无需登录）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "version": config.C.AppVersion})
	})

	api := r.Group("/api")
	{
		// 公开
		api.POST("/auth/login", handlers.Login)
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok", "version": config.C.AppVersion})
		})
		api.GET("/settings", handlers.GetSetting)   // 企业信息公开可读（登录页展示）
		api.GET("/settings/logo", handlers.GetLogo) // 企业 Logo 公开可读（登录页展示）
		// 知识附件（含中转缓存）公开可读：路由用不可猜测的 stored_name（纳秒随机串），
		// 匿名者无法按自增主键枚举他人附件；富文本内嵌图片仍可经 <img src> 直接渲染。
		api.GET("/workspace/knowledge_attachments/:key/download", handlers.DownloadKnowledgeAttachment)
		api.GET("/workspace/temp-attachments/:key/download", handlers.DownloadTempAttachment)
		api.GET("/version", func(c *gin.Context) {
			c.JSON(200, gin.H{"version": config.C.AppVersion})
		})
		// Web Push VAPID 公钥（订阅前获取，公开即可；私钥在服务端）
		api.GET("/push/vapid-public", handlers.GetVapidPublicKey)

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
			// 广播确认回执（接收人确认收到）v0.13.0
			auth.POST("/notifications/:id/ack", handlers.MarkNotificationAck)
			// 部门广播通知（部门管/超管）v0.2.0
			auth.POST("/notifications/broadcast", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.BroadcastNotification)
			// 广播附件下载（接收人本人/超管）v0.11.0
			auth.GET("/notifications/attachments/:key/download", handlers.DownloadNotifAttachment)
			// 广播送达/已读/确认统计（超管看全部；部门管理员看自己发的）v0.12.0/v0.13.0
			auth.GET("/notifications/broadcasts", handlers.ListBroadcastStats)
			auth.GET("/notifications/broadcasts/:bid/unread", handlers.BroadcastUnreadList)
			auth.GET("/notifications/broadcasts/:bid/unacked", handlers.BroadcastUnackedList)
			auth.POST("/notifications/broadcasts/:bid/nudge", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.NudgeBroadcast)
			// Web Push 订阅管理（登录用户本人）v0.13.0
			auth.POST("/push/subscribe", handlers.PushSubscribe)
			auth.POST("/push/unsubscribe", handlers.PushUnsubscribe)
			// 定时广播（部门管/超管）v0.13.0
			auth.GET("/scheduled-broadcasts", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ListScheduledBroadcasts)
			auth.POST("/scheduled-broadcasts", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.CreateScheduledBroadcast)
			auth.DELETE("/scheduled-broadcasts/:id", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.DeleteScheduledBroadcast)

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
			auth.POST("/users/batch", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.BatchUsers)
			auth.GET("/users/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportUsersXLSX) // v0.2.0 人员导出（v0.8.0 起 .xlsx）

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
			auth.GET("/schedules/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportSchedulesXLSX)
			auth.GET("/tasks/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportTasksXLSX)
			auth.GET("/logs/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportLogsXLSX)

			// 系统运行日志（崩溃/异常排查）：列表仅管理员可读；导出限超管/部门管理员
			auth.GET("/system-logs", handlers.ListSystemLogs)
			auth.GET("/system-logs/export", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ExportSystemLogsXLSX)

			// 工作台：迷你知识库 / 工作日志 / 交接接力（所有登录用户）
			auth.GET("/workspace/knowledge", handlers.ListKnowledge)
			auth.GET("/workspace/knowledge/categories", handlers.ListKnowledgeCategories)
			auth.GET("/workspace/knowledge/export", handlers.ExportKnowledge)
			auth.GET("/workspace/export/bundle", middleware.RequireRole(models.RoleSuperAdmin), handlers.ExportWorkspaceBundle)
			auth.GET("/workspace/knowledge/:id/attachments", handlers.ListKnowledgeAttachments)
			auth.GET("/workspace/knowledge/:id/history", handlers.ListKnowledgeHistory)
			auth.POST("/workspace/knowledge/:id/attachments", handlers.UploadKnowledgeAttachment)
			auth.POST("/workspace/temp-attachments", handlers.UploadTempAttachment)
			auth.DELETE("/workspace/knowledge_attachments/:aid", handlers.DeleteKnowledgeAttachment)
			auth.POST("/workspace/knowledge", handlers.CreateKnowledge)
			auth.PUT("/workspace/knowledge/:id", handlers.UpdateKnowledge)
			auth.DELETE("/workspace/knowledge/:id", handlers.DeleteKnowledge)

			// —— 知识库升级（v0.15.0）：检索/标签/回收站/统计/评论/版本/模板/导入导出/双向链接 ——
			auth.GET("/workspace/knowledge/:id", handlers.GetKnowledge)
			auth.GET("/workspace/knowledge/tags", handlers.ListKnowledgeTags)
			auth.GET("/workspace/knowledge/stats", handlers.KnowledgeStats)
			auth.GET("/workspace/knowledge/trash", handlers.ListKnowledgeTrash)
			auth.POST("/workspace/knowledge/trash/empty", handlers.EmptyKnowledgeTrash)
			auth.POST("/workspace/knowledge/:id/restore", handlers.RestoreKnowledge)
			auth.DELETE("/workspace/knowledge/:id/purge", handlers.PurgeKnowledge)
			auth.POST("/workspace/knowledge/:id/pin", handlers.ToggleKnowledgePin)
			auth.POST("/workspace/knowledge/:id/star", handlers.ToggleKnowledgeStar)
			auth.GET("/workspace/knowledge/:id/comments", handlers.ListKnowledgeComments)
			auth.POST("/workspace/knowledge/:id/comments", handlers.CreateKnowledgeComment)
			auth.DELETE("/workspace/knowledge/comments/:cid", handlers.DeleteKnowledgeComment)
			auth.GET("/workspace/knowledge/:id/versions", handlers.ListKnowledgeVersions)
			auth.POST("/workspace/knowledge/:id/version/:vid/restore", handlers.RestoreKnowledgeVersion)
			auth.GET("/workspace/knowledge/:id/backlinks", handlers.ListKnowledgeBacklinks)
			auth.GET("/workspace/knowledge/:id/outlinks", handlers.ListKnowledgeOutlinks)
			auth.GET("/workspace/knowledge/templates", handlers.ListKnowledgeTemplates)
			auth.POST("/workspace/knowledge/templates", handlers.CreateKnowledgeTemplate)
			auth.GET("/workspace/knowledge/templates/:id", handlers.ApplyKnowledgeTemplate)
			auth.DELETE("/workspace/knowledge/templates/:id", handlers.DeleteKnowledgeTemplate)
			auth.POST("/workspace/knowledge/import", handlers.ImportKnowledge)
			auth.GET("/workspace/knowledge/export/markdown", handlers.ExportKnowledgeMarkdown)
			auth.GET("/workspace/knowledge/export/doc", handlers.ExportKnowledgeDoc)

			auth.GET("/workspace/logs", handlers.ListWorkLogs)
			auth.POST("/workspace/logs", handlers.CreateWorkLog)
			auth.PUT("/workspace/logs/:id", handlers.UpdateWorkLog)
			auth.DELETE("/workspace/logs/:id", handlers.DeleteWorkLog)
			// v0.16.0 工作日志补齐：统计 / 导出
			auth.GET("/workspace/logs/stats", handlers.WorkLogStats)
			auth.GET("/workspace/logs/export", handlers.ExportWorkLogs)
			// v0.16.0 日志转交接单（把「还没做完的」一键转给他人）
			auth.POST("/workspace/logs/:id/handover", handlers.LogToHandover)

			auth.GET("/workspace/handovers", handlers.ListHandovers)
			auth.POST("/workspace/handovers", handlers.CreateHandover)
			auth.POST("/workspace/handovers/:id/status", handlers.UpdateHandoverStatus)
			auth.DELETE("/workspace/handovers/:id", handlers.DeleteHandover)
			// v0.16.0 交接接力补齐：统计 / 处理时间线 / 退回 / 催办
			auth.GET("/workspace/handovers/stats", handlers.HandoverStats)
			auth.GET("/workspace/handovers/:id/events", handlers.ListHandoverEvents)
			auth.POST("/workspace/handovers/:id/urge", handlers.UrgeHandover)

			// v0.16.0 工作台统一附件（日志 / 交接单共用，复用知识库附件存储口径）
			auth.GET("/workspace/attachments", handlers.ListWSAttachments)
			auth.POST("/workspace/attachments", handlers.UploadWSAttachment)
			auth.GET("/workspace/attachments/:key/download", handlers.DownloadWSAttachment)
			auth.DELETE("/workspace/attachments/:id", handlers.DeleteWSAttachment)

			// 模板管理（管理员可查看下载，超管可修改）
			auth.GET("/templates", middleware.RequireRole(models.RoleSuperAdmin, models.RoleDeptAdmin), handlers.ListTemplates)
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

			// 设置（仅超管写；完整配置读取也仅超管，避免 SMTP 等敏感项公开泄露）
			auth.GET("/settings/full", middleware.RequireRole(models.RoleSuperAdmin), handlers.GetSettingFull)
			auth.POST("/settings", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateSetting)
			auth.POST("/settings/logo", middleware.RequireRole(models.RoleSuperAdmin), handlers.UploadLogo)
			auth.DELETE("/settings/logo", middleware.RequireRole(models.RoleSuperAdmin), handlers.DeleteLogo)
			auth.POST("/settings/log-retention", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateLogRetention)
			auth.POST("/settings/timezone", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateTimezone)
			auth.POST("/settings/daily-summary", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateDailySummary)
			auth.POST("/settings/overdue-grace", middleware.RequireRole(models.RoleSuperAdmin), handlers.UpdateOverdueGrace)

			// 系统备份与还原（仅超管）
			auth.GET("/backups", middleware.RequireRole(models.RoleSuperAdmin), handlers.ListBackupsHandler)
			auth.POST("/backups", middleware.RequireRole(models.RoleSuperAdmin), handlers.CreateBackupHandler)
			auth.GET("/backups/:id/download", middleware.RequireRole(models.RoleSuperAdmin), handlers.DownloadBackupHandler)
			auth.POST("/backups/:id/restore", middleware.RequireRole(models.RoleSuperAdmin), handlers.RestoreBackupHandler)
			auth.DELETE("/backups/:id", middleware.RequireRole(models.RoleSuperAdmin), handlers.DeleteBackupHandler)
			auth.POST("/backups/import", middleware.RequireRole(models.RoleSuperAdmin), handlers.ImportBackupHandler)
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
					setCacheHeaders(c, rel)
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
			setCacheHeaders(c, "index.html")
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusOK)
			io.Copy(c.Writer, idx)
		})
	}

	logger.Info("server", "服务启动完成 version=%s 监听端口=:%s 运行日志=%s",
		config.C.AppVersion, config.C.Port, logFile)

	// 优雅退出：捕获终止信号并记录日志，便于排查「为何服务被停」
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		logger.Warn("server", "收到退出信号，服务即将停止")
		os.Exit(0)
	}()

	r.Run(":" + config.C.Port)
}

// setCacheHeaders 为前端静态资源设置缓存策略。
//
// 背景：SPA + PWA 场景下，若 index.html / sw.js 被浏览器或 Service Worker 缓存，
// 发新版后用户仍加载旧壳，会出现「点了菜单没反应」这类难以定位的问题。
//
// 策略：
//   - index.html、sw.js：no-cache（每次校验，保证发版后能拿到新壳与新 SW）
//   - 带内容 hash 的 assets/*：可长期强缓存（hash 变化即为新文件）
//   - 其他（favicon、manifest 等）：短缓存
func setCacheHeaders(c *gin.Context, rel string) {
	switch {
	case rel == "index.html" || rel == "sw.js" || rel == "":
		c.Header("Cache-Control", "no-cache, must-revalidate")
		c.Header("Pragma", "no-cache")
	case strings.HasPrefix(rel, "assets/"):
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	default:
		c.Header("Cache-Control", "public, max-age=3600")
	}
}
