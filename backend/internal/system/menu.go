package system

import (
	"net/http"
	"strings"

	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============================================================================
// 菜单 / 权限管理（方案 §5）
//
// menu_type 只保留 M(目录) / C(菜单) / F(按钮)。接口权限的唯一事实来源是代码
// （routeperm.go），菜单表里的 F 节点 perms 必须与之逐字符一致 —— 由
// SelfCheck()/Consistency() 双向差集强制（方案 §5.5）。
// ============================================================================

func (h *H) ListMenus(c *gin.Context) {
	var all []models.SysMenu
	q := h.DB.Order("parent_id, order_num, id")
	if name := strings.TrimSpace(c.Query("menu_name")); name != "" {
		q = q.Where("menu_name LIKE ?", "%"+name+"%")
	}
	if t := strings.TrimSpace(c.Query("menu_type")); t != "" {
		q = q.Where("menu_type = ?", t)
	}
	if err := q.Find(&all).Error; err != nil {
		fail(c, http.StatusInternalServerError, "查询失败："+err.Error())
		return
	}
	ok(c, gin.H{"list": all, "tree": buildMenuTree(all, 0, nil)})
}

func (h *H) GetMenu(c *gin.Context) {
	id := queryID(c, "id")
	var m models.SysMenu
	if err := h.DB.First(&m, id).Error; err != nil {
		fail(c, http.StatusNotFound, "菜单不存在")
		return
	}
	ok(c, gin.H{"menu": m})
}

type menuSaveReq struct {
	ID        uint   `json:"id"`
	ParentID  uint   `json:"parent_id"`
	MenuName  string `json:"menu_name"`
	OrderNum  int    `json:"order_num"`
	Path      string `json:"path"`
	Component string `json:"component"`
	Query     string `json:"query"`
	IsFrame   int    `json:"is_frame"`
	IsCache   int    `json:"is_cache"`
	MenuType  string `json:"menu_type"`
	Visible   int    `json:"visible"`
	Status    int    `json:"status"`
	Perms     string `json:"perms"`
	Icon      string `json:"icon"`
}

func (h *H) CreateMenu(c *gin.Context) {
	var req menuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	if err := validateMenuSave(req, true); err != nil {
		badReq(c, err.Error())
		return
	}
	m := models.SysMenu{
		ParentID: req.ParentID, MenuName: strings.TrimSpace(req.MenuName), OrderNum: req.OrderNum,
		Path: req.Path, Component: req.Component, Query: req.Query, IsFrame: req.IsFrame,
		IsCache: req.IsCache, MenuType: req.MenuType, Visible: req.Visible, Status: req.Status,
		Perms: strings.TrimSpace(req.Perms), Icon: req.Icon,
	}
	err := rbacWriteMenu(h.DB, func(tx *gorm.DB) error {
		if err := h.assertPermsUnique(tx, m.Perms, 0); err != nil {
			return err
		}
		return tx.Create(&m).Error
	})
	if err != nil {
		badReq(c, "创建失败："+err.Error())
		return
	}
	writeAudit(c, "menu", m.ID, m.MenuName, "create", nil, gin.H{"perms": m.Perms, "type": m.MenuType})
	ok(c, gin.H{"id": m.ID})
}

func (h *H) UpdateMenu(c *gin.Context) {
	var req menuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var m models.SysMenu
	if err := h.DB.First(&m, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "菜单不存在")
		return
	}
	if err := validateMenuSave(req, false); err != nil {
		badReq(c, err.Error())
		return
	}
	if req.ParentID == m.ID {
		badReq(c, "上级菜单不能是自己")
		return
	}
	before := gin.H{"menu_name": m.MenuName, "perms": m.Perms, "path": m.Path, "visible": m.Visible}
	err := rbacWriteMenu(h.DB, func(tx *gorm.DB) error {
		if err := h.assertPermsUnique(tx, strings.TrimSpace(req.Perms), m.ID); err != nil {
			return err
		}
		return tx.Model(&models.SysMenu{}).Where("id = ?", m.ID).Updates(map[string]interface{}{
			"parent_id": req.ParentID, "menu_name": strings.TrimSpace(req.MenuName),
			"order_num": req.OrderNum, "path": req.Path, "component": req.Component,
			"query": req.Query, "is_frame": req.IsFrame, "is_cache": req.IsCache,
			"menu_type": req.MenuType, "visible": req.Visible, "status": req.Status,
			"perms": strings.TrimSpace(req.Perms), "icon": req.Icon,
		}).Error
	})
	if err != nil {
		badReq(c, "更新失败："+err.Error())
		return
	}
	// 结构变更（path/component/visible）影响所有用户的导航渲染 → 全员失效（方案 §10.3）
	BumpPermEpoch()
	writeAudit(c, "menu", m.ID, m.MenuName, "update", before,
		gin.H{"perms": req.Perms, "path": req.Path, "visible": req.Visible})
	ok(c, nil)
}

