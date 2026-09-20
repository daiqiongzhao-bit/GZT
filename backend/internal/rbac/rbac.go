// Package rbac 提供 RBAC 的公共原语：权限集合、数据范围解析与渲染、写路径边界、内置角色保护。
//
// 依赖约束（方案 §7.3，务必遵守）：
//   - 只能依赖标准库 + gorm.io/gorm + 叶子包 internal/models
//   - 严禁 import internal/middleware 或 internal/db —— 会形成导入环
//     （internal/db 为 AutoMigrate 反向 import 了业务模块，middleware 又 import db）
//   - 需要数据库时通过参数注入 *gorm.DB，不 import db 包
package rbac

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"shiftworkbench/internal/models"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------- 数据范围档位

// 与 roles.data_scope 一一对应（数值同 models.DataScope*）
const (
	ScopeAll          = models.DataScopeAll
	ScopeCustom       = models.DataScopeCustom
	ScopeDept         = models.DataScopeDept
	ScopeDeptAndChild = models.DataScopeDeptAndChil
	ScopeSelf         = models.DataScopeSelf
)

// DeptIDInListLimit 是 IN 列表渲染的阈值（方案 §13.2 / 评审 P0-1）。
// 展开后的部门数超过该值时，RenderAuto 会改用子查询渲染，把展开交回数据库，
// 避免 `IN (上千个 id)` 在 SQLite 上退化为全表扫 + 超长 SQL。
const DeptIDInListLimit = 200

// RenderMode 控制数据范围的渲染方式。
type RenderMode int

const (
	RenderAuto     RenderMode = iota // 按 DeptIDInListLimit 自动选择（推荐默认）
	RenderInList                     // 强制 IN (?)，便于单测断言
	RenderSubquery                   // 强制子查询，便于单测断言
)

// ---------------------------------------------------------------- 输入

// RoleGrant 是"用户绑定的一个启用角色"的精简视图。
type RoleGrant struct {
	RoleID    int64
	RoleKey   string
	DataScope int
	Status    int // 0 正常 / 1 停用；非 0 的角色一律不参与解析
	Perms     []string
}

// DeptTree 由调用方注入（见 depttree.go 的 GormDeptTree），避免本包 import internal/db。
type DeptTree interface {
	SubtreeIDs(root int64) ([]int64, error) // root 及其全部子孙的 id（含 root）
	DeptExists(id int64) (bool, error)
}

// ResolveInput 是解析数据范围所需的全部输入。
type ResolveInput struct {
	UserID int64
	DeptID int64

	// Grants：该用户绑定的全部角色（内部会过滤 Status != 0）
	Grants []RoleGrant

	// CustomDeptIDs：来自 role_depts，且已限定为"启用角色中 data_scope=2 的那些角色"的并集。
	CustomDeptIDs []int64

	Tree DeptTree

	// RestrictRoleIDs 非空时只在给定角色子集内解析。★ 写路径必须设置（见 ResolveWriteBoundary）。
	RestrictRoleIDs []int64

	Mode RenderMode
}

// ---------------------------------------------------------------- 输出

// Scope 是解析后的数据范围，可渲染成任意查询的过滤条件。
type Scope struct {
	All          bool    // 任一角色 data_scope=1 → true（唯一"不附加条件"的情况）
	DeptIDs      []int64 // 展开后的部门集合（已去重排序）
	SubtreeRoots []int64 // 子树根：子查询回退时用它重新展开
	DeptID       int64   // 锚点部门
	UserID       int64
	IncludeSelf  bool // 集合之外是否还要并上"我自己"（"仅本人"档 / 多档并集）
	FallbackSelf bool // ★ 显式兜底标记：无任何档位命中（命名出来便于测试断言与排障）
	Mode         RenderMode
}

// ---------------------------------------------------------------- 解析

// ResolveScope 解析"读"用的数据范围：多角色取最宽（并集）。
//
// 注意：返回值只用于读查询。写操作请用 ResolveWriteBoundary。
func ResolveScope(in ResolveInput) (Scope, error) { return resolve(in) }

