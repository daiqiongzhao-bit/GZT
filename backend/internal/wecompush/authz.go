package wecompush

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"
)

// ctxUserKey 与 middleware.CtxUserKey 的值保持一致。
//
// 这里刻意**不 import middleware**：internal/db 为了 AutoMigrate 反向 import 了本包
// （见 db.go），而 middleware 又 import db —— 若本包再 import middleware，就会形成
// wecompush → middleware → db → wecompush 的导入环，Go 直接拒绝编译（已实测踩坑）。
// models 是叶子包（不 import 任何内部包），因此只依赖 models 是安全的。
const ctxUserKey = "claims"

// ctxPermSetKey 与 system.PermSetKey 的值保持一致（同一字符串字面量，避免导入 system 包）。
// 全局闸门 system.GuardByPath 会在每个已认证请求上写入当前用户的权限集合。
const ctxPermSetKey = "rbac_perms"

// viewPerm 是本模块的页面级权限标识，与 system.PWpView 同字面量。
const viewPerm = "wecompush:view"

// claimsFromCtx 取当前登录用户（等价于 middleware.GetClaims，但避免导入环）
func claimsFromCtx(c *gin.Context) *models.Claims {
	v, exists := c.Get(ctxUserKey)
	if !exists {
		return nil
	}
	cl, ok2 := v.(*models.Claims)
	if !ok2 {
		return nil
	}
	return cl
}

// 企微推送模块的访问控制
//
// 设计（2026-09-20）：
//   - 默认**仅管理员**（超级管理员 + 部门管理员）可查看与修改本模块；
//   - 超级管理员可在「企微推送 → 设置 → 访问权限」里调整白名单角色；
//   - 超级管理员自身**始终**保留访问权，避免把自己锁在门外；
//   - 白名单存 wp_settings（模块自带的键值表），**零 DB 结构改动**、不影响 GZT 原表。
//
// 闸门挂在本模块所有业务接口上（见 RegisterRoutes），前端隐藏导航只是体验层，
// 真正的安全边界始终在后端。

// keyAccessRoles wp_settings 中的白名单键。刻意**不**放进 notify.go 的 settingKeys：
// 那个白名单是 PUT /settings 的可写键集合，若加进去，部门管理员就能改权限了。
const keyAccessRoles = "wp_access_roles"

// roleOption 系统内置角色（与 models.Role 对齐），供前端渲染勾选项
type roleOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var roleOptions = []roleOption{
	{Value: string(models.RoleSuperAdmin), Label: "超级管理员"},
	{Value: string(models.RoleDeptAdmin), Label: "部门管理员"},
	{Value: string(models.RoleExecutor), Label: "执行者"},
}

func isKnownRole(r string) bool {
	for _, o := range roleOptions {
		if o.Value == r {
			return true
		}
	}
	return false
}

// defaultAccessRoles 未配置时的默认白名单：仅管理员
func defaultAccessRoles() []string {
	return []string{string(models.RoleSuperAdmin), string(models.RoleDeptAdmin)}
}