func (h *H) DeleteMenu(c *gin.Context) {
	id := queryID(c, "id")
	var m models.SysMenu
	if err := h.DB.First(&m, id).Error; err != nil {
		fail(c, http.StatusNotFound, "菜单不存在")
		return
	}
	var children int64
	h.DB.Model(&models.SysMenu{}).Where("parent_id = ?", m.ID).Count(&children)
	if children > 0 {
		badReq(c, "存在 "+itoa(uint64(children))+" 个子节点，请先删除子节点")
		return
	}
	if m.Perms != "" {
		var bound int64
		h.DB.Model(&models.SysRoleMenu{}).Where("menu_id = ?", m.ID).Count(&bound)
		if bound > 0 {
			badReq(c, "该权限节点仍被 "+itoa(uint64(bound))+" 个角色引用，请先取消授权")
			return
		}
	}
	err := rbacWriteMenu(h.DB, func(tx *gorm.DB) error {
		if err := tx.Where("menu_id = ?", m.ID).Delete(&models.SysRoleMenu{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.SysMenu{}, m.ID).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除失败："+err.Error())
		return
	}
	BumpPermEpoch()
	writeAudit(c, "menu", m.ID, m.MenuName, "remove", gin.H{"perms": m.Perms}, nil)
	ok(c, nil)
}

func validateMenuSave(req menuSaveReq, isCreate bool) error {
	name := strings.TrimSpace(req.MenuName)
	if name == "" {
		return errStr("菜单名称不能为空")
	}
	switch req.MenuType {
	case models.MenuTypeDir, models.MenuTypeMenu, models.MenuTypeButton:
	default:
		return errStr("menu_type 只能是 M(目录) / C(菜单) / F(按钮)")
	}
	if req.MenuType == models.MenuTypeButton && strings.TrimSpace(req.Perms) == "" {
		return errStr("按钮必须填写权限标识 perms")
	}
	if !strings.Contains(req.Perms, "*") || req.MenuType == models.MenuTypeButton {
		if req.Perms != "" {
			parts := strings.Split(req.Perms, ":")
			if len(parts) < 3 {
				return errStr("权限标识需为 模块:资源:操作 三段式，如 system:user:add")
			}
		}
	}
	return nil
}

func (h *H) assertPermsUnique(tx *gorm.DB, perms string, excludeID uint) error {
	if perms == "" {
		return nil
	}
	var n int64
	q := tx.Model(&models.SysMenu{}).Where("perms = ?", perms)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	q.Count(&n)
	if n > 0 {
		return errStr("权限标识已存在：" + perms)
	}
	return nil
}

// ---------------------------------------------------------------- 前端权限下发

// PermRoutes 返回当前用户可见的菜单（M/C）树，供前端导航渲染与路由准入。
func (h *H) PermRoutes(c *gin.Context) {
	cl := claimsFromCtx(c)
	if cl == nil {
		fail(c, http.StatusUnauthorized, "未认证")
		return
	}
	if IsSuperAdmin(c) {
		var all []models.SysMenu
		_ = h.DB.Where("menu_type IN ?", []string{models.MenuTypeDir, models.MenuTypeMenu}).
			Order("parent_id, order_num, id").Find(&all).Error
		ok(c, gin.H{"tree": buildMenuTree(all, 0, nil), "super": true})
		return
	}
	var all []models.SysMenu
	_ = h.DB.Raw(`
		SELECT DISTINCT m.* FROM menus m
		JOIN role_menus rm ON rm.menu_id = m.id
		JOIN roles r       ON r.id = rm.role_id
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = ? AND r.status = 0 AND m.status = 0
		  AND m.menu_type IN (?, ?)
		ORDER BY m.parent_id, m.order_num, m.id`,
		cl.UserID, models.MenuTypeDir, models.MenuTypeMenu).Scan(&all).Error
	ok(c, gin.H{"tree": buildMenuTree(all, 0, nil), "super": false})
}

// PermMy 返回当前用户的权限快照（前端按钮显隐用；后端仍会独立校验）。
func (h *H) PermMy(c *gin.Context) {
	cl := claimsFromCtx(c)
	if cl == nil {
		fail(c, http.StatusUnauthorized, "未认证")
		return
	}
	perms, keys := permsOf(cl)
	scope := gin.H{"all": false, "dept_ids": []uint{}, "fallback_self": true}
	if sc, err := ScopeOf(c); err == nil {
		scope = gin.H{
			"all": sc.All, "dept_ids": sc.DeptIDs,
			"include_self": sc.IncludeSelf, "fallback_self": sc.FallbackSelf,
			"subtree_roots": sc.SubtreeRoots, "dept_id": sc.DeptID,
		}
	}
	ok(c, gin.H{
		"perms":     perms.Sorted(),
		"role_keys": keys,
		"wildcard":  perms.Has("*:*:*"),
		"scope":     scope,
		"enforce":   EnforceMode(),
	})
}

// PermConsistency 返回一致性自检报告（接口 perms ↔ 菜单 perms 双向差集）。
func (h *H) PermConsistency(c *gin.Context) {
	ok(c, Consistency())
}

// ---------------------------------------------------------------- 写包装

// rbacWriteMenu 复用 rbac 的"进程内串行 + 单事务"写包装。
func rbacWriteMenu(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return rbacWithWrite(db, fn)
}
