package rbac

import (
	"fmt"
	"time"

	"shiftworkbench/internal/models"

	"gorm.io/gorm"
)

// GormDeptTree 是 DeptTree 的 gorm 实现（依赖注入 *gorm.DB，不 import internal/db）。
//
// 部门树很小（GZT 实测 3 个节点），但代码按任意深度写：
//   - SubtreeIDs 一次查全表在内存里建树（避免递归 SQL 的方言问题）
//   - 结果带 30s TTL 缓存，避免每个请求都查一次库
type GormDeptTree struct {
	DB *gorm.DB
}

// SubtreeIDs 返回 root 及其全部子孙部门 id（含 root 自身）。
func (t GormDeptTree) SubtreeIDs(root int64) ([]int64, error) {
	all, err := cachedDeptList(t.DB)
	if err != nil {
		return nil, err
	}
	children := map[int64][]int64{}
	for _, d := range all {
		children[int64(d.ParentID)] = append(children[int64(d.ParentID)], int64(d.ID))
	}
	out := []int64{root}
	queue := []int64{root}
	seen := map[int64]bool{root: true}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, id := range children[p] {
			if seen[id] {
				continue // 脏数据成环时防死循环
			}
			seen[id] = true
			out = append(out, id)
			queue = append(queue, id)
		}
	}
	return out, nil
}

// DeptExists 判断部门是否存在（剔除脏数据，避免渲染出永不命中的条件）。
func (t GormDeptTree) DeptExists(id int64) (bool, error) {
	if id <= 0 {
		return false, nil
	}
	all, err := cachedDeptList(t.DB)
	if err != nil {
		return false, err
	}
	for _, d := range all {
		if int64(d.ID) == id {
			return true, nil
		}
	}
	return false, nil
}

// ---------------- 部门列表短缓存 ----------------

var (
	deptCacheAt time.Time
	deptCache   []models.Department
)

const deptCacheTTL = 30 * time.Second

func cachedDeptList(db *gorm.DB) ([]models.Department, error) {
	if time.Since(deptCacheAt) < deptCacheTTL && deptCache != nil {
		return deptCache, nil
	}
	var all []models.Department
	if err := db.Find(&all).Error; err != nil {
		return nil, err
	}
	deptCache, deptCacheAt = all, time.Now()
	return all, nil
}

// InvalidateDeptCache 在部门树变更后立即失效缓存（方案 §10.3：改部门树必须全员失效）。
func InvalidateDeptCache() { deptCacheAt = time.Time{} }

// ---------------- 角色授权加载 ----------------

// roleGrantRow 是 grants 查询的落点结构。
type roleGrantRow struct {
	RoleID    int64
	RoleKey   string
	DataScope int
	Status    int
}

// LoadGrants 读某用户绑定的全部角色（含停用角色，由 resolve 负责过滤）。
func LoadGrants(db *gorm.DB, userID int64) ([]RoleGrant, error) {
	if userID <= 0 {
		return nil, nil
	}
	var rows []roleGrantRow
	err := db.Raw(`
		SELECT r.id AS role_id, r.role_key AS role_key, r.data_scope AS data_scope, r.status AS status
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = ?`, userID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("加载用户角色失败: %w", err)
	}
	out := make([]RoleGrant, 0, len(rows))
	for _, r := range rows {
		out = append(out, RoleGrant{
			RoleID: r.RoleID, RoleKey: r.RoleKey, DataScope: r.DataScope, Status: r.Status,
		})
	}
	return out, nil
}

// LoadCustomDeptIDs 读 role_depts，但**只取 data_scope=2 且启用的角色**的部门。
// 为什么要限定：否则一个 data_scope=5 的角色被误配了 role_depts 也会被算进来。
func LoadCustomDeptIDs(db *gorm.DB, userID int64) ([]int64, error) {
	if userID <= 0 {
		return nil, nil
	}
	var ids []int64
	err := db.Raw(`
		SELECT DISTINCT rd.dept_id
		FROM role_depts rd
		JOIN user_roles ur ON ur.role_id = rd.role_id
		JOIN roles r       ON r.id = ur.role_id
		WHERE ur.user_id = ?
		  AND r.status = 0
		  AND r.data_scope = ?`, userID, ScopeCustom).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("加载自定义部门失败: %w", err)
	}
	return ids, nil
}

