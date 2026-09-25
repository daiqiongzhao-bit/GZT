package system

// ============================================================================
// 接口权限声明表（方案 §5.5 措施 1 + 4）
//
// 设计取舍（为什么是"集中声明表"而不是每个路由手写 middleware.RequirePerm）：
//
//	GZT 有 180+ 条已登录接口。逐个改动 main.go 的风险是"漏一个"——而漏掉的那个
//	恰恰就是越权口子，且不会有任何提示。集中声明表把"覆盖"变成可校验的属性：
//	  · 未在表中声明的接口 = 默认拒绝（fail-closed），而不是默认放行
//	  · 表里的每个 perms 都必须能在 menus(F 节点) 里找到，由一致性自检强制
//
// 三种取值（都算"明确声明"，没有"未声明"的中间态）：
//	  PermAuth   "@auth"      显式声明为"登录即可"，不需要额外权限
//	  "xxx:yyy:zzz"           需要该权限标识（超管通配恒通过）
//	  未出现在表中            默认拒绝（符合方案的 fail-closed 要求）
//
// 键格式：优先 "METHOD /full/path"，未命中再退回 "/full/path"（任一方法）。
// 用 c.FullPath() 做键，因此 :id 之类通配按注册时的模式串书写。
// ============================================================================

// PermAuth 表示"登录即可，无额外权限要求"（显式声明，不是遗漏）。
const PermAuth = "@auth"