// ResolveWriteBoundary 解析"写"用的边界。
//
// ★ 为什么不直接复用 ResolveScope（评审 P2 的核心）：
// ResolveScope 取多角色并集。若用户同时持有
//
//	角色 A：data_scope=5（仅本人），含写权限
//	角色 B：data_scope=1（全部），不含任何写权限
//
// 并集结果是"全部"。拿它去卡写接口，用户就借角色 B 的读范围获得了全量写能力。
// 正确做法：写边界只在"真正授予了该操作所需 perms 的角色"子集内取最宽。
func ResolveWriteBoundary(in ResolveInput, restrictToRoleIDs []int64) (Scope, error) {
	if len(restrictToRoleIDs) == 0 {
		// 没有任何角色授予该权限 → 边界收敛为"仅本人"，绝不退化成"全部"
		return Scope{
			UserID:       in.UserID,
			DeptID:       in.DeptID,
			IncludeSelf:  true,
			FallbackSelf: true,
			Mode:         in.Mode,
		}, nil
	}
	in.RestrictRoleIDs = restrictToRoleIDs
	return resolve(in)
}

func resolve(in ResolveInput) (Scope, error) {
	s := Scope{UserID: in.UserID, DeptID: in.DeptID, Mode: in.Mode}

	// 1) 停用角色不参与解析（方案 §9.1(1)）
	grants := make([]RoleGrant, 0, len(in.Grants))
	for _, g := range in.Grants {
		if g.Status != 0 {
			continue
		}
		if len(in.RestrictRoleIDs) > 0 && !ContainsInt64(in.RestrictRoleIDs, g.RoleID) {
			continue
		}
		grants = append(grants, g)
	}

	// 2) 无可用角色 → 兜底"仅本人"（fail-closed）
	if len(grants) == 0 {
		s.FallbackSelf = true
		return s, nil
	}

	// 3) 任一角色为"全部" → 整体不限制
	for _, g := range grants {
		if g.DataScope == ScopeAll {
			s.All = true
			return s, nil
		}
	}

	set := map[int64]struct{}{}
	roots := map[int64]struct{}{}
	wantSelf := false

	addDept := func(id int64) {
		if id > 0 {
			set[id] = struct{}{}
		}
	}

	for _, g := range grants {
		switch g.DataScope {
		case ScopeCustom:
			// 自定义部门：仅这些部门本身，不含子树
			for _, d := range in.CustomDeptIDs {
				addDept(d)
			}
		case ScopeDept:
			addDept(in.DeptID)
		case ScopeDeptAndChild:
			roots[in.DeptID] = struct{}{}
			if in.Tree != nil {
				ids, err := in.Tree.SubtreeIDs(in.DeptID)
				if err != nil {
					return Scope{}, fmt.Errorf("展开部门子树失败(dept=%d): %w", in.DeptID, err)
				}
				for _, id := range ids {
					addDept(id)
				}
			} else {
				// 没有注入部门树：至少保住锚点，不能静默放开
				addDept(in.DeptID)
			}
		case ScopeSelf:
			wantSelf = true
		default:
			// 未知/非法档位 → 按最严处理（不加入任何部门）
			wantSelf = true
		}
	}

	// 4) ★ P0-2：data_scope=2 但自定义部门为空（"黑洞角色"）→ 降级"仅本人"
	//    不返回"无任何条件"，也不返回"0 行可见"，而是可解释的最小权限。
	if len(set) == 0 {
		s.FallbackSelf = true
		s.IncludeSelf = true
		return s, nil
	}

	s.DeptIDs = make([]int64, 0, len(set))
	for id := range set {
		s.DeptIDs = append(s.DeptIDs, id)
	}
	sort.Slice(s.DeptIDs, func(i, j int) bool { return s.DeptIDs[i] < s.DeptIDs[j] })

	s.SubtreeRoots = make([]int64, 0, len(roots))
	for id := range roots {
		s.SubtreeRoots = append(s.SubtreeRoots, id)
	}
	sort.Slice(s.SubtreeRoots, func(i, j int) bool { return s.SubtreeRoots[i] < s.SubtreeRoots[j] })

	// "仅本人"与部门集合共存时，两个条件取 OR
	s.IncludeSelf = wantSelf
	return s, nil
}

// ---------------------------------------------------------------- 渲染

// Filter 返回 GORM 查询范围函数，必须挂在**任何返回数据的查询**上（方案 §9.1(3)）。
func (s Scope) Filter(deptCol, userCol string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		// 1) 全开：唯一不附加条件的分支。All 只由 resolve() 依据 data_scope=1 置位，
		//    不接受任何来自请求参数的直接赋值。
		if s.All {
			return db
		}
		// 2) 有部门集合 → IN 列表 或 子查询
		if len(s.DeptIDs) > 0 {
			cond, args := s.renderDeptCond(deptCol)
			if s.IncludeSelf {
				return db.Where(cond+" OR "+userCol+" = ?", append(args, s.UserID)...)
			}
			return db.Where(cond, args...)
		}
		// 3) ★ 最后一道闸门：永远退化为"仅本人"，绝不返回无条件的 db（fail-closed）。
		return db.Where(userCol+" = ?", s.UserID)
	}
}

