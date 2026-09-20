package system

import (
	"fmt"
	"sort"
	"strings"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"gorm.io/gorm"
)

// ============================================================================
// RBAC 迁移与种子（方案 §12.1 P1/P2）
//
// 原则（方案 §1.3「不破坏现状」）：
//   - 只新增表 / 新增列，不改不删任何现有列
//   - users.role 保留双读：种子只把它"再表达"为 user_roles，不改它的值
//   - 全部幂等：可重复执行，重启不会产生重复数据
// ============================================================================

// Migrate 建索引、改造部门唯一索引、回填 ancestors（在 AutoMigrate 之后调用）。
func Migrate() error {
	if db.DB == nil {
		return fmt.Errorf("数据库未初始化")
	}
	stmts := []string{
		// menus.perms 全局唯一（部分索引：允许 NULL / 空串，SQLite 唯一索引对 NULL 本就不冲突）
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_menus_perms ON menus(perms) WHERE perms IS NOT NULL AND perms <> ''`,
		// ★ departments 唯一索引改造（方案 §12.2(2)）：原 idx_departments_name 是 name 单列全局唯一，
		// 树形化后"同级重名"会直接插入失败，必须显式 DROP 再建联合唯一。
		`DROP INDEX IF EXISTS idx_departments_name`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_departments_parent_name ON departments(parent_id, name)`,
		// ★ users.dept_id 索引（方案 §12.2(3)）：数据范围过滤高频命中，实测原本缺失。
		`CREATE INDEX IF NOT EXISTS idx_users_dept_id ON users(dept_id)`,
	}
	for _, s := range stmts {
		if err := db.DB.Exec(s).Error; err != nil {
			// 索引改造失败不应阻塞启动（表结构仍可用），但要显式暴露原因
			return fmt.Errorf("执行 [%s] 失败: %w", s, err)
		}
	}
	if err := backfillAncestors(); err != nil {
		return fmt.Errorf("回填 ancestors 失败: %w", err)
	}
	return nil
}

// backfillAncestors 用 Go 侧建树回填 ancestors（不依赖 CTE 方言，且便于校验）。
// 规则：节点 ancestors = 从根到"父节点"的路径，根节点为 "0"。
// 例：4(物流部,根) → 5(三亚预订仓)，则 dept5.ancestors = "0,4"。
func backfillAncestors() error {
	var all []models.Department
	if err := db.DB.Find(&all).Error; err != nil {
		return err
	}
	byID := map[uint]models.Department{}
	for _, d := range all {
		byID[d.ID] = d
	}
	pathOf := map[uint]string{}
	var build func(id uint, guard map[uint]bool) string
	build = func(id uint, guard map[uint]bool) string {
		if p, ok := pathOf[id]; ok {
			return p
		}
		d, ok := byID[id]
		if !ok {
			return "0"
		}
		if guard[id] { // 脏数据成环保护
			return "0"
		}
		guard[id] = true
		var p string
		if d.ParentID == 0 {
			p = "0"
		} else {
			parentPath := build(d.ParentID, guard)
			if parentPath == "0" {
				p = fmt.Sprintf("0,%d", d.ParentID)
			} else {
				p = fmt.Sprintf("%s,%d", parentPath, d.ParentID)
			}
		}
		pathOf[id] = p
		return p
	}
	for _, d := range all {
		build(d.ID, map[uint]bool{})
	}
	// 只写有变化的行，避免无谓写放大
	for _, d := range all {
		want := pathOf[d.ID]
		if d.Ancestors == want {
			continue
		}
		if err := db.DB.Model(&models.Department{}).Where("id = ?", d.ID).
			Update("ancestors", want).Error; err != nil {
			return err
		}
	}
	rbac.InvalidateDeptCache()
	return nil
}

// ---------------------------------------------------------------- 菜单种子

type seedMenu struct {
	Name      string
	Type      string // M / C / F
	Path      string
	Component string
	Icon      string
	Perms     string
	Visible   int
	Children  []seedMenu
}

func dir(name, icon string, children ...seedMenu) seedMenu {
	return seedMenu{Name: name, Type: models.MenuTypeDir, Icon: icon, Children: children}
}

func page(name, path, component, perms string, children ...seedMenu) seedMenu {
	return seedMenu{Name: name, Type: models.MenuTypeMenu, Path: path, Component: component, Perms: perms, Children: children}
}

