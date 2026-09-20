package system

import (
	"fmt"
	"net/http"

	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// 可分配角色（提权防护 + 角色选择器数据源）
//
// 背景：`system:user:` 是一个前缀授权。内置「部门管理员」虽持有该前缀，但已被
// DenyExact 显式排除 `system:user:authRole`（方案 §8.3：改角色绑定属提权/收权动作，
// 不下放）。不过**自定义角色**完全可能被超管授予 authRole，届时若只做"目标用户是否
// 在本部门子树内"的水平校验，该角色持有者就能把「超级管理员」授予自己的下属 —— 越权提权。
//
// 因此角色分配必须叠加一层**垂直校验**：待分配角色不得高于操作者自己。
// 这层校验是防御纵深（deny-by-design 之外的第二道），不能因为"内置角色没有 authRole"而省掉。
// ============================================================================

// RoleOption 是"可分配角色"下拉项（只暴露选择器需要的字段）。
type RoleOption struct {
	ID       uint   `json:"id"`
	RoleKey  string `json:"role_key"`
	RoleName string `json:"role_name"`
	RoleSort int    `json:"role_sort"`
}

// rolePermSets 返回 {角色ID → 权限标识集合}，只统计**启用**角色。
func (h *H) rolePermSets() map[uint]map[string]struct{} {
	var rows []struct {
		RoleID uint
		Perms  string
	}
	_ = h.DB.Raw(`SELECT rm.role_id AS role_id, m.perms AS perms
	              FROM role_menus rm
	              JOIN menus m ON m.id = rm.menu_id
	              JOIN roles r ON r.id = rm.role_id
	              WHERE r.status = 0 AND m.perms IS NOT NULL AND m.perms <> ''`).Scan(&rows).Error

	out := map[uint]map[string]struct{}{}
	for _, r := range rows {
		s := out[r.RoleID]
		if s == nil {
			s = map[string]struct{}{}
			out[r.RoleID] = s
		}
		s[r.Perms] = struct{}{}
	}
	return out
}

// GrantableRoleIDs 返回当前登录人**有权分配**的角色 ID 集合（方案 §8.3 垂直校验）。
//
//   - 超管（或持有通配权限）：全部启用角色
//   - 其他角色：权限集 ⊆ 自己权限集的角色
//
// 为什么判定标准是"子集"而不是"只能分配自己持有的角色"：
// 后者会让部门管理员无法给新同事分配权限更低的角色（如执行者），
// 而那恰恰是"部门管理员管本部门用户"最日常的操作。
// "子集"同样满足"不得提权"这条硬约束，但保住了可用性。
func (h *H) GrantableRoleIDs(c *gin.Context) []uint {
	var roles []models.SysRole
	_ = h.DB.Where("status = 0").Order("role_sort, id").Find(&roles).Error
	if len(roles) == 0 {
		return nil
	}
	all := make([]uint, 0, len(roles))
	for _, r := range roles {
		all = append(all, r.ID)
	}

	if IsSuperAdmin(c) {
		return all
	}
	own := PermsOf(c)
	if own.Has(rbac.WildcardPerm) {
		return all
	}

	sets := h.rolePermSets()
	out := make([]uint, 0, len(roles))
	for _, r := range roles {
		ps := sets[r.ID]
		subset := true
		for p := range ps {
			if !own.Has(p) {
				subset = false
				break
			}
		}
		// 权限集为空的角色天然是子集：它本身就等于"看不到任何数据"，分配它不构成提权
		if subset {
			out = append(out, r.ID)
		}
	}
	return out
}

// assertCanGrant 校验待分配角色是否都落在自己的可分配集合内。
// 返回 error 时上层应回 403。超管直接放行。
func (h *H) assertCanGrant(c *gin.Context, roleIDs []uint) error {
	if IsSuperAdmin(c) || PermsOf(c).Has(rbac.WildcardPerm) {
		return nil
	}
	grantable := h.GrantableRoleIDs(c)
	for _, rid := range roleIDs {
		if !rbac.ContainsUint(grantable, rid) {
			return fmt.Errorf("无权分配角色 id=%d（不能分配权限高于自己的角色）", rid)
		}
	}
	return nil
}

// RoleOptions 返回"当前登录人可分配的角色"，供用户管理的角色选择器使用。
//
// 声明为 @auth（登录即可）而不是 system:role:list：响应已按调用者自身权限裁剪，
// 不含任何超出其可分配范围的信息；而部门管理员必须能读到它才能管理本部门用户。
// （角色名本身并非秘密：GET /api/users 返回的 role_names 已包含它们。）
func (h *H) RoleOptions(c *gin.Context) {
	ids := h.GrantableRoleIDs(c)
	empty := make([]RoleOption, 0)
	if len(ids) == 0 {
		ok(c, gin.H{"list": empty})
		return
	}
	var roles []models.SysRole
	if err := h.DB.Where("status = 0 AND id IN ?", ids).
		Order("role_sort, id").Find(&roles).Error; err != nil {
		fail(c, http.StatusInternalServerError, "查询失败："+err.Error())
		return
	}
	opts := make([]RoleOption, 0, len(roles))
	for _, r := range roles {
		opts = append(opts, RoleOption{
			ID: r.ID, RoleKey: r.RoleKey, RoleName: r.RoleName, RoleSort: r.RoleSort,
		})
	}
	ok(c, gin.H{"list": opts})
}
