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
	"shiftworkbench/internal/system"
	"shiftworkbench/internal/wecompush"

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
	// v0.39.0 RBAC（权限管理模块）：
	//   1) Migrate  —— 建索引、把 departments.name 的单列唯一改成 (parent_id,name) 联合唯一、
	//                  补 users.dept_id 索引、回填 ancestors（全部幂等，不破坏既有数据）
	//   2) Seed     —— 播种完整权限树（每个模块/按钮一个 perms）+ 3 个内置角色 + users.role 回填
	//   3) SelfCheck—— 接口声明 ↔ 菜单 perms 双向差集自检（防"配了菜单忘了拦截"的越权）
	if err := system.Migrate(); err != nil {
		// 迁移失败**不阻塞启动**：这三件事（permissions 唯一索引 / departments 联合唯一 /
		// ancestors 回填）都属于增强项，失败时表结构仍可用、应用仍能服务；
		// 而 GuardByPath 默认 log 模式也不会因此误伤任何请求。
		// 反过来，这里若 Fatal 就等于"一个索引建不出来 → 整站起不来"，代价不成比例。
		logger.Info("server", "RBAC 迁移未完成（服务继续启动，权限按 fail-closed 处理）: %v", err)
	}
	if st, err := system.Seed(); err != nil {
		logger.Info("server", "RBAC 播种异常（服务继续启动，权限按 fail-closed 生效）: %v", err)
	} else {
		logger.Info("server", "RBAC 就绪：接口声明 %d 条，菜单新增 %d，角色新增 %d，授权 %d，用户角色回填 %d，拦截模式=%s",
			system.RoutePermCount(), st["menus_created"], st["roles_created"],
			st["role_menus"], st["user_roles_backfilled"], system.EnforceMode())
	}
	if probs := system.SelfCheck(); len(probs) > 0 {
		for _, p := range probs {
			logger.Info("server", "RBAC 自检发现问题: %s", p)
		}
	} else {
		logger.Info("server", "RBAC 自检通过：接口权限声明与菜单权限节点完全一致")
	}
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
		// v0.39.0 RBAC：**全部已登录接口**统一过权限闸门（权限在 routeperm.go 集中声明）。
		// 未在声明表里的接口按 fail-closed 处理 —— 于是"新增了接口却忘了声明"会立刻
		// 表现为 403 + 自检报警，而不是悄悄放行。
		// 默认模式 log（只记日志不拦截），因此初次上线不会误伤任何既有流程；
		// 观察审计日志确认无异常后，再由超管切到 on。
		auth.Use(system.GuardByPath())
		{
			auth.GET("/auth/me", handlers.Me)
			auth.POST("/auth/change-password", handlers.ChangePassword)
			auth.POST("/auth/unlock", handlers.UnlockLogin)
			auth.GET("/dashboard", handlers.Dashboard)
			auth.GET("/logs", handlers.ListLogs)
			auth.GET("/notifications", handlers.ListNotifications)
			auth.GET("/notifications/unread-count", handlers.UnreadNotificationCount)
			auth.POST("/notifications/:id/read", handlers.MarkNotificationRead)
			auth.POST("/notifications/read-all", handlers.MarkAllNotificationsRead)
			// 广播确认回执（接收人确认收到）v0.13.0
			auth.POST("/notifications/:id/ack", handlers.MarkNotificationAck)
			// 部门广播通知（部门管/超管）v0.2.0
			auth.POST("/notifications/broadcast", handlers.BroadcastNotification)
			// 广播附件下载（接收人本人/超管）v0.11.0
			auth.GET("/notifications/attachments/:key/download", handlers.DownloadNotifAttachment)
			// 广播送达/已读/确认统计（超管看全部；部门管理员看自己发的）v0.12.0/v0.13.0
			auth.GET("/notifications/broadcasts", handlers.ListBroadcastStats)
			auth.GET("/notifications/broadcasts/:bid/unread", handlers.BroadcastUnreadList)
			auth.GET("/notifications/broadcasts/:bid/unacked", handlers.BroadcastUnackedList)
			auth.POST("/notifications/broadcasts/:bid/nudge", handlers.NudgeBroadcast)
			// Web Push 订阅管理（登录用户本人）v0.13.0
			auth.POST("/push/subscribe", handlers.PushSubscribe)
			auth.POST("/push/unsubscribe", handlers.PushUnsubscribe)
			// 定时广播（部门管/超管）v0.13.0
			auth.GET("/scheduled-broadcasts", handlers.ListScheduledBroadcasts)
			auth.POST("/scheduled-broadcasts", handlers.CreateScheduledBroadcast)
			auth.DELETE("/scheduled-broadcasts/:id", handlers.DeleteScheduledBroadcast)

			// 部门（仅超管写）
			auth.GET("/departments", handlers.ListDepartments)
			auth.POST("/departments", handlers.CreateDepartment)
			auth.DELETE("/departments/:id", handlers.DeleteDepartment)

			// 部门班次定义（部门管/超管可改本部门）
			auth.GET("/shift-configs", handlers.ListShiftConfigs)
			auth.POST("/shift-configs", handlers.UpsertShiftConfig)
			auth.DELETE("/shift-configs/:id", handlers.DeleteShiftConfig)

			// 鉴权相关：登出、超管强制下线、在线会话
			auth.POST("/logout", middleware.AuthRequired(), middleware.Logout)
			auth.POST("/users/:id/force-logout", middleware.ForceLogout)
			auth.GET("/sessions", handlers.GetSessions)

			// 人员
			auth.GET("/users", handlers.ListUsers)
			auth.POST("/users", handlers.CreateUser)
			auth.PUT("/users/:id", handlers.UpdateUser)
			auth.POST("/users/:id/reset-password", handlers.ResetPassword)
			auth.DELETE("/users/:id", handlers.DeleteUser)
			auth.POST("/users/import", handlers.ImportUsers)
			auth.POST("/users/batch", handlers.BatchUsers)
			auth.GET("/users/export", handlers.ExportUsersXLSX) // v0.2.0 人员导出（v0.8.0 起 .xlsx）

			// 班表
			auth.GET("/schedules", handlers.ListSchedules)
			auth.POST("/schedules", handlers.CreateSchedule)
			auth.PUT("/schedules/:id", handlers.UpdateSchedule)
			auth.DELETE("/schedules/:id", handlers.DeleteSchedule)

			// 排班管理（v0.18.0）：规则 / 员工偏好 / 需求 / 特殊工作日 / 生成
			auth.GET("/shift-rules", handlers.GetShiftRule)
			auth.PUT("/shift-rules", handlers.UpdateShiftRule)

			auth.GET("/shift-prefs", handlers.ListUserShiftPrefs)
			auth.PUT("/shift-prefs", handlers.UpsertUserShiftPref)
			auth.DELETE("/shift-prefs/:userId", handlers.DeleteUserShiftPref)

			// 员工需求：提交/查看人人可用（限本人）；删除按状态区分；解锁仅管理员
			auth.GET("/shift-requests", handlers.ListShiftRequests)
			auth.POST("/shift-requests", handlers.CreateShiftRequest)
			auth.PUT("/shift-requests/:id", handlers.UpdateShiftRequest)
			auth.DELETE("/shift-requests/:id", handlers.DeleteShiftRequest)
			auth.POST("/shift-requests/:id/unlock", handlers.UnlockShiftRequest)

			auth.GET("/special-workdays", handlers.ListSpecialWorkDays)
			auth.POST("/special-workdays", handlers.UpsertSpecialWorkDay)
			auth.DELETE("/special-workdays/:id", handlers.DeleteSpecialWorkDay)

			auth.GET("/special-restdays", handlers.ListSpecialRestDays)
			auth.POST("/special-restdays", handlers.UpsertSpecialRestDay)
			auth.DELETE("/special-restdays/:id", handlers.DeleteSpecialRestDay)
			auth.GET("/holidays", handlers.ListHolidays) // 法定节假日（内置只读，非强制）

			auth.POST("/schedules/generate", handlers.GenerateSchedule)
			auth.POST("/schedules/validate", handlers.ValidatePlan)
			auth.POST("/schedules/apply", handlers.ApplyPlan)

			// 排班版本管理（v0.37.0）：改动后自动留档 + 多版本暂存/对比/回滚，定稿后再推送到正式班表
			auth.GET("/shift-drafts", handlers.ListPlanDrafts)
			auth.POST("/shift-drafts", handlers.SavePlanDraft)
			auth.GET("/shift-drafts/:id", handlers.GetPlanDraft)
			auth.PUT("/shift-drafts/:id", handlers.UpdatePlanDraft)
			auth.DELETE("/shift-drafts/:id", handlers.DeletePlanDraft)
			auth.POST("/shift-drafts/:id/apply", handlers.ApplyPlanDraft)

			// 任务
			auth.GET("/tasks", handlers.ListTasks)
			auth.GET("/tasks/counts", handlers.TaskCounts)
			auth.POST("/tasks", handlers.CreateTask)
			auth.PUT("/tasks/:id", handlers.UpdateTask)
			auth.POST("/tasks/:id/toggle", handlers.ToggleTask)
			auth.POST("/tasks/:id/freeze", handlers.FreezeTask)
			auth.GET("/tasks/:id/completions", handlers.ListTaskCompletions)
			auth.DELETE("/tasks/:id", handlers.DeleteTask)
			auth.POST("/tasks/batch-delete", handlers.BatchDeleteTasks)

			// 任务完成记录审计（部门管/超管可读本部门）
			auth.GET("/completions", handlers.ListCompletions)

			// 批量操作 / 导入导出（部门管/超管）
			auth.POST("/tasks/batch", handlers.BatchTasks)
			auth.POST("/tasks/import", handlers.ImportTasksCSV)
			auth.POST("/schedules/import", handlers.ImportSchedulesCSV)
			auth.GET("/schedules/export", handlers.ExportSchedulesXLSX)
			auth.GET("/tasks/export", handlers.ExportTasksXLSX)
			auth.GET("/logs/export", handlers.ExportLogsXLSX)

			// 系统运行日志（崩溃/异常排查）：列表仅管理员可读；导出限超管/部门管理员
			auth.GET("/system-logs", handlers.ListSystemLogs)
			auth.GET("/system-logs/export", handlers.ExportSystemLogsXLSX)

			// 工作台：迷你知识库 / 工作日志 / 交接接力（所有登录用户）
			auth.GET("/workspace/knowledge", handlers.ListKnowledge)
			auth.GET("/workspace/knowledge/categories", handlers.ListKnowledgeCategories)
			auth.GET("/workspace/knowledge/members", handlers.ListKnowledgeMembers)
			auth.GET("/workspace/knowledge/export", handlers.ExportKnowledge)
			auth.GET("/workspace/export/bundle", handlers.ExportWorkspaceBundle)
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

			// 知识库编辑草稿（v0.31.0）：自动保存双写（本地 IndexedDB + 服务端），崩溃 / 误关可恢复
			auth.PUT("/workspace/knowledge/draft", handlers.SaveKnowledgeDraft)
			auth.GET("/workspace/knowledge/draft", handlers.GetKnowledgeDraft)
			auth.DELETE("/workspace/knowledge/draft", handlers.DeleteKnowledgeDraft)
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
			auth.GET("/templates", handlers.ListTemplates)
			auth.GET("/templates/schedule-template", handlers.DownloadScheduleTemplateXLSX)
			auth.GET("/templates/task-template", handlers.DownloadTaskTemplateXLSX)
			auth.GET("/templates/user-template", handlers.DownloadUserTemplateXLSX)
			auth.GET("/templates/:id/download", handlers.DownloadTemplate)
			auth.POST("/templates", handlers.UpsertTemplate)
			auth.DELETE("/templates/:id", handlers.DeleteTemplate)

			// Webhook
			auth.GET("/webhooks", handlers.ListWebhooks)
			auth.POST("/webhooks", handlers.CreateWebhook)
			auth.PUT("/webhooks/:id", handlers.UpdateWebhook)
			auth.POST("/webhooks/notify", handlers.NotifyTodayHandler)
			auth.DELETE("/webhooks/:id", handlers.DeleteWebhook)
			auth.POST("/webhooks/test", handlers.TestWebhook)

			// 邮件通知配置（仅超管）
			auth.POST("/settings/smtp", handlers.UpdateSMTP)
			auth.POST("/settings/test-email", handlers.TestEmail)

			// 设置（仅超管写；完整配置读取也仅超管，避免 SMTP 等敏感项公开泄露）
			auth.GET("/settings/full", handlers.GetSettingFull)
			auth.POST("/settings", handlers.UpdateSetting)
			auth.POST("/settings/logo", handlers.UploadLogo)
			auth.DELETE("/settings/logo", handlers.DeleteLogo)
			auth.POST("/settings/log-retention", handlers.UpdateLogRetention)
			auth.POST("/settings/timezone", handlers.UpdateTimezone)
			auth.POST("/settings/daily-summary", handlers.UpdateDailySummary)
			auth.POST("/settings/overdue-grace", handlers.UpdateOverdueGrace)

			// 系统备份与还原（仅超管）
			auth.GET("/backups", handlers.ListBackupsHandler)
			auth.POST("/backups", handlers.CreateBackupHandler)
			auth.GET("/backups/:id/download", handlers.DownloadBackupHandler)
			auth.POST("/backups/:id/restore", handlers.RestoreBackupHandler)
			auth.DELETE("/backups/:id", handlers.DeleteBackupHandler)
			auth.POST("/backups/import", handlers.ImportBackupHandler)
			auth.GET("/backup-config", handlers.GetBackupConfigHandler)
			auth.POST("/backup-config", handlers.SaveBackupConfigHandler)
		}
	}

	// 企微推送模块（增量并入 GZT，独立表 wp_tasks/wp_logs/wp_settings，不影响既有数据/结构）
	wpHandler := wecompush.New(db.DB, wecompush.LoadConfig())
	wecompush.RegisterRoutes(api.Group("/wecom-push", middleware.AuthRequired(), system.GuardByPath()), wpHandler)
	go wecompush.NewScheduler(db.DB, wpHandler, time.Local).Start()

	// 系统管理（v0.39.0 RBAC）：用户 / 角色 / 菜单 / 部门 四个子模块。
	// 同样挂 GuardByPath —— 其接口权限全部在 routePerms 中显式声明，
	// 因此"新增了一个接口却忘了声明"会立刻表现为 403 并被 SelfCheck 报出来。
	system.RegisterRoutes(
		api.Group("/system", middleware.AuthRequired(), system.GuardByPath()),
		system.New(db.DB),
	)

	// RBAC 启动期覆盖自检（「每个接口都必须有明确约束」的兜底证据）。
	// 运行期 UndeclaredRoutes() 只能记录被调用过的接口，扫全量注册表才能证明没有漏网之鱼：
	// 未在 routePerms 声明、也不在 publicRoutes 的 /api 接口会被列出（fail-closed 下运行时 403）。
	if audit := system.AuditRegisteredRoutes(r.Routes()); audit.OK() {
		logger.Info("server", "RBAC 覆盖自检通过：/api 接口 %d 个（公开 %d / 已声明 %d），无漏声明；拦截模式=%s",
			audit.Total, audit.Public, audit.Declared, system.EnforceMode())
	} else {
		logger.Warn("server", "RBAC 覆盖自检发现 %d 个未声明接口（默认拒绝，运行时会 403）：%s",
			len(audit.Undeclared), strings.Join(audit.Undeclared, " | "))
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