// dirHidden 隐藏目录：承载"没有独立页面、但需要被按角色开关"的基础权限节点。
// 不出现在导航里，但在「菜单管理」中可见可授权（同 RuoYi 对纯接口权限节点的做法）。
func dirHidden(name, icon string, children ...seedMenu) seedMenu {
	return seedMenu{Name: name, Type: models.MenuTypeDir, Icon: icon, Visible: 1, Children: children}
}

// pageHidden 用于"没有独立页面但需要承载按钮权限"的节点（如通知中心）。
func pageHidden(name, perms string, children ...seedMenu) seedMenu {
	return seedMenu{Name: name, Type: models.MenuTypeMenu, Perms: perms, Visible: 1, Children: children}
}

func btn(name, perms string) seedMenu {
	return seedMenu{Name: name, Type: models.MenuTypeButton, Perms: perms}
}

// MenuSeed 返回完整的权限树。**覆盖范围要求（用户明确）**：
// 每一个功能、模块、按钮都必须在这里有对应的权限节点，且与 routeperm.go 的声明一一对应。
func MenuSeed() []seedMenu {
	return []seedMenu{
		page("首页", "/", "Dashboard", PDashboardView),

		dir("班表管理", "schedule",
			page("班表", "/schedule", "Schedule", PScheduleView,
				btn("班次配置", PScheduleShiftConf),
				btn("排班规则", PScheduleRule),
				btn("员工偏好", PSchedulePref),
				btn("特殊工作日/休息日", PScheduleSpecial),
				btn("生成班表", PScheduleGenerate),
				btn("校验班表", PScheduleValidate),
				btn("应用班表", PScheduleApply),
				btn("排班草稿", PScheduleDraft),
				btn("手工编辑班表", PScheduleEdit),
				btn("删除班表", PScheduleRemove),
				btn("导入班表", PScheduleImport),
				btn("导出班表", PScheduleExport),
				btn("解锁换班申请", PScheduleReqUnlock),
			),
		),

		dir("任务管理", "tasks",
			page("任务", "/tasks", "Tasks", PTaskView,
				btn("新增任务", PTaskAdd),
				btn("编辑任务", PTaskEdit),
				btn("删除任务", PTaskRemove),
				btn("冻结任务", PTaskFreeze),
				btn("任务导入", PTaskImport),
				btn("任务导出", PTaskExport),
				btn("完成记录", PTaskCompletion),
				btn("完成记录（全部/跨人）", PTaskCompletionAll),
			),
		),

		dir("知识库", "workspace",
			page("知识库", "/workspace", "Workspace", PKnowledgeView,
				btn("新增条目", PKnowledgeAdd),
				btn("编辑条目", PKnowledgeEdit),
				btn("删除条目", PKnowledgeRemove),
				btn("附件管理", PKnowledgeAttachment),
				btn("评论", PKnowledgeComment),
				btn("回收站/恢复/清空", PKnowledgeTrash),
				btn("版本回滚", PKnowledgeVersion),
				btn("模板管理", PKnowledgeTemplate),
				btn("导入", PKnowledgeImport),
				btn("导出（全量包/Markdown/Word）", PKnowledgeExport),
			),
			page("工作日志", "/workspace?tab=log", "Workspace", PWorklogView,
				btn("新增日志", PWorklogAdd),
				btn("编辑日志", PWorklogEdit),
				btn("删除日志", PWorklogRemove),
				btn("日志转交接", PWorklogHandover),
				btn("导出日志", PWorklogExport),
			),
			page("交接接力", "/workspace?tab=handover", "Workspace", PHandoverView,
				btn("新建交接", PHandoverAdd),
				btn("更新交接状态", PHandoverStatus),
				btn("催办", PHandoverUrge),
				btn("删除交接", PHandoverRemove),
			),
			pageHidden("工作台附件", PWSAttachView,
				btn("上传附件", PWSAttachUpload),
				btn("下载附件", PWSAttachDownload),
				btn("删除附件", PWSAttachRemove),
			),
		),

		dir("通知中心", "alert",
			pageHidden("通知", PNotifyView,
				btn("发送广播", PNotifyBroadcast),
				btn("广播统计", PNotifyBcastList),
				btn("定时广播", PNotifyBcastAdd),
				btn("取消定时广播", PNotifyBcastDel),
				btn("催办广播", PNotifyBcastNudge),
			),
		),

		dir("企微推送", "wecom",
			page("企微推送", "/wecom-push", "WecomPush", PWpView,
				btn("查看推送日志", PWpLogView),
				btn("新建推送任务", PWpTaskAdd),
				btn("编辑推送任务", PWpTaskEdit),
				btn("删除推送任务", PWpTaskRemove),
				btn("立即执行任务", PWpTaskRun),
				btn("任务启停", PWpTaskToggle),
				btn("发送测试消息", PWpNotifyTest),
				btn("保存模块设置", PWpSetting),
				btn("重新授权", PWpAuthManage),
				btn("访问权限配置", PWpAccessConfig),
			),
		),

		// 无独立页面的「登录即用」共享只读能力：以隐藏节点承载，
		// 使「接口声明的 perm 必须能在菜单中找到」这条自检恒成立，同时不污染导航。
		dirHidden("基础权限", "users",
			pageHidden("共享只读", PCommonRoster),
		),

		dir("系统管理", "settings",
			page("用户管理", "/system/users", "SystemUser", PSysUserList,
				btn("新增用户", PSysUserAdd),
				btn("编辑用户", PSysUserEdit),
				btn("删除用户", PSysUserRemove),
				btn("重置密码", PSysUserResetPwd),
				btn("分配角色", PSysUserAuthRole),
				btn("导入用户", PSysUserImport),
				btn("导出用户", PSysUserExport),
				btn("强制下线", PSysUserForceOut),
				btn("在线会话", PSysUserSessions),
			),
			page("角色管理", "/system/roles", "SystemRole", PSysRoleList,
				btn("新增角色", PSysRoleAdd),
				btn("编辑角色", PSysRoleEdit),
				btn("删除角色", PSysRoleRemove),
				btn("分配菜单权限", PSysRoleMenu),
				btn("分配数据范围", PSysRoleDataScop),
			),
			page("菜单管理", "/system/menus", "SystemMenu", PSysMenuList,
				btn("新增菜单", PSysMenuAdd),
				btn("编辑菜单", PSysMenuEdit),
				btn("删除菜单", PSysMenuRemove),
			),
			page("部门管理", "/system/depts", "SystemDept", PSysDeptList,
				btn("新增部门", PSysDeptAdd),
				btn("编辑部门", PSysDeptEdit),
				btn("删除部门", PSysDeptRemove),
			),
			page("系统设置", "/settings", "Settings", PSysSettingView,
				btn("保存常规设置", PSysSettingEdit),
				btn("邮件通知（SMTP）", PSysSettingSmtp),
				btn("企业 Logo", PSysSettingLogo),
				btn("日志保留", PSysSettingReten),
				btn("时区", PSysSettingTz),
				btn("每日汇总推送", PSysSettingDaily),
				btn("逾期宽限", PSysSettingGrace),
			),
			pageHidden("模板管理", PSysTplList,
				btn("新增模板", PSysTplAdd),
				btn("删除模板", PSysTplRemove),
			),
			pageHidden("Webhook", PSysHookList,
				btn("新增 Webhook", PSysHookAdd),
				btn("编辑 Webhook", PSysHookEdit),
				btn("删除 Webhook", PSysHookRemove),
				btn("测试 Webhook", PSysHookTest),
				btn("推送今日汇总", PSysHookNotify),
			),
			pageHidden("备份还原", PSysBakList,
				btn("新建备份", PSysBakAdd),
				btn("下载备份", PSysBakDownload),
				btn("还原备份", PSysBakRestore),
				btn("删除备份", PSysBakRemove),
				btn("导入备份", PSysBakImport),
				btn("自动备份配置", PSysBakConfig),
			),
			pageHidden("系统日志", PSysLogList,
				btn("导出运行日志", PSysLogExport),
				btn("解锁登录", PSysLogUnlock),
			),
		),
	}
}