// 模块前缀常量（避免手写字符串出现拼写漂移）
const (
	// 基础权限：无独立页面、被多个模块复用的"登录即用"能力。
	// 不用 PermAuth 的原因：它需要能被超管按角色收回（例如收回某角色的通讯录查看）。
	PCommonRoster = "common:roster:view"

	PDashboardView = "dashboard:view"

	PScheduleView      = "schedule:view"
	PScheduleEdit      = "schedule:edit"
	PScheduleRemove    = "schedule:remove"
	PScheduleGenerate  = "schedule:generate"
	PScheduleValidate  = "schedule:validate"
	PScheduleApply     = "schedule:apply"
	PScheduleShiftConf = "schedule:shiftconfig"
	PScheduleRule      = "schedule:rule"
	PSchedulePref      = "schedule:pref"
	PScheduleSpecial   = "schedule:special"
	PScheduleReqUnlock = "schedule:request:unlock"
	PScheduleDraft     = "schedule:draft"
	PScheduleImport    = "schedule:import"
	PScheduleExport    = "schedule:export"

	PTaskView       = "task:view"
	PTaskAdd        = "task:add"
	PTaskEdit       = "task:edit"
	PTaskRemove     = "task:remove"
	PTaskFreeze     = "task:freeze"
	PTaskCompletion = "task:completion"
	// 全部完成记录（跨人查询，旧版为管理员专属）；与上面的"单人完成记录"分开声明，
	// 否则给执行者开"本人完成记录"会连带放开跨人查询。
	PTaskCompletionAll = "task:completion:all"
	PTaskImport        = "task:import"
	PTaskExport        = "task:export"

	PKnowledgeView       = "knowledge:view"
	PKnowledgeAdd        = "knowledge:add"
	PKnowledgeEdit       = "knowledge:edit"
	PKnowledgeRemove     = "knowledge:remove"
	PKnowledgeAttachment = "knowledge:attachment"
	PKnowledgeComment    = "knowledge:comment"
	PKnowledgeTrash      = "knowledge:trash"
	PKnowledgeVersion    = "knowledge:version"
	PKnowledgeTemplate   = "knowledge:template"
	PKnowledgeImport     = "knowledge:import"
	PKnowledgeExport     = "knowledge:export"

	PWorklogView     = "worklog:view"
	PWorklogAdd      = "worklog:add"
	PWorklogEdit     = "worklog:edit"
	PWorklogRemove   = "worklog:remove"
	PWorklogExport   = "worklog:export"
	PWorklogHandover = "worklog:handover"

	PHandoverView   = "handover:view"
	PHandoverAdd    = "handover:add"
	PHandoverStatus = "handover:status"
	PHandoverRemove = "handover:remove"
	PHandoverUrge   = "handover:urge"

	PWSAttachView     = "wsattach:view"
	PWSAttachUpload   = "wsattach:upload"
	PWSAttachDownload = "wsattach:download"
	PWSAttachRemove   = "wsattach:remove"

	PNotifyView       = "notify:view"
	PNotifyBroadcast  = "notify:broadcast"
	PNotifyBcastList  = "notify:broadcast:list"
	PNotifyBcastAdd   = "notify:broadcast:add"
	PNotifyBcastDel   = "notify:broadcast:remove"
	PNotifyBcastNudge = "notify:broadcast:nudge"

	PWpView         = "wecompush:view"
	PWpLogView      = "wecompush:log:view"
	PWpTaskAdd      = "wecompush:task:add"
	PWpTaskEdit     = "wecompush:task:edit"
	PWpTaskRemove   = "wecompush:task:remove"
	PWpTaskRun      = "wecompush:task:run"
	PWpTaskToggle   = "wecompush:task:toggle"
	PWpNotifyTest   = "wecompush:notify:test"
	PWpSetting      = "wecompush:setting:edit"
	PWpAuthManage   = "wecompush:auth:manage"
	PWpAccessConfig = "wecompush:access:config"

	PSysUserList     = "system:user:list"
	PSysUserAdd      = "system:user:add"
	PSysUserEdit     = "system:user:edit"
	PSysUserRemove   = "system:user:remove"
	PSysUserResetPwd = "system:user:resetPwd"
	PSysUserAuthRole = "system:user:authRole"
	PSysUserImport   = "system:user:import"
	PSysUserExport   = "system:user:export"
	PSysUserForceOut = "system:user:forceLogout"
	PSysUserSessions = "system:user:sessions"

	PSysRoleList     = "system:role:list"
	PSysRoleAdd      = "system:role:add"
	PSysRoleEdit     = "system:role:edit"
	PSysRoleRemove   = "system:role:remove"
	PSysRoleMenu     = "system:role:menu"
	PSysRoleDataScop = "system:role:dataScope"

	PSysMenuList   = "system:menu:list"
	PSysMenuAdd    = "system:menu:add"
	PSysMenuEdit   = "system:menu:edit"
	PSysMenuRemove = "system:menu:remove"

	PSysDeptList   = "system:dept:list"
	PSysDeptAdd    = "system:dept:add"
	PSysDeptEdit   = "system:dept:edit"
	PSysDeptRemove = "system:dept:remove"

	PSysSettingView  = "system:setting:view"
	PSysSettingEdit  = "system:setting:edit"
	PSysSettingSmtp  = "system:setting:smtp"
	PSysSettingLogo  = "system:setting:logo"
	PSysSettingReten = "system:setting:log-retention"
	PSysSettingTz    = "system:setting:timezone"
	PSysSettingDaily = "system:setting:daily-summary"
	PSysSettingGrace = "system:setting:overdue-grace"

	PSysTplList   = "system:template:list"
	PSysTplAdd    = "system:template:add"
	PSysTplRemove = "system:template:remove"

	PSysHookList   = "system:webhook:list"
	PSysHookAdd    = "system:webhook:add"
	PSysHookEdit   = "system:webhook:edit"
	PSysHookRemove = "system:webhook:remove"
	PSysHookTest   = "system:webhook:test"
	PSysHookNotify = "system:webhook:notify"

	PSysBakList     = "system:backup:list"
	PSysBakAdd      = "system:backup:add"
	PSysBakDownload = "system:backup:download"
	PSysBakRestore  = "system:backup:restore"
	PSysBakRemove   = "system:backup:remove"
	PSysBakImport   = "system:backup:import"
	PSysBakConfig   = "system:backup:config"

	PSysLogList   = "system:log:list"
	PSysLogExport = "system:log:export"
	PSysLogUnlock = "system:log:unlock"
)