func (s Scope) renderDeptCond(deptCol string) (string, []interface{}) {
	if s.useSubquery() {
		return deptCol + " IN (" + s.subtreeQuery() + ")", nil
	}
	return deptCol + " IN ?", []interface{}{s.DeptIDs}
}

func (s Scope) useSubquery() bool {
	if len(s.SubtreeRoots) == 0 {
		return false // 没有子树根就无法回退成子查询（自定义部门只能是 IN 列表）
	}
	switch s.Mode {
	case RenderSubquery:
		return true
	case RenderInList:
		return false
	default:
		return len(s.DeptIDs) > DeptIDInListLimit
	}
}

// subtreeQuery 生成部门子树子查询 SQL。
// root 全部是 int64，用 %d 拼接不存在注入风险；不要改成字符串参数。
func (s Scope) subtreeQuery() string {
	if len(s.SubtreeRoots) == 0 {
		return "SELECT id FROM departments WHERE 1=0"
	}
	parts := make([]string, 0, len(s.SubtreeRoots))
	for _, root := range s.SubtreeRoots {
		// 逗号包边防止前缀误匹配：部门 3 不应匹配到 13（方案 §6.3）
		parts = append(parts, fmt.Sprintf(
			"(id = %d OR (',' || ancestors || ',') LIKE '%%,%d,%%')", root, root))
	}
	return "SELECT id FROM departments WHERE " + strings.Join(parts, " OR ")
}

// ---------------------------------------------------------------- 写路径归属断言（方案 §9.1(2)）