// ---------------------------------------------------------------- 角色种子

type seedRole struct {
	Key       string
	Name      string
	Sort      int
	DataScope int
	Remark    string
	// AllowPerms：该角色被授予的 perms 前缀白名单（按前缀匹配，含精确项）。
	// 空表示"全部"。
	AllowPrefixes []string
	DenyExact     []string
}

func execPagePerm(name, perms string, children ...seedMenu) seedMenu {
	return page(name, "", "", perms, children...)
}

// RoleSeeds 三个内置角色模板（方案 §13.4）。
//
// 授权范围刻意与"改造前 main.go 里的 RequireRole 矩阵"对齐，
// 保证从角色硬编码切到 perms 时不发生"原本能用的人突然不能用了"。
func RoleSeeds() []seedRole {
	return []seedRole{
		{
			Key: models.RoleKeySuperAdmin, Name: "超级管理员", Sort: 1, DataScope: models.DataScopeAll,
			Remark: "内置角色：不可删除、role_key 不可改、不可停用；恒可访问全部功能",
			// 空 = 全部
		},
		{
			Key: models.RoleKeyDeptAdmin, Name: "部门管理员", Sort: 2, DataScope: models.DataScopeDeptAndChil,
			Remark: "内置角色：管理边界由本人 dept_id 锚定（本部门及以下）",
			AllowPrefixes: []string{
				"dashboard:", "schedule:", "task:", "knowledge:", "worklog:", "handover:",
				"wsattach:", "notify:", "wecompush:", "common:",
				"system:user:", "system:template:", "system:webhook:", "system:log:",
			},
			DenyExact: []string{
				// 提权/收权类动作不下放（方案 §8.3）：改角色绑定、强制下线、查看会话
				PSysUserAuthRole, PSysUserForceOut, PSysUserSessions,
				// 越权风险：模块访问白名单由超管独占
				PWpAccessConfig,
				// 知识库全量导出含全部门内容（改造前即 super_admin 独占）
				PKnowledgeExport,
			},
		},
		{
			Key: models.RoleKeyExecutor, Name: "执行者", Sort: 3, DataScope: models.DataScopeSelf,
			Remark: "内置角色：对齐改造前登录即可用的能力（读 + 本人操作），非系统兜底默认值；可按需在角色管理中收权",
			AllowPrefixes: []string{
				"common:", "dashboard:", "schedule:view", "task:view", "task:completion",
				"knowledge:view", "knowledge:add", "knowledge:edit", "knowledge:remove",
				"knowledge:comment", "knowledge:attachment", "knowledge:trash",
				"knowledge:version", "knowledge:template", "knowledge:import",
				"worklog:view", "worklog:add", "worklog:edit", "worklog:remove", "worklog:handover",
				// worklog:export 必需：GET /api/workspace/logs/export 改造前无 RequireRole（登录即可），
				// 且 Workspace.vue 的「导出」按钮没有任何角色门控 —— 漏掉它会让执行者的导出直接 403。
				"worklog:export",
				"handover:view", "handover:add", "handover:status", "handover:remove", "handover:urge",
				"wsattach:view", "wsattach:upload", "wsattach:download", "wsattach:remove",
				"notify:view",
			},
		},
	}
}

