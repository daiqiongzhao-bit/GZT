package system

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// 权限解析、缓存与接口闸门（方案 §5.4 / §5.5 / §9.1 / §10）
//
// 依赖约束：本包**不 import internal/middleware**（会形成 system → middleware
// → db → wecompush 之外的潜在环），全部从 gin.Context 直接取 claims。
// ============================================================================

// CtxClaimsKey 与 middleware.CtxUserKey 保持一致（同一字符串字面量，避免耦合）。
const CtxClaimsKey = "claims"

// PermManageKey 是"当前用户可管理的部门 ID 集合"的上下文键（nil 表示不受限）。
const PermManageDeptKey = "rbac_manage_depts"

// PermSetKey 是"当前用户权限集合（rbac.PermSet）"的上下文键。
//
// 为什么要把权限集合放进上下文：某些业务模块自带**模块级准入闸门**
// （典型：wecompush 的 AccessGuard，历史实现只看 users.role 的内置角色白名单，
// 导致按 RBAC 授权给自定义角色的人"给了权限却进不去"）。那些模块不能 import 本包
// （会形成 system → 业务模块 → ... 的耦合），本包也不该 import 它们。
// 折中：闸门在这里把已经算好的权限集合放进 gin.Context，业务模块按同一字符串
// 字面量取用（与 CtxClaimsKey 的做法一致，避免导入环）。
//
// 生命周期：每次请求一份（PermsOf 自带 60s 缓存，无额外开销）。
const PermSetKey = "rbac_perms"

// ---------------------------------------------------------------- 拦截模式

const (
	ModeOff = "off" // 不校验、不记录（完全回退旧行为）
	ModeLog = "log" // 校验但只记录不拦截（方案 P3 观察期）
	ModeOn  = "on"  // 正式拦截（方案 P4）
)

var enforceCache struct {
	mu   sync.RWMutex
	mode string
	at   time.Time
}

// EnforceMode 返回当前权限拦截模式：环境变量 RBAC_ENFORCE 优先，其次读 settings.rbac_enforce。
// 任何异常都回落到 ModeLog（既不静默放开也不突然全站 403，便于排障）。
func EnforceMode() string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("RBAC_ENFORCE"))); v != "" {
		return normalizeMode(v)
	}
	enforceCache.mu.RLock()
	if enforceCache.mode != "" && time.Since(enforceCache.at) < 10*time.Second {
		m := enforceCache.mode
		enforceCache.mu.RUnlock()
		return m
	}
	enforceCache.mu.RUnlock()

	mode := ModeLog
	if db.DB != nil {
		var st models.Setting
		if err := db.DB.Select("rbac_enforce").First(&st, 1).Error; err == nil && st.RbacEnforce != "" {
			mode = normalizeMode(st.RbacEnforce)
		}
	}
	enforceCache.mu.Lock()
	enforceCache.mode, enforceCache.at = mode, time.Now()
	enforceCache.mu.Unlock()
	return mode
}

func normalizeMode(v string) string {
	switch v {
	case ModeOff, ModeLog, ModeOn:
		return v
	default:
		return ModeLog
	}
}