// ResolveForUser 是业务侧最常用的入口：给定用户，解析其"读"数据范围。
func ResolveForUser(db *gorm.DB, userID, deptID int64) (Scope, error) {
	grants, err := LoadGrants(db, userID)
	if err != nil {
		return Scope{}, err
	}
	custom, err := LoadCustomDeptIDs(db, userID)
	if err != nil {
		return Scope{}, err
	}
	return ResolveScope(ResolveInput{
		UserID:        userID,
		DeptID:        deptID,
		Grants:        grants,
		CustomDeptIDs: custom,
		Tree:          GormDeptTree{DB: db},
	})
}

// ResolveWriteForUser 解析"写"边界：只在持有 requiredPerm 的角色子集内取最宽（方案 §9.1(2)）。
func ResolveWriteForUser(db *gorm.DB, userID, deptID int64, requiredPerm string) (Scope, error) {
	grants, err := LoadGrants(db, userID)
	if err != nil {
		return Scope{}, err
	}
	custom, err := LoadCustomDeptIDs(db, userID)
	if err != nil {
		return Scope{}, err
	}
	roleIDs, err := RolesGrantingPerm(db, userID, requiredPerm)
	if err != nil {
		return Scope{}, err
	}
	return ResolveWriteBoundary(ResolveInput{
		UserID:        userID,
		DeptID:        deptID,
		Grants:        grants,
		CustomDeptIDs: custom,
		Tree:          GormDeptTree{DB: db},
	}, roleIDs)
}

// RolesGrantingPerm 返回"该用户持有的、且通过 role_menus 授予了 perm 的启用角色" id 列表。
func RolesGrantingPerm(db *gorm.DB, userID int64, perm string) ([]int64, error) {
	if userID <= 0 || perm == "" {
		return nil, nil
	}
	var ids []int64
	err := db.Raw(`
		SELECT DISTINCT r.id
		FROM user_roles ur
		JOIN roles r      ON r.id = ur.role_id
		JOIN role_menus rm ON rm.role_id = r.id
		JOIN menus m      ON m.id = rm.menu_id
		WHERE ur.user_id = ?
		  AND r.status   = 0
		  AND m.status   = 0
		  AND m.perms    = ?`, userID, perm).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("查询授予该权限的角色失败: %w", err)
	}
	return ids, nil
}

// PermsAndRoles 加载用户的权限集合与角色标识集合（供缓存与接口鉴权使用）。
// 超管硬编码返回通配，保证即使 role_menus 配置缺失也不会把管理员锁死（方案 §13.4 解析层）。
func PermsAndRoles(db *gorm.DB, userID int64, legacyRole string) (PermSet, []string, error) {
	if userID <= 0 {
		return NewPermSet(), nil, nil
	}
	var rows []struct {
		RoleKey string
		Perms   string
	}
	err := db.Raw(`
		SELECT DISTINCT r.role_key AS role_key, m.perms AS perms
		FROM user_roles ur
		JOIN roles r       ON r.id = ur.role_id
		JOIN role_menus rm ON rm.role_id = r.id
		JOIN menus m       ON m.id = rm.menu_id
		WHERE ur.user_id = ?
		  AND r.status = 0
		  AND m.status = 0
		  AND m.perms IS NOT NULL
		  AND m.perms <> ''`, userID).Scan(&rows).Error
	if err != nil {
		return nil, nil, fmt.Errorf("加载用户权限失败: %w", err)
	}
	perms := NewPermSet()
	keySet := map[string]struct{}{}
	for _, r := range rows {
		if r.RoleKey != "" {
			keySet[r.RoleKey] = struct{}{}
		}
		if r.Perms != "" {
			perms[r.Perms] = struct{}{}
		}
	}
	// 超管兜底：users.role 仍是唯一事实来源之一，硬编码通配，绝不把管理员锁死
	if legacyRole == models.RoleKeySuperAdmin {
		perms[WildcardPerm] = struct{}{}
		keySet[models.RoleKeySuperAdmin] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	return perms, keys, nil
}