// ContainsTarget 判断目标对象是否落在本边界内。这是**写操作**的前置校验入口。
func (s Scope) ContainsTarget(targetDeptID, targetUserID int64) bool {
	if s.All {
		return true
	}
	if s.UserID > 0 && s.UserID == targetUserID {
		return true // 自己永远在自己边界内
	}
	for _, d := range s.DeptIDs {
		if d == targetDeptID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- 权限集合

// WildcardPerm 是超管通配标识（方案 §5.4）。
const WildcardPerm = "*:*:*"

// PermSet 是只读语义的权限集合。
type PermSet map[string]struct{}

func NewPermSet(perms ...string) PermSet {
	p := make(PermSet, len(perms))
	for _, x := range perms {
		if x != "" {
			p[x] = struct{}{}
		}
	}
	return p
}

func (p PermSet) Has(perm string) bool {
	if _, ok := p[WildcardPerm]; ok {
		return true
	}
	_, ok := p[perm]
	return ok
}

func (p PermSet) HasAny(perms ...string) bool {
	for _, x := range perms {
		if p.Has(x) {
			return true
		}
	}
	return false
}

func (p PermSet) Sorted() []string {
	out := make([]string, 0, len(p))
	for x := range p {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- 写并发（方案 §10.5 / 评审 P1-3）

// WriteMu 把所有 RBAC 写操作串行化。
// GZT 当前是单实例 + SQLite（库级写锁），进程内互斥的代价近乎为零，
// 却能一次性消除"两个管理员同时改角色"导致的 database is locked 与竞态。
var WriteMu sync.Mutex

// WithWrite 是 RBAC 所有写接口的统一包装：进程内串行 + 单事务。
// 约定：事务内先完成写入，再计算"受影响用户集"并递增其 token_version（先写后算）。
func WithWrite(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	WriteMu.Lock()
	defer WriteMu.Unlock()
	return db.Transaction(fn)
}

// ---------------------------------------------------------------- 内置角色保护（方案 §4.7 / §13.4）

// BuiltinRoleKeys 是不可删除、不可改 role_key、不可停用的内置角色。
var BuiltinRoleKeys = []string{models.RoleKeySuperAdmin, models.RoleKeyDeptAdmin, models.RoleKeyExecutor}

func IsBuiltinRoleKey(key string) bool {
	for _, k := range BuiltinRoleKeys {
		if k == key {
			return true
		}
	}
	return false
}

// RoleSaveReq 角色保存请求（校验用）。
type RoleSaveReq struct {
	ID        int64
	RoleName  string
	RoleKey   string
	DataScope int
	Status    int
	DeptIDs   []int64 // 仅 data_scope=2 时有意义
}

// ValidateRoleSave 在写入前调用，返回 error 时上层回 400。
//
// ★ P0-2 的核心：不允许把"黑洞角色"写进库。
// data_scope=2 且没有部门 = 用户登录后看不到任何数据，比"仅本人"更严，
// 现场表现是"功能全 403 / 列表全空"，极易被误判成系统 bug。挡在写入侧最便宜。
func ValidateRoleSave(req RoleSaveReq) error {
	if strings.TrimSpace(req.RoleName) == "" {
		return errors.New("角色名称不能为空")
	}
	if strings.TrimSpace(req.RoleKey) == "" {
		return errors.New("role_key 不能为空")
	}
	if req.DataScope < ScopeAll || req.DataScope > ScopeSelf {
		return fmt.Errorf("数据范围取值非法：%d", req.DataScope)
	}
	if req.DataScope == ScopeCustom && len(req.DeptIDs) == 0 {
		return errors.New("数据范围为「自定义部门」时必须至少选择 1 个部门")
	}
	// 内置角色不可停用（超管尤其；其余内置角色一并收口，避免误操作）
	if req.Status != 0 && IsBuiltinRoleKey(req.RoleKey) {
		return fmt.Errorf("内置角色 %s 不可停用", req.RoleKey)
	}
	return nil
}

// ValidateBuiltinRoleMutation 校验角色接口的删除/改 key 动作（方案 §4.7 L2）。
func ValidateBuiltinRoleMutation(oldKey, newKey string, isDelete bool) error {
	if !IsBuiltinRoleKey(oldKey) {
		return nil
	}
	if isDelete {
		return fmt.Errorf("内置角色 %s 不可删除", oldKey)
	}
	if newKey != "" && newKey != oldKey {
		return fmt.Errorf("内置角色 %s 的 role_key 不可修改（role_name 可以改）", oldKey)
	}
	return nil
}

// ErrLastSuperAdmin 见 AssertNotLastSuperAdmin。
var ErrLastSuperAdmin = errors.New("系统必须保留至少 1 个启用状态的超级管理员")

// AssertNotLastSuperAdmin 由**三个入口**共同调用（方案 §4.7 L3）：
//  1. 删除用户     DELETE /api/system/user/:id
//  2. 停用用户     PUT    /api/system/user/changeStatus
//  3. 解绑/改角色  PUT    /api/system/user/authRole
//
// excludeUserID 传"即将被移除超管身份的用户"；检查排除他之后是否还剩启用超管。
func AssertNotLastSuperAdmin(tx *gorm.DB, excludeUserID int64) error {
	var n int64
	err := tx.Raw(`
		SELECT COUNT(*)
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r       ON r.id = ur.role_id
		WHERE r.role_key = ?
		  AND r.status   = 0
		  AND u.frozen   = 0
		  AND u.id <> ?`,
		models.RoleKeySuperAdmin, excludeUserID,
	).Scan(&n).Error
	if err != nil {
		return fmt.Errorf("校验超管数量失败: %w", err)
	}
	if n == 0 {
		return ErrLastSuperAdmin
	}
	return nil
}

// UserSaveReq 用户保存请求（校验用）。
type UserSaveReq struct {
	ID       int64
	Username string
	DeptID   int64
	RoleIDs  []int64
	IsCreate bool
}

// ValidateUserSave 中「创建时角色必填」对应评审小点 4 的**部分采纳**（方案 §13.4）：
// 采纳出发点（executor 是"最小可用"而非"最小安全"），调整落点（直接拒绝创建，
// 而不是静默创建一个"什么都看不见"的账号）。运行期兜底由 resolve() fail-closed 到"仅本人"，
// 并**明确禁止**"无角色 → 自动补 executor"这种隐式授权。
func ValidateUserSave(req UserSaveReq) error {
	if strings.TrimSpace(req.Username) == "" {
		return errors.New("登录名不能为空")
	}
	if req.DeptID <= 0 {
		return errors.New("归属部门不能为空")
	}
	if req.IsCreate && len(req.RoleIDs) == 0 {
		return errors.New("请至少为用户分配 1 个角色")
	}
	return nil
}

// ValidateGrantable 防止"分配权限高于自己"的提权（方案 §8.3）。
func ValidateGrantable(operatorRoleIDs, grantableRoleIDs, requestedRoleIDs []int64) error {
	for _, rid := range requestedRoleIDs {
		if !ContainsInt64(operatorRoleIDs, rid) || !ContainsInt64(grantableRoleIDs, rid) {
			return fmt.Errorf("无权分配角色 id=%d（不能分配权限高于自己的角色）", rid)
		}
	}
	return nil
}

// ---------------------------------------------------------------- 小工具

// ContainsInt64 判断切片是否包含指定值。
func ContainsInt64(xs []int64, v int64) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// ContainsUint 判断 uint 切片是否包含指定值。
func ContainsUint(xs []uint, v uint) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