// SetEnforceMode 写入数据库并立即刷新缓存（运维开关，无需重启）。
func SetEnforceMode(mode string) error {
	mode = normalizeMode(mode)
	if db.DB != nil {
		if err := db.DB.Model(&models.Setting{}).Where("id = 1").
			Update("rbac_enforce", mode).Error; err != nil {
			return err
		}
	}
	enforceCache.mu.Lock()
	enforceCache.mode, enforceCache.at = mode, time.Now()
	enforceCache.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------- claims

func claimsFromCtx(c *gin.Context) *models.Claims {
	v, ok := c.Get(CtxClaimsKey)
	if !ok {
		return nil
	}
	cl, ok := v.(*models.Claims)
	if !ok {
		return nil
	}
	return cl
}

// ---------------------------------------------------------------- 权限缓存

type permEntry struct {
	perms    rbac.PermSet
	roleKeys []string
	epoch    uint64
	at       time.Time
}

var (
	permMu    sync.RWMutex
	permCache = map[string]permEntry{}
	permEpoch uint64
)

const permCacheTTL = 60 * time.Second

// BumpPermEpoch 在全量失效时调用（例如菜单结构变更导致所有用户受影响）。
func BumpPermEpoch() {
	permMu.Lock()
	permEpoch++
	permCache = map[string]permEntry{}
	permMu.Unlock()
}

// InvalidatePerm 精确失效单个用户（角色/授权变更后调用）。
func InvalidatePerm(userID uint) {
	permMu.Lock()
	prefix := itoa(uint64(userID)) + ":"
	for k := range permCache {
		if strings.HasPrefix(k, prefix) {
			delete(permCache, k)
		}
	}
	permMu.Unlock()
}

// permsOf 取用户权限集合（带缓存）。
// 缓存键含 token_version：版本自增即自然失效，与鉴权中间件的失效语义完全一致。
func permsOf(cl *models.Claims) (rbac.PermSet, []string) {
	if cl == nil || cl.UserID == 0 {
		return rbac.NewPermSet(), nil
	}
	key := itoa(uint64(cl.UserID)) + ":" + itoa(uint64(cl.Version))

	permMu.RLock()
	e, ok := permCache[key]
	permMu.RUnlock()
	if ok && e.epoch == permEpoch && time.Since(e.at) < permCacheTTL {
		return e.perms, e.roleKeys
	}

	perms, keys, err := rbac.PermsAndRoles(db.DB, int64(cl.UserID), string(cl.Role))
	if err != nil {
		// 加载失败 → fail-closed：只给空集合（超管通配由 PermsAndRoles 内部按 users.role 兜底）
		perms, keys = rbac.NewPermSet(), nil
	}
	permMu.Lock()
	permCache[key] = permEntry{perms: perms, roleKeys: keys, epoch: permEpoch, at: time.Now()}
	permMu.Unlock()
	return perms, keys
}

// PermsOf 供 handler 使用：取当前用户权限集合。
func PermsOf(c *gin.Context) rbac.PermSet {
	p, _ := permsOf(claimsFromCtx(c))
	return p
}

// RoleKeysOf 取当前用户角色标识集合。
func RoleKeysOf(c *gin.Context) []string {
	_, k := permsOf(claimsFromCtx(c))
	return k
}

// HasPerm 判断当前用户是否具备某权限。
func HasPerm(c *gin.Context, perm string) bool {
	if perm == "" || perm == PermAuth {
		return true
	}
	return PermsOf(c).Has(perm)
}

// IsSuperAdmin 判断当前用户是否超级管理员（users.role 口径，向上兼容旧逻辑）。
func IsSuperAdmin(c *gin.Context) bool {
	cl := claimsFromCtx(c)
	return cl != nil && cl.Role == models.RoleSuperAdmin
}

// ---------------------------------------------------------------- 数据范围

// ScopeOf 解析当前登录人的**读**数据范围（方案 §3.3）。
func ScopeOf(c *gin.Context) (rbac.Scope, error) {
	cl := claimsFromCtx(c)
	if cl == nil {
		return rbac.Scope{}, errNoClaims
	}
	return rbac.ResolveForUser(db.DB, int64(cl.UserID), int64(cl.DeptID))
}

// ScopeIDsOf 返回当前用户可见部门 ID 集合；nil 表示不受限（超管 / 全部档）。
// 保留与既有 handlers.deptScopeIDs 相同的契约，便于直接替换。
//
// ★ 契约铁律：**非 nil 必然非空**（下面兜底逻辑不要"优化"掉）。
//
// 既有 handler 普遍写成：
//
//	if ids := deptScopeIDs(c); len(ids) > 0 { q = q.Where("dept_id IN ?", ids) }
//
// 也就是用 `len(ids) > 0` 判断"要不要过滤"。一旦这里返回**空切片**，就会被解读成
// "不过滤" —— 后果是数据越权可见，而且不只是列表：canViewDept/canManageDept 之类
// 的**授权判断**也会凭空集合误判为"无权"，把本部门数据挡成 403。实测同时踩到两种：
//   - 执行者（data_scope=5 仅本人，解析结果 DeptIDs 为空）看到全部门 673 条班表（应为 670）
//   - /api/shift-rules、/shift-prefs、/special-workdays 等对本部门数据误报 403
//
// 因此当解析结果没有部门集合时（仅本人档 / 黑洞角色 / 解析失败），一律收敛到
// **本人部门**：这是这些只认 `dept_id` 的 handler 能表达的最窄且有意义的口径 ——
// 既不会放开全量，也不至于让本部门页面空掉。注意不是展开子树：仅本人档的"本人数据"
// 其 dept_id 就是本人部门，带上子树反而会多看到子部门的数据。
func ScopeIDsOf(c *gin.Context) []uint {
	cl := claimsFromCtx(c)
	sc, err := ScopeOf(c)
	if err == nil && sc.All {
		return nil // 超管 / data_scope=1：不附加任何部门条件
	}
	out := make([]uint, 0, len(sc.DeptIDs)+1)
	if err == nil {
		for _, d := range sc.DeptIDs {
			out = append(out, uint(d))
		}
	}
	if len(out) > 0 {
		return out
	}
	if cl != nil && cl.DeptID > 0 {
		return []uint{cl.DeptID}
	}
	if cl != nil {
		// 连部门锚点都没有：返回必定匹配不到数据的哨兵值。
		// 实测 users / schedules / tasks / work_logs 均无 dept_id=0 的行，等价于"看不到"，
		// 比返回空切片（= 看全部）安全得多。
		return []uint{0}
	}
	return nil // 不可达：AuthRequired 已在前面拦下未认证请求
}

// CanManageDeptOf 判断能否操作目标部门（写边界，方案 §9.1(2)）。
// 与"可见"不同：这里在"持有相应权限的角色"子集内取最宽，避免借读范围拿写权限。
func CanManageDeptOf(c *gin.Context, deptID uint, requiredPerm string) bool {
	cl := claimsFromCtx(c)
	if cl == nil {
		return false
	}
	if IsSuperAdmin(c) || PermsOf(c).Has(rbac.WildcardPerm) {
		return true
	}
	sc, err := rbac.ResolveWriteForUser(db.DB, int64(cl.UserID), int64(cl.DeptID), requiredPerm)
	if err != nil {
		return false
	}
	return sc.ContainsTarget(int64(deptID), 0)
}

// CanManageDeptStrict 是"严格写边界"版本：只在**持有 requiredPerm 的角色**子集内取最宽，
// 用于"可见 ≠ 可管"的场景（方案 §8.2 路径 B）。一般用 CanManageDeptOf 即可。
func CanManageDeptStrict(c *gin.Context, deptID uint, requiredPerm string) bool {
	return CanManageDeptOf(c, deptID, requiredPerm)
}

// ---------------------------------------------------------------- 一致性自检（方案 §5.5）

// ConsistencyReport 是"接口 perms ↔ 菜单 F 节点 perms"的双向差集结果。
type ConsistencyReport struct {
	Declared      []string `json:"declared"`       // 代码侧声明的 perms
	InMenu        []string `json:"in_menu"`        // 菜单 F 节点的 perms
	OnlyDeclared  []string `json:"only_declared"`  // 有校验但菜单没配（功能不可达）
	OnlyInMenu    []string `json:"only_in_menu"`   // ★ 菜单配了但接口没校验（越权漏洞）
	UndeclaredAPI []string `json:"undeclared_api"` // 未在 routePerms 中声明的接口（运行时累计）
	OK            bool     `json:"ok"`
}

// Consistency 计算两个方向的差集。菜单侧从 DB 实时读取。
func Consistency() ConsistencyReport {
	declared := DeclaredPerms()
	inMenu := menuDeclaredPerms()

	dset := map[string]struct{}{}
	for _, p := range declared {
		dset[p] = struct{}{}
	}
	mset := map[string]struct{}{}
	for _, p := range inMenu {
		mset[p] = struct{}{}
	}
	rep := ConsistencyReport{Declared: declared, InMenu: inMenu}
	for _, p := range declared {
		if _, ok := mset[p]; !ok {
			rep.OnlyDeclared = append(rep.OnlyDeclared, p)
		}
	}
	for _, p := range inMenu {
		if _, ok := dset[p]; !ok {
			rep.OnlyInMenu = append(rep.OnlyInMenu, p)
		}
	}
	rep.UndeclaredAPI = UndeclaredRoutes()
	rep.OK = len(rep.OnlyDeclared) == 0 && len(rep.OnlyInMenu) == 0 && len(rep.UndeclaredAPI) == 0
	return rep
}

// menuDeclaredPerms 从菜单表读取所有**携带 perms 的节点**（C 菜单 + F 按钮）的权限标识。
//
// 为什么必须包含 C（页面）节点：页面级接口的权限标识就配在 C 节点上，
// 例如 GET /api/dashboard → dashboard:view、GET /api/system/user/list → system:user:list。
// 若只看 F，这些"后端确有校验、菜单侧也确实配置了"的权限会被误报成 OnlyDeclared（功能不可达），
// 自检报告就会永远不干净 —— 首版实测踩到：18 条全是 C 节点权限。
// M（目录）节点不承载 perms，天然不在范围内。
func menuDeclaredPerms() []string {
	if db.DB == nil {
		return nil
	}
	var perms []string
	_ = db.DB.Raw(`SELECT perms FROM menus
	               WHERE menu_type IN (?, ?) AND perms IS NOT NULL AND perms <> ''
	               ORDER BY perms`, models.MenuTypeMenu, models.MenuTypeButton).Scan(&perms).Error
	return perms
}

// ---------------------------------------------------------------- 未声明接口跟踪

var (
	undeclMu       sync.Mutex
	undeclaredSeen = map[string]struct{}{}
)

// UndeclaredRoutes 返回运行期遇到过的"未声明接口"（方法 + 模式串）。
func UndeclaredRoutes() []string {
	undeclMu.Lock()
	defer undeclMu.Unlock()
	out := make([]string, 0, len(undeclaredSeen))
	for k := range undeclaredSeen {
		out = append(out, k)
	}
	return out
}

func markUndeclared(k string) {
	undeclMu.Lock()
	undeclaredSeen[k] = struct{}{}
	undeclMu.Unlock()
}

// ---------------------------------------------------------------- 闸门中间件

// GuardByPath 是全局接口闸门：按 c.FullPath() 在 routePerms 里查所需权限。
//
//	fail-closed：未声明的接口**默认拒绝**（方案 §5.5 措施 4），
//	而不是像"逐个挂 RequirePerm"那样默认放行 —— 漏声明的后果是 403 而不是越权。
//
// 挂载位置：AuthRequired() 之后、业务路由之前。
func GuardByPath() gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := claimsFromCtx(c)
		if cl == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			return
		}
		// ★ 无论下面走哪条分支，都先把权限集合放进上下文：
		//   业务模块的模块级闸门（wecompush.AccessGuard）依赖它做"与 RBAC 同口径"的准入判定。
		perms := PermsOf(c)
		c.Set(PermSetKey, perms)

		perm, declared := LookupPerm(c.Request.Method, c.FullPath())
		if !declared {
			key := c.Request.Method + " " + c.FullPath()
			markUndeclared(key)
			switch EnforceMode() {
			case ModeOff:
				c.Next()
			case ModeLog:
				auditViolation(c, cl, "", "接口未声明权限（默认拒绝策略）", key)
				c.Next()
			default:
				auditViolation(c, cl, "", "接口未声明权限", key)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":           "接口未声明权限（RBAC 默认拒绝）",
					"perm_undeclared": true,
					"route":           key,
				})
			}
			return
		}
		if perm == "" || perm == PermAuth {
			c.Next()
			return
		}
		if perms.Has(perm) {
			c.Next()
			return
		}

		// 未通过
		switch EnforceMode() {
		case ModeOff:
			c.Next()
		case ModeLog:
			auditViolation(c, cl, perm, "权限不足（观察期仅记录）", c.FullPath())
			c.Next()
		default:
			auditViolation(c, cl, perm, "权限不足", c.FullPath())
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "无权限：" + perm,
				"perm":  perm,
				"route": c.FullPath(),
			})
		}
	}
}

// RequirePerm 是显式逐路由声明（用于新增接口；与 GuardByPath 叠加时任一不过即拒绝）。
func RequirePerm(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if HasPerm(c, perm) {
			c.Next()
			return
		}
		switch EnforceMode() {
		case ModeOff:
			c.Next()
		case ModeLog:
			auditViolation(c, claimsFromCtx(c), perm, "权限不足（观察期仅记录）", c.FullPath())
			c.Next()
		default:
			auditViolation(c, claimsFromCtx(c), perm, "权限不足", c.FullPath())
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权限：" + perm, "perm": perm})
		}
	}
}

// RequireSuper 仅超级管理员（用于 RBAC 自身的高危操作）。
func RequireSuper() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsSuperAdmin(c) {
			c.Next()
			return
		}
		switch EnforceMode() {
		case ModeOff:
			c.Next()
		case ModeLog:
			c.Next()
		default:
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "仅超级管理员可操作"})
		}
	}
}

// ---------------------------------------------------------------- 小工具

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

var errNoClaims = errString("未认证")

type errString string

func (e errString) Error() string { return string(e) }