// matched 判断 perm 是否落在该角色的授权范围内。
func (r seedRole) matched(perm string) bool {
	if len(r.AllowPrefixes) == 0 {
		return true // 全开
	}
	for _, d := range r.DenyExact {
		if d == perm {
			return false
		}
	}
	for _, p := range r.AllowPrefixes {
		if p == perm {
			return true
		}
		if strings.HasSuffix(p, ":") && strings.HasPrefix(perm, p) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- 执行播种

// Seed 执行 RBAC 播种（幂等）。返回统计信息用于启动日志。
func Seed() (map[string]int, error) {
	stat := map[string]int{}
	if db.DB == nil {
		return stat, fmt.Errorf("数据库未初始化")
	}
	if err := rbac.WithWrite(db.DB, func(tx *gorm.DB) error {
		// 1) 菜单树（按 父ID+名称 幂等）
		created, err := upsertMenus(tx, 0, MenuSeed())
		if err != nil {
			return err
		}
		stat["menus_created"] = created

		// 2) 角色
		for _, rs := range RoleSeeds() {
			var r models.SysRole
			err := tx.Where("role_key = ?", rs.Key).First(&r).Error
			if err == gorm.ErrRecordNotFound {
				r = models.SysRole{
					RoleName: rs.Name, RoleKey: rs.Key, RoleSort: rs.Sort,
					DataScope: rs.DataScope, Status: 0, Remark: rs.Remark,
				}
				if err := tx.Create(&r).Error; err != nil {
					return err
				}
				stat["roles_created"]++
			} else if err != nil {
				return err
			}
		}

		// 3) 角色 → 菜单授权
		var menus []models.SysMenu
		if err := tx.Find(&menus).Error; err != nil {
			return err
		}
		for _, rs := range RoleSeeds() {
			var r models.SysRole
			if err := tx.Where("role_key = ?", rs.Key).First(&r).Error; err != nil {
				return err
			}
			for _, m := range menus {
				// 目录节点始终授权（否则子节点在前端无法成树）
				allow := m.MenuType == models.MenuTypeDir || rs.matched(m.Perms)
				if m.Perms == "" && m.MenuType != models.MenuTypeDir {
					allow = rs.Key == models.RoleKeySuperAdmin
				}
				if !allow {
					continue
				}
				var n int64
				tx.Model(&models.SysRoleMenu{}).Where("role_id = ? AND menu_id = ?", r.ID, m.ID).Count(&n)
				if n == 0 {
					if err := tx.Create(&models.SysRoleMenu{RoleID: r.ID, MenuID: m.ID}).Error; err != nil {
						return err
					}
					stat["role_menus"]++
				}
			}
		}

		// 4) users.role → user_roles 回填（双读：users.role 的值不动）
		res := tx.Exec(`
			INSERT OR IGNORE INTO user_roles(user_id, role_id)
			SELECT u.id, r.id FROM users u JOIN roles r ON r.role_key = u.role
			WHERE u.role IN (?, ?, ?)`,
			models.RoleKeySuperAdmin, models.RoleKeyDeptAdmin, models.RoleKeyExecutor)
		if res.Error != nil {
			return res.Error
		}
		stat["user_roles_backfilled"] = int(res.RowsAffected)

		// 5) 部门管理员/执行者若一个部门都没有，给个安全的兜底：不改动任何数据，
		//    仅确保 seed 出来的角色 data_scope 与模板一致（管理员改过就不覆盖）
		return nil
	}); err != nil {
		return stat, err
	}
	return stat, nil
}

// upsertMenus 递归写入菜单，返回新建节点数。
func upsertMenus(tx *gorm.DB, parentID uint, nodes []seedMenu) (int, error) {
	created := 0
	for i, n := range nodes {
		var m models.SysMenu
		err := tx.Where("parent_id = ? AND menu_name = ?", parentID, n.Name).First(&m).Error
		want := models.SysMenu{
			ParentID: parentID, MenuName: n.Name, OrderNum: (i + 1) * 10,
			Path: n.Path, Component: n.Component, MenuType: n.Type,
			Visible: n.Visible, Status: 0, Perms: n.Perms, Icon: n.Icon,
		}
		if err == gorm.ErrRecordNotFound {
			if err := tx.Create(&want).Error; err != nil {
				return created, err
			}
			created++
			m = want
		} else if err != nil {
			return created, err
		} else {
			// 已存在：只同步"结构性字段"，不动 visible（允许管理员自行隐藏）
			upd := map[string]interface{}{
				"menu_type": n.Type, "path": n.Path, "component": n.Component,
				"perms": n.Perms, "icon": n.Icon, "order_num": want.OrderNum,
			}
			if err := tx.Model(&models.SysMenu{}).Where("id = ?", m.ID).Updates(upd).Error; err != nil {
				return created, err
			}
			m.Perms = n.Perms
		}
		if len(n.Children) > 0 {
			c, err := upsertMenus(tx, m.ID, n.Children)
			if err != nil {
				return created, err
			}
			created += c
		}
	}
	return created, nil
}

// SelfCheck 做启动自检（方案 §5.5 措施 3）：
//  1. 所有 F/C 节点 perms 非空且唯一
//  2. 接口声明的 perms 全部能在菜单表中找到
//  3. 菜单配了但接口未校验的 perms（潜在越权）→ 必须为 0
//
// 返回人类可读的问题列表（空表示通过）。
func SelfCheck() []string {
	var problems []string
	if db.DB == nil {
		return []string{"数据库未初始化"}
	}
	rep := Consistency()
	if len(rep.OnlyInMenu) > 0 {
		problems = append(problems, "菜单已配置但后端未校验（越权风险）: "+strings.Join(rep.OnlyInMenu, ", "))
	}
	if len(rep.OnlyDeclared) > 0 {
		problems = append(problems, "后端已校验但菜单未配置（功能不可达）: "+strings.Join(rep.OnlyDeclared, ", "))
	}
	if len(rep.UndeclaredAPI) > 0 {
		sort.Strings(rep.UndeclaredAPI)
		problems = append(problems, "未声明权限的接口: "+strings.Join(rep.UndeclaredAPI, ", "))
	}
	// F 节点 perms 必须非空
	var empty int64
	db.DB.Model(&models.SysMenu{}).
		Where("menu_type = ? AND (perms IS NULL OR perms = '')", models.MenuTypeButton).
		Count(&empty)
	if empty > 0 {
		problems = append(problems, fmt.Sprintf("存在 %d 个按钮节点未配置 perms", empty))
	}
	return problems
}