// routePerms 是接口 → 权限标识的完整声明表。
var routePerms = map[string]string{
	// ---------------- 自身 / 会话 ----------------
	"GET /api/auth/me":               PermAuth,
	"POST /api/auth/change-password": PermAuth,
	"POST /api/logout":               PermAuth,
	"POST /api/push/subscribe":       PermAuth,
	"POST /api/push/unsubscribe":     PermAuth,
	"POST /api/auth/unlock":          PSysLogUnlock,

	// ---------------- 概览 / 审计 ----------------
	"GET /api/dashboard":   PDashboardView,
	"GET /api/logs":        PSysLogList,
	"GET /api/logs/export": PSysLogExport,
	"POST /api/auth/logout": PermAuth, // v0.40.8：登出本人即可（令牌即将失效前调用）
	"GET /api/users/:id/auth-logs": PSysUserList, // v0.40.8：员工登录/登出时间线
	"GET /api/auth-logs":        PSysUserList, // v0.41.1：全局登录审计（跨用户）
	"GET /api/auth-logs/export": PSysUserList, // v0.41.1：登录审计导出（xlsx/csv）

	// ---------------- 通知 ----------------
	"GET /api/notifications":                           PNotifyView,
	"GET /api/notifications/unread-count":              PNotifyView,
	"POST /api/notifications/:id/read":                 PermAuth,
	"POST /api/notifications/read-all":                 PermAuth,
	"POST /api/notifications/:id/ack":                  PermAuth,
	"GET /api/notifications/attachments/:key/download": PNotifyView,
	"POST /api/notifications/broadcast":                PNotifyBroadcast,
	"GET /api/notifications/broadcasts":                PNotifyBcastList,
	"GET /api/notifications/broadcasts/:bid/unread":    PNotifyBcastList,
	"GET /api/notifications/broadcasts/:bid/unacked":   PNotifyBcastList,
	"POST /api/notifications/broadcasts/:bid/nudge":    PNotifyBcastNudge,
	"GET /api/scheduled-broadcasts":                    PNotifyBcastList,
	"POST /api/scheduled-broadcasts":                   PNotifyBcastAdd,
	"DELETE /api/scheduled-broadcasts/:id":             PNotifyBcastDel,

	// ---------------- 部门（读为登录级：多处以部门选择器形式复用；写仅超管） ----------------
	"GET /api/departments":        PermAuth,
	"POST /api/departments":       PSysDeptAdd,
	"DELETE /api/departments/:id": PSysDeptRemove,

	// ---------------- 班次配置 ----------------
	"GET /api/shift-configs":        PermAuth,
	"POST /api/shift-configs":       PScheduleShiftConf,
	"DELETE /api/shift-configs/:id": PScheduleShiftConf,

	// ---------------- 用户 ----------------
	"GET /api/users":                     PCommonRoster, // 通讯录/人员选择器：班表/任务/知识库/排班计划共用
	"POST /api/users":                    PSysUserAdd,
	"PUT /api/users/:id":                 PSysUserEdit,
	"POST /api/users/:id/reset-password": PSysUserResetPwd,
	"DELETE /api/users/:id":              PSysUserRemove,
	"POST /api/users/import":             PSysUserImport,
	"POST /api/users/batch":              PSysUserEdit,
	"GET /api/users/export":              PSysUserExport,
	"POST /api/users/:id/force-logout":   PSysUserForceOut,
	"GET /api/sessions":                  PSysUserSessions,

	// ---------------- 班表 ----------------
	"GET /api/schedules":                  PScheduleView,
	"POST /api/schedules":                 PScheduleEdit,
	"PUT /api/schedules/:id":              PScheduleEdit,
	"DELETE /api/schedules/:id":           PScheduleRemove,
	"GET /api/shift-rules":                PScheduleView,
	"PUT /api/shift-rules":                PScheduleRule,
	"GET /api/shift-prefs":                PScheduleView,
	"PUT /api/shift-prefs":                PSchedulePref,
	"DELETE /api/shift-prefs/:userId":     PSchedulePref,
	"GET /api/shift-requests":             PScheduleView,
	"POST /api/shift-requests":            PermAuth, // 员工自己提交换班/休假申请
	"PUT /api/shift-requests/:id":         PermAuth,
	"DELETE /api/shift-requests/:id":      PermAuth,
	"POST /api/shift-requests/:id/unlock": PScheduleReqUnlock,
	"GET /api/special-workdays":           PScheduleView,
	"POST /api/special-workdays":          PScheduleSpecial,
	"DELETE /api/special-workdays/:id":    PScheduleSpecial,
	"GET /api/special-restdays":           PScheduleView,
	"POST /api/special-restdays":          PScheduleSpecial,
	"DELETE /api/special-restdays/:id":    PScheduleSpecial,
	"GET /api/holidays":                   PScheduleView,
	"POST /api/schedules/generate":        PScheduleGenerate,
	"POST /api/schedules/validate":        PScheduleValidate,
	"POST /api/schedules/apply":           PScheduleApply,
	"POST /api/schedules/import":          PScheduleImport,
	"GET /api/schedules/export":           PScheduleExport,
	"GET /api/shift-drafts":               PScheduleDraft,
	"POST /api/shift-drafts":              PScheduleDraft,
	"GET /api/shift-drafts/:id":           PScheduleDraft,
	"PUT /api/shift-drafts/:id":           PScheduleDraft,
	"DELETE /api/shift-drafts/:id":        PScheduleDraft,
	"POST /api/shift-drafts/:id/apply":    PScheduleDraft,

	// ---------------- 任务 ----------------
	"GET /api/tasks":                 PTaskView,
	"GET /api/tasks/counts":          PTaskView,
	"POST /api/tasks":                PTaskAdd,
	"PUT /api/tasks/:id":             PTaskEdit,
	"POST /api/tasks/:id/toggle":     PermAuth, // 员工打卡完成自己的任务
	"POST /api/tasks/:id/freeze":     PTaskFreeze,
	"GET /api/tasks/:id/completions": PTaskCompletion,
	"DELETE /api/tasks/:id":          PTaskRemove,
	"POST /api/tasks/batch-delete":   PTaskRemove,
	"GET /api/completions":           PTaskCompletionAll,
	"POST /api/tasks/batch":          PTaskEdit,
	"POST /api/tasks/import":         PTaskImport,
	"GET /api/tasks/export":          PTaskExport,

	// ---------------- 系统日志 ----------------
	"GET /api/system-logs":        PSysLogList,
	"GET /api/system-logs/export": PSysLogExport,

	// ---------------- 知识库 ----------------
	"GET /api/workspace/knowledge":                           PKnowledgeView,
	"GET /api/workspace/knowledge/categories":                PKnowledgeView,
	"GET /api/workspace/knowledge/members":                   PKnowledgeView,
	"GET /api/workspace/knowledge/tags":                      PKnowledgeView,
	"GET /api/workspace/knowledge/stats":                     PKnowledgeView,
	"GET /api/workspace/knowledge/:id":                       PKnowledgeView,
	"GET /api/workspace/knowledge/:id/attachments":           PKnowledgeView,
	"GET /api/workspace/knowledge/:id/history":               PKnowledgeView,
	"GET /api/workspace/knowledge/:id/versions":              PKnowledgeView,
	"GET /api/workspace/knowledge/:id/backlinks":             PKnowledgeView,
	"GET /api/workspace/knowledge/:id/outlinks":              PKnowledgeView,
	"GET /api/workspace/knowledge/:id/comments":              PKnowledgeView,
	"POST /api/workspace/knowledge":                          PKnowledgeAdd,
	"PUT /api/workspace/knowledge/:id":                       PKnowledgeEdit,
	"DELETE /api/workspace/knowledge/:id":                    PKnowledgeRemove,
	"POST /api/workspace/knowledge/:id/pin":                  PKnowledgeEdit,
	"POST /api/workspace/knowledge/:id/star":                 PermAuth, // 收藏是个人行为
	"POST /api/workspace/knowledge/:id/comments":             PKnowledgeComment,
	"DELETE /api/workspace/knowledge/comments/:cid":          PKnowledgeComment,
	"POST /api/workspace/knowledge/:id/attachments":          PKnowledgeAttachment,
	"POST /api/workspace/temp-attachments":                   PKnowledgeAttachment,
	"DELETE /api/workspace/knowledge_attachments/:aid":       PKnowledgeAttachment,
	"GET /api/workspace/knowledge/trash":                     PKnowledgeTrash,
	"POST /api/workspace/knowledge/trash/empty":              PKnowledgeTrash,
	"POST /api/workspace/knowledge/:id/restore":              PKnowledgeTrash,
	"DELETE /api/workspace/knowledge/:id/purge":              PKnowledgeTrash,
	"POST /api/workspace/knowledge/:id/version/:vid/restore": PKnowledgeVersion,
	"GET /api/workspace/knowledge/templates":                 PKnowledgeView,
	"GET /api/workspace/knowledge/templates/:id":             PKnowledgeView,
	"POST /api/workspace/knowledge/templates":                PKnowledgeTemplate,
	"DELETE /api/workspace/knowledge/templates/:id":          PKnowledgeTemplate,
	"POST /api/workspace/knowledge/import":                   PKnowledgeImport,
	"GET /api/workspace/knowledge/export":                    PKnowledgeExport,
	"GET /api/workspace/knowledge/export/markdown":           PKnowledgeExport,
	"GET /api/workspace/knowledge/export/doc":                PKnowledgeExport,
	"GET /api/workspace/export/bundle":                       PKnowledgeExport,
	"PUT /api/workspace/knowledge/draft":                     PermAuth, // 个人编辑草稿自动保存
	"GET /api/workspace/knowledge/draft":                     PermAuth,
	"DELETE /api/workspace/knowledge/draft":                  PermAuth,

	// ---------------- 工作日志 ----------------
	"GET /api/workspace/logs":               PWorklogView,
	"GET /api/workspace/logs/stats":         PWorklogView,
	"POST /api/workspace/logs":              PWorklogAdd,
	"PUT /api/workspace/logs/:id":           PWorklogEdit,
	"DELETE /api/workspace/logs/:id":        PWorklogRemove,
	"GET /api/workspace/logs/export":        PWorklogExport,
	"POST /api/workspace/logs/:id/handover": PWorklogHandover,

	// ---------------- 交接接力 ----------------
	"GET /api/workspace/handovers":             PHandoverView,
	"GET /api/workspace/handovers/stats":       PHandoverView,
	"GET /api/workspace/handovers/:id/events":  PHandoverView,
	"POST /api/workspace/handovers":            PHandoverAdd,
	"POST /api/workspace/handovers/:id/status": PHandoverStatus,
	"POST /api/workspace/handovers/:id/urge":   PHandoverUrge,
	"DELETE /api/workspace/handovers/:id":      PHandoverRemove,

	// ---------------- 工作台附件 ----------------
	"GET /api/workspace/attachments":               PWSAttachView,
	"POST /api/workspace/attachments":              PWSAttachUpload,
	"GET /api/workspace/attachments/:key/download": PWSAttachDownload,
	"DELETE /api/workspace/attachments/:id":        PWSAttachRemove,

	// ---------------- 模板 ----------------
	"GET /api/templates":                   PSysTplList,
	"GET /api/templates/schedule-template": PSysTplList,
	"GET /api/templates/task-template":     PSysTplList,
	"GET /api/templates/user-template":     PSysTplList,
	"GET /api/templates/:id/download":      PSysTplList,
	"POST /api/templates":                  PSysTplAdd,
	"DELETE /api/templates/:id":            PSysTplRemove,

	// ---------------- Webhook ----------------
	"GET /api/webhooks":         PSysHookList,
	"POST /api/webhooks":        PSysHookAdd,
	"PUT /api/webhooks/:id":     PSysHookEdit,
	"DELETE /api/webhooks/:id":  PSysHookRemove,
	"POST /api/webhooks/test":   PSysHookTest,
	"POST /api/webhooks/notify": PSysHookNotify,

	// ---------------- 系统设置 ----------------
	"GET /api/settings/full":           PSysSettingView,
	"POST /api/settings":               PSysSettingEdit,
	"POST /api/settings/smtp":          PSysSettingSmtp,
	"POST /api/settings/test-email":    PSysSettingSmtp,
	"POST /api/settings/logo":          PSysSettingLogo,
	"DELETE /api/settings/logo":        PSysSettingLogo,
	"POST /api/settings/log-retention": PSysSettingReten,
	"POST /api/settings/timezone":      PSysSettingTz,
	"POST /api/settings/daily-summary": PSysSettingDaily,
	"POST /api/settings/overdue-grace": PSysSettingGrace,

	// ---------------- 备份还原 ----------------
	"GET /api/backups":              PSysBakList,
	"POST /api/backups":             PSysBakAdd,
	"GET /api/backups/:id/download": PSysBakDownload,
	"POST /api/backups/:id/restore": PSysBakRestore,
	"POST /api/backups/:id/verify":  PSysBakList,
	"DELETE /api/backups/:id":       PSysBakRemove,
	"POST /api/backups/import":      PSysBakImport,
	"GET /api/backup-config":        PSysBakConfig,
	"POST /api/backup-config":       PSysBakConfig,

	// ---------------- 企微推送 ----------------
	"GET /api/wecom-push/access":            PermAuth,
	"PUT /api/wecom-push/access":            PWpAccessConfig,
	"GET /api/wecom-push/summary":           PWpView,
	"GET /api/wecom-push/cli-status":        PWpView,
	"GET /api/wecom-push/groups":            PWpView,
	"GET /api/wecom-push/fields":            PWpView,
	"GET /api/wecom-push/settings/full":     PWpView,
	"GET /api/wecom-push/logs":              PWpLogView,
	"GET /api/wecom-push/logs/export":       PWpLogView, // v0.41.1：推送运行日志导出
	"PUT /api/wecom-push/settings":          PWpSetting,
	"POST /api/wecom-push/notify/test":      PWpNotifyTest,
	"POST /api/wecom-push/auth/start":       PWpAuthManage,
	"GET /api/wecom-push/auth/status":       PWpAuthManage,
	"GET /api/wecom-push/auth/qr":           PWpAuthManage,
	"POST /api/wecom-push/auth/cancel":      PWpAuthManage,
	"POST /api/wecom-push/auth/manual":      PWpAuthManage,
	"GET /api/wecom-push/tasks":             PWpView,
	"POST /api/wecom-push/tasks":            PWpTaskAdd,
	"PUT /api/wecom-push/tasks/:id":         PWpTaskEdit,
	"DELETE /api/wecom-push/tasks/:id":      PWpTaskRemove,
	"POST /api/wecom-push/tasks/:id/toggle": PWpTaskToggle,
	"POST /api/wecom-push/tasks/:id/run":    PWpTaskRun,
	"GET /api/wecom-push/tasks/:id/sql":     PWpView,
	"GET /api/wecom-push/files/:name":       PWpView, // v0.40.8：运行日志附件下载

	// ---------------- 系统管理（RBAC 自身） ----------------
	"GET /api/system/user/list":          PSysUserList,
	"GET /api/system/user/detail/:id":    PSysUserList,
	"POST /api/system/user":              PSysUserAdd,
	"PUT /api/system/user":               PSysUserEdit,
	"POST /api/system/user/delete/:id":   PSysUserRemove,
	"POST /api/system/user/resetPwd":     PSysUserResetPwd,
	"POST /api/system/user/changeStatus": PSysUserEdit,
	"POST /api/system/user/authRole":     PSysUserAuthRole,
	"GET /api/system/role/list":          PSysRoleList,
	// 角色选择器：响应只含"调用者有权分配的角色"，故登录即可，无需 system:role:list
	"GET /api/system/role/options":          PermAuth,
	"GET /api/system/role/detail/:id":       PSysRoleList,
	"POST /api/system/role":                 PSysRoleAdd,
	"PUT /api/system/role":                  PSysRoleEdit,
	"POST /api/system/role/delete/:id":      PSysRoleRemove,
	"GET /api/system/role/menuTree/:roleId": PSysRoleList,
	"PUT /api/system/role/menu":             PSysRoleMenu,
	"GET /api/system/role/deptTree/:roleId": PSysRoleList,
	"PUT /api/system/role/dataScope":        PSysRoleDataScop,
	"POST /api/system/role/changeStatus":    PSysRoleEdit,
	"GET /api/system/menu/list":             PSysMenuList,
	"GET /api/system/menu/detail/:id":       PSysMenuList,
	"POST /api/system/menu":                 PSysMenuAdd,
	"PUT /api/system/menu":                  PSysMenuEdit,
	"POST /api/system/menu/delete/:id":      PSysMenuRemove,
	"GET /api/system/dept/tree":             PSysDeptList,
	"GET /api/system/dept/treeSelect":       PermAuth, // 表单部门选择器：登录即可（按数据范围过滤）
	"POST /api/system/dept":                 PSysDeptAdd,
	"PUT /api/system/dept":                  PSysDeptEdit,
	"POST /api/system/dept/delete/:id":      PSysDeptRemove,
	"GET /api/system/perm/my":               PermAuth,
	"GET /api/system/perm/routes":           PermAuth,
	"GET /api/system/perm/consistency":      PSysRoleList,
	"POST /api/system/perm/enforce":         PermAuth, // 真正的门是 handler 内的硬性超管断言
}

// LookupPerm 查接口所需权限。返回 (perm, declared)。
// declared=false 表示"未声明"——调用方必须按 fail-closed 处理（拒绝或按模式记日志）。
func LookupPerm(method, fullPath string) (string, bool) {
	if v, ok := routePerms[method+" "+fullPath]; ok {
		return v, true
	}
	if v, ok := routePerms[fullPath]; ok {
		return v, true
	}
	return "", false
}

// DeclaredPerms 返回表里所有"真实权限标识"（排除 @auth），供一致性自检使用。
func DeclaredPerms() []string {
	set := map[string]struct{}{}
	for _, v := range routePerms {
		if v == "" || v == PermAuth {
			continue
		}
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// RoutePermCount 返回已声明接口数（启动日志用，便于确认覆盖范围）。
func RoutePermCount() int { return len(routePerms) }