// normalizeAccessRoles 去空、去重、剔除非内置角色，并强制保留超级管理员。
// 永远返回非空列表——配置损坏时回落到默认（失败即收紧，不放行）。
func normalizeAccessRoles(in []string) []string {
	out := make([]string, 0, len(in)+1)
	seen := map[string]bool{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] || !isKnownRole(r) {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	if !seen[string(models.RoleSuperAdmin)] {
		out = append(out, string(models.RoleSuperAdmin))
	}
	if len(out) == 0 {
		return defaultAccessRoles()
	}
	return out
}

// AccessRoles 读取白名单。未配置 / JSON 损坏 / 全非法 → 默认仅管理员。
func (h *H) AccessRoles() []string {
	raw := h.getSetting(keyAccessRoles)
	if raw == "" {
		return defaultAccessRoles()
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return defaultAccessRoles()
	}
	return normalizeAccessRoles(arr)
}

// HasAccess 指定角色是否在**角色白名单**内（超级管理员恒可）。
//
// ★ 注意：这不是唯一的准入依据。自 v0.40.0 起，RBAC 的 wecompush:view 权限是主通道
// （见 CanEnter）—— 白名单只用于"没有 RBAC 权限、但按内置角色整体放行"的历史配置兼容。
func (h *H) HasAccess(role string) bool {
	if role == string(models.RoleSuperAdmin) {
		return true
	}
	for _, r := range h.AccessRoles() {
		if r == role {
			return true
		}
	}
	return false
}

// rbacAllows 从上下文取当前用户权限集合，判断是否持有本模块的查看权限。
//
// 上下文里的集合由 system.GuardByPath 写入（含超管通配），因此这里与全局闸门口径完全一致。
func rbacAllows(c *gin.Context) bool {
	v, ok := c.Get(ctxPermSetKey)
	if !ok {
		return false
	}
	ps, ok2 := v.(rbac.PermSet)
	if !ok2 {
		return false
	}
	return ps.Has(viewPerm)
}

// CanEnter 是本模块的**唯一准入判定**（AccessGuard 与 /access 接口共用）。
//
// 三条放行路径，任一命中即可：
//  1. users.role = 超级管理员（避免把自己锁在门外）
//  2. RBAC 持有 wecompush:view（角色管理页勾选「企微推送 → 查看」）
//  3. 落在角色白名单内（「企微推送 → 设置 → 访问权限」，兼容历史配置）
//
// ★ 为什么加了第 2 条：v0.39.x 时准入只看内置角色枚举，按 RBAC 给**自定义角色**
//   勾了企微推送权限的人依然被 403 / 导航不显示（现场原话："我给了企微推送权限，
//   他反而没有"）。权限体系必须单一真源，模块白名单只能是叠加的例外，不能是否决项。
func (h *H) CanEnter(c *gin.Context, role string) bool {
	if role == string(models.RoleSuperAdmin) {
		return true
	}
	if rbacAllows(c) {
		return true
	}
	return h.HasAccess(role)
}

// AccessGuard 模块级访问闸门：既无 RBAC 权限、也不在白名单内的角色一律 403。
func (h *H) AccessGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := claimsFromCtx(c)
		if cl == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			return
		}
		if h.CanEnter(c, string(cl.Role)) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "无「企微推送」访问权限，如需开通请联系超级管理员（角色管理 → 勾选该角色 → 企微推送 → 查看）",
		})
	}
}

// GetAccess 查询访问权限配置。
// **刻意不挂在闸门上**（任何登录用户可读）：前端要据此决定导航项是否显示，
// 且返回内容仅为角色名单与「我能不能进」，不含任何业务数据。
func (h *H) GetAccess(c *gin.Context) {
	cl := claimsFromCtx(c)
	role := ""
	if cl != nil {
		role = string(cl.Role)
	}
	allowed := h.AccessRoles()
	viaRbac := rbacAllows(c)
	ok(c, gin.H{
		"roles":         roleOptions,
		"allowed_roles": allowed,
		"my_role":       role,
		"can_access":    h.CanEnter(c, role),
		// 准入依据：true = 来自 RBAC 角色权限（角色管理页勾选），false = 来自模块白名单
		"via_rbac":   viaRbac,
		"can_config": role == string(models.RoleSuperAdmin), // 仅超管可改权限
	})
}

// UpdateAccess 超级管理员设置「可访问角色」白名单。
func (h *H) UpdateAccess(c *gin.Context) {
	cl := claimsFromCtx(c)
	if cl == nil || cl.Role != models.RoleSuperAdmin {
		fail(c, http.StatusForbidden, "仅超级管理员可设置角色权限")
		return
	}
	var body struct {
		AllowedRoles []string `json:"allowed_roles"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "参数格式不正确")
		return
	}
	final := normalizeAccessRoles(body.AllowedRoles)
	b, err := json.Marshal(final)
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存失败："+err.Error())
		return
	}
	if err := h.setSetting(keyAccessRoles, string(b)); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败："+err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "allowed_roles": final})
}
