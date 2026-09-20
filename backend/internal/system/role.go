package system

import (
	"net/http"
	"strings"

	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============================================================================
// 角色管理（方案 §4）
//
// 角色是唯一的"授权枢纽"：同时挂功能权限（role_menus）与数据权限
// （roles.data_scope + role_depts）。两条链完全独立，撤其一不影响另一条。
// ============================================================================

type roleRow struct {
	ID                uint   `json:"id"`
	RoleName          string `json:"role_name"`
	RoleKey           string `json:"role_key"`
	RoleSort          int    `json:"role_sort"`
	DataScope         int    `json:"data_scope"`
	MenuCheckStrictly int    `json:"menu_check_strictly"`
	DeptCheckStrictly int    `json:"dept_check_strictly"`
	Status            int    `json:"status"`
	Remark            string `json:"remark"`
	Builtin           bool   `json:"builtin"`
	UserCount         int64  `json:"user_count"`
	MenuCount         int64  `json:"menu_count"`
	CreatedAt         string `json:"created_at"`
}

func (h *H) ListRoles(c *gin.Context) {
	var roles []models.SysRole
	if err := h.DB.Order("role_sort, id").Find(&roles).Error; err != nil {
		fail(c, http.StatusInternalServerError, "查询失败："+err.Error())
		return
	}
	uc, mc := h.roleCounts()
	rows := make([]roleRow, 0, len(roles))
	for _, r := range roles {
		rows = append(rows, roleRow{
			ID: r.ID, RoleName: r.RoleName, RoleKey: r.RoleKey, RoleSort: r.RoleSort,
			DataScope: r.DataScope, MenuCheckStrictly: r.MenuCheckStrictly,
			DeptCheckStrictly: r.DeptCheckStrictly, Status: r.Status, Remark: r.Remark,
			Builtin:   rbac.IsBuiltinRoleKey(r.RoleKey),
			UserCount: uc[r.ID], MenuCount: mc[r.ID],
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	ok(c, gin.H{"list": rows, "data_scopes": dataScopeOptions()})
}

func (h *H) roleCounts() (map[uint]int64, map[uint]int64) {
	uc, mc := map[uint]int64{}, map[uint]int64{}
	var urows []struct {
		RoleID uint
		N      int64
	}
	_ = h.DB.Raw(`SELECT role_id, COUNT(*) AS n FROM user_roles GROUP BY role_id`).Scan(&urows).Error
	for _, r := range urows {
		uc[r.RoleID] = r.N
	}
	var mrows []struct {
		RoleID uint
		N      int64
	}
	_ = h.DB.Raw(`SELECT role_id, COUNT(*) AS n FROM role_menus GROUP BY role_id`).Scan(&mrows).Error
	for _, r := range mrows {
		mc[r.RoleID] = r.N
	}
	return uc, mc
}

func dataScopeOptions() []gin.H {
	return []gin.H{
		{"value": models.DataScopeAll, "label": "全部数据", "desc": "不加任何限制（通常只给超级管理员）"},
		{"value": models.DataScopeCustom, "label": "自定义部门", "desc": "仅指定的部门（需至少选择 1 个）"},
		{"value": models.DataScopeDept, "label": "本部门", "desc": "仅本人所属部门"},
		{"value": models.DataScopeDeptAndChil, "label": "本部门及以下", "desc": "本部门 + 全部子部门（部门管理员标准档）"},
		{"value": models.DataScopeSelf, "label": "仅本人", "desc": "只能看到与自己相关的数据（默认最安全）"},
	}
}

func (h *H) GetRole(c *gin.Context) {
	id := queryID(c, "id")
	var r models.SysRole
	if err := h.DB.First(&r, id).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	ok(c, gin.H{"role": r, "dept_ids": h.roleDeptIDs(r.ID), "builtin": rbac.IsBuiltinRoleKey(r.RoleKey)})
}

type roleSaveReq struct {
	ID                uint   `json:"id"`
	RoleName          string `json:"role_name"`
	RoleKey           string `json:"role_key"`
	RoleSort          int    `json:"role_sort"`
	DataScope         int    `json:"data_scope"`
	MenuCheckStrictly *int   `json:"menu_check_strictly"`
	DeptCheckStrictly *int   `json:"dept_check_strictly"`
	Status            int    `json:"status"`
	Remark            string `json:"remark"`
	DeptIDs           []uint `json:"dept_ids"`
}

func (h *H) CreateRole(c *gin.Context) {
	var req roleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	if err := rbac.ValidateRoleSave(rbac.RoleSaveReq{
		RoleName: req.RoleName, RoleKey: req.RoleKey, DataScope: req.DataScope,
		Status: req.Status, DeptIDs: toInt64(req.DeptIDs),
	}); err != nil {
		badReq(c, err.Error())
		return
	}
	key := strings.TrimSpace(req.RoleKey)
	if rbac.IsBuiltinRoleKey(key) {
		badReq(c, "role_key 与内置角色冲突："+key)
		return
	}
	r := models.SysRole{
		RoleName: strings.TrimSpace(req.RoleName), RoleKey: key, RoleSort: req.RoleSort,
		DataScope: req.DataScope, MenuCheckStrictly: intOr(req.MenuCheckStrictly, 1),
		DeptCheckStrictly: intOr(req.DeptCheckStrictly, 1), Status: req.Status, Remark: req.Remark,
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		var n int64
		tx.Model(&models.SysRole{}).Where("role_key = ?", r.RoleKey).Count(&n)
		if n > 0 {
			return errStr("role_key 已存在")
		}
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		return replaceRoleDepts(tx, r.ID, req.DeptIDs)
	})
	if err != nil {
		badReq(c, "创建失败："+err.Error())
		return
	}
	writeAudit(c, "role", r.ID, r.RoleName, "create", nil,
		gin.H{"role_key": r.RoleKey, "data_scope": r.DataScope, "dept_ids": req.DeptIDs})
	ok(c, gin.H{"id": r.ID})
}

func (h *H) UpdateRole(c *gin.Context) {
	var req roleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var r models.SysRole
	if err := h.DB.First(&r, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	// 内置角色保护（方案 §4.7 L2）：禁改 role_key、禁停用；role_name 允许改
	if err := rbac.ValidateBuiltinRoleMutation(r.RoleKey, strings.TrimSpace(req.RoleKey), false); err != nil {
		badReq(c, err.Error())
		return
	}
	newKey := r.RoleKey
	if !rbac.IsBuiltinRoleKey(r.RoleKey) && strings.TrimSpace(req.RoleKey) != "" {
		newKey = strings.TrimSpace(req.RoleKey)
	}
	if err := rbac.ValidateRoleSave(rbac.RoleSaveReq{
		RoleName: req.RoleName, RoleKey: newKey, DataScope: req.DataScope,
		Status: req.Status, DeptIDs: toInt64(req.DeptIDs),
	}); err != nil {
		badReq(c, err.Error())
		return
	}
	before := gin.H{"role_name": r.RoleName, "role_key": r.RoleKey, "data_scope": r.DataScope,
		"status": r.Status, "dept_ids": h.roleDeptIDs(r.ID)}

	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		upd := map[string]interface{}{
			"role_name": strings.TrimSpace(req.RoleName), "role_key": newKey,
			"role_sort": req.RoleSort, "data_scope": req.DataScope, "status": req.Status,
			"remark":              req.Remark,
			"menu_check_strictly": intOr(req.MenuCheckStrictly, r.MenuCheckStrictly),
			"dept_check_strictly": intOr(req.DeptCheckStrictly, r.DeptCheckStrictly),
		}
		if err := tx.Model(&models.SysRole{}).Where("id = ?", r.ID).Updates(upd).Error; err != nil {
			return err
		}
		if err := replaceRoleDepts(tx, r.ID, req.DeptIDs); err != nil {
			return err
		}
		// 数据范围/状态变更 → 绑该角色的所有用户权限缓存失效（方案 §10.3）
		_, err := bumpRoleUsers(tx, r.ID)
		return err
	})
	if err != nil {
		badReq(c, "更新失败："+err.Error())
		return
	}
	writeAudit(c, "role", r.ID, r.RoleName, "update_data_scope", before,
		gin.H{"role_name": req.RoleName, "data_scope": req.DataScope, "status": req.Status,
			"dept_ids": req.DeptIDs})
	ok(c, nil)
}

// DeleteRole 删除角色：内置角色不可删；有用户绑定的角色不可删（方案 §4.1 #6）。
func (h *H) DeleteRole(c *gin.Context) {
	id := queryID(c, "id")
	var r models.SysRole
	if err := h.DB.First(&r, id).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	if err := rbac.ValidateBuiltinRoleMutation(r.RoleKey, "", true); err != nil {
		badReq(c, err.Error())
		return
	}
	var n int64
	h.DB.Model(&models.SysUserRole{}).Where("role_id = ?", r.ID).Count(&n)
	if n > 0 {
		badReq(c, "该角色下仍有 "+itoa(uint64(n))+" 个用户，请先调整其角色后再删除")
		return
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", r.ID).Delete(&models.SysRoleMenu{}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", r.ID).Delete(&models.SysRoleDept{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.SysRole{}, r.ID).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除失败："+err.Error())
		return
	}
	writeAudit(c, "role", r.ID, r.RoleName, "remove",
		gin.H{"role_key": r.RoleKey, "data_scope": r.DataScope}, nil)
	ok(c, nil)
}

// ChangeRoleStatus 角色启停（内置角色不可停用）。
func (h *H) ChangeRoleStatus(c *gin.Context) {
	var req struct {
		ID     uint `json:"id"`
		Status int  `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var r models.SysRole
	if err := h.DB.First(&r, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	if req.Status != 0 && rbac.IsBuiltinRoleKey(r.RoleKey) {
		badReq(c, "内置角色不可停用："+r.RoleKey)
		return
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&models.SysRole{}).Where("id = ?", r.ID).
			Update("status", req.Status).Error; err != nil {
			return err
		}
		_, err := bumpRoleUsers(tx, r.ID)
		return err
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "更新失败："+err.Error())
		return
	}
	writeAudit(c, "role", r.ID, r.RoleName, "change_status",
		gin.H{"status": r.Status}, gin.H{"status": req.Status})
	ok(c, nil)
}

// ---------------------------------------------------------------- 分配菜单/按钮

type menuNode struct {
	ID       uint       `json:"id"`
	Name     string     `json:"name"`
	Type     string     `json:"type"`
	Perms    string     `json:"perms"`
	Path     string     `json:"path"`
	Visible  int        `json:"visible"`
	Status   int        `json:"status"`
	Checked  bool       `json:"checked"`
	Children []menuNode `json:"children,omitempty"`
}

// RoleMenuTree 返回完整菜单树 + 该角色已勾选的 id（方案 §4.4）。
func (h *H) RoleMenuTree(c *gin.Context) {
	roleID := queryID(c, "roleId")
	var r models.SysRole
	if err := h.DB.First(&r, roleID).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	checked := map[uint]bool{}
	var ids []uint
	_ = h.DB.Raw(`SELECT menu_id FROM role_menus WHERE role_id = ?`, roleID).Scan(&ids).Error
	for _, id := range ids {
		checked[id] = true
	}
	tree := h.menuTree(checked)
	ok(c, gin.H{
		"role": r, "tree": tree, "checked_ids": ids,
		"builtin":    rbac.IsBuiltinRoleKey(r.RoleKey),
		"super_lock": r.RoleKey == models.RoleKeySuperAdmin,
	})
}

func (h *H) menuTree(checked map[uint]bool) []menuNode {
	var all []models.SysMenu
	_ = h.DB.Order("parent_id, order_num, id").Find(&all).Error
	return buildMenuTree(all, 0, checked)
}

func buildMenuTree(all []models.SysMenu, parent uint, checked map[uint]bool) []menuNode {
	var out []menuNode
	for _, m := range all {
		if m.ParentID != parent {
			continue
		}
		n := menuNode{
			ID: m.ID, Name: m.MenuName, Type: m.MenuType, Perms: m.Perms,
			Path: m.Path, Visible: m.Visible, Status: m.Status,
			Checked:  checked == nil || checked[m.ID],
			Children: buildMenuTree(all, m.ID, checked),
		}
		out = append(out, n)
	}
	return out
}

// UpdateRoleMenu 保存角色的菜单/按钮授权（先删后插，同事务）。
func (h *H) UpdateRoleMenu(c *gin.Context) {
	var req struct {
		RoleID  uint   `json:"role_id"`
		MenuIDs []uint `json:"menu_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var r models.SysRole
	if err := h.DB.First(&r, req.RoleID).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	// 超管必须保留全部权限，防止管理员把自己锁死（方案 §13.4 解析层）
	if r.RoleKey == models.RoleKeySuperAdmin {
		var total int64
		h.DB.Model(&models.SysMenu{}).Count(&total)
		if int64(len(req.MenuIDs)) < total {
			badReq(c, "超级管理员必须拥有全部权限（不允许取消勾选）")
			return
		}
	}
	before := h.roleMenuIDs(req.RoleID)
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", r.ID).Delete(&models.SysRoleMenu{}).Error; err != nil {
			return err
		}
		for _, mid := range req.MenuIDs {
			if err := tx.Create(&models.SysRoleMenu{RoleID: r.ID, MenuID: mid}).Error; err != nil {
				return err
			}
		}
		_, err := bumpRoleUsers(tx, r.ID)
		return err
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存失败："+err.Error())
		return
	}
	writeAudit(c, "role", r.ID, r.RoleName, "update_menu",
		gin.H{"menu_ids": before}, gin.H{"menu_ids": req.MenuIDs})
	ok(c, gin.H{"count": len(req.MenuIDs)})
}

func (h *H) roleMenuIDs(roleID uint) []uint {
	var ids []uint
	_ = h.DB.Raw(`SELECT menu_id FROM role_menus WHERE role_id = ? ORDER BY menu_id`, roleID).Scan(&ids).Error
	return ids
}

// ---------------------------------------------------------------- 分配数据范围

func (h *H) RoleDeptTree(c *gin.Context) {
	roleID := queryID(c, "roleId")
	var r models.SysRole
	if err := h.DB.First(&r, roleID).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	checked := map[uint]bool{}
	for _, id := range h.roleDeptIDs(roleID) {
		checked[id] = true
	}
	ok(c, gin.H{
		"role": r, "tree": h.deptTree(checked, false),
		"checked_ids": h.roleDeptIDs(roleID), "data_scopes": dataScopeOptions(),
	})
}

// UpdateRoleDataScope 保存数据范围（含 P0-2 的"空集合拒绝保存"）。
func (h *H) UpdateRoleDataScope(c *gin.Context) {
	var req struct {
		RoleID    uint   `json:"role_id"`
		DataScope int    `json:"data_scope"`
		DeptIDs   []uint `json:"dept_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var r models.SysRole
	if err := h.DB.First(&r, req.RoleID).Error; err != nil {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	}
	if req.DataScope < models.DataScopeAll || req.DataScope > models.DataScopeSelf {
		badReq(c, "数据范围取值非法")
		return
	}
	// ★ P0-2：data_scope=2 且未选择任何部门 → 写入侧直接拒绝（黑洞角色）
	if req.DataScope == models.DataScopeCustom && len(req.DeptIDs) == 0 {
		badReq(c, "数据范围为「自定义部门」时必须至少选择 1 个部门")
		return
	}
	before := gin.H{"data_scope": r.DataScope, "dept_ids": h.roleDeptIDs(r.ID)}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&models.SysRole{}).Where("id = ?", r.ID).
			Update("data_scope", req.DataScope).Error; err != nil {
			return err
		}
		// 切换档位离开 2 时保留 role_depts（切回来不用重配），解析期忽略它
		if err := replaceRoleDepts(tx, r.ID, req.DeptIDs); err != nil {
			return err
		}
		_, err := bumpRoleUsers(tx, r.ID)
		return err
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存失败："+err.Error())
		return
	}
	writeAudit(c, "role", r.ID, r.RoleName, "update_data_scope", before,
		gin.H{"data_scope": req.DataScope, "dept_ids": req.DeptIDs})
	ok(c, nil)
}

func (h *H) roleDeptIDs(roleID uint) []uint {
	var ids []uint
	_ = h.DB.Raw(`SELECT dept_id FROM role_depts WHERE role_id = ? ORDER BY dept_id`, roleID).Scan(&ids).Error
	return ids
}

func replaceRoleDepts(tx *gorm.DB, roleID uint, deptIDs []uint) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&models.SysRoleDept{}).Error; err != nil {
		return err
	}
	for _, d := range deptIDs {
		if err := tx.Create(&models.SysRoleDept{RoleID: roleID, DeptID: d}).Error; err != nil {
			return err
		}
	}
	return nil
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

type errStr string

func (e errStr) Error() string { return string(e) }
