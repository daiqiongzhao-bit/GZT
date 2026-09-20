package system

import (
	"net/http"
	"strconv"
	"strings"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// H 是系统管理（RBAC）模块的 handler 集合。
// 依赖以 *gorm.DB 注入，不 import internal/db 之外的内部包，避免导入环（方案 §7.3）。
type H struct {
	DB *gorm.DB
}

func New(gdb *gorm.DB) *H {
	if gdb == nil {
		gdb = db.DB
	}
	return &H{DB: gdb}
}

// ---------------------------------------------------------------- 写包装

// rbacWithWrite 复用 rbac 的"进程内串行 + 单事务"写包装（方案 §10.5）。
func rbacWithWrite(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return rbac.WithWrite(db, fn)
}

// ---------------------------------------------------------------- 响应助手

func ok(c *gin.Context, data interface{}) {
	if data == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusOK, data)
}

func fail(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

func badReq(c *gin.Context, msg string) { fail(c, http.StatusBadRequest, msg) }

// ---------------------------------------------------------------- 参数助手

func queryID(c *gin.Context, key string) uint {
	v, _ := strconv.ParseUint(strings.TrimSpace(c.Param(key)), 10, 32)
	return uint(v)
}

func queryInt(c *gin.Context, key string, def int) int {
	s := strings.TrimSpace(c.Query(key))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// ---------------------------------------------------------------- 缓存/会话失效

// bumpRoleUsers 递增"绑定了该角色"的所有用户的 token_version（方案 §10.3）。
// 一套机制同时完成"踢下线 + 权限缓存失效 + 重建"，零新增基础设施。
func bumpRoleUsers(tx *gorm.DB, roleID uint) (int64, error) {
	res := tx.Exec(`
		UPDATE users SET token_version = token_version + 1
		WHERE id IN (SELECT user_id FROM user_roles WHERE role_id = ?)`, roleID)
	if res.Error != nil {
		return 0, res.Error
	}
	// 精确失效进程内权限缓存
	var ids []uint
	_ = tx.Raw(`SELECT user_id FROM user_roles WHERE role_id = ?`, roleID).Scan(&ids).Error
	for _, id := range ids {
		InvalidatePerm(id)
	}
	return res.RowsAffected, nil
}

// bumpUsers 递增指定用户的 token_version。
func bumpUsers(tx *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Exec(`UPDATE users SET token_version = token_version + 1 WHERE id IN ?`, ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		InvalidatePerm(id)
	}
	return nil
}

// ---------------------------------------------------------------- 归属断言（写操作必备，方案 §9.1(2)）

// assertCanTouchUser 判断目标用户是否落在当前操作者的写边界内。
// 注意用的是"写边界"（在持有相应权限的角色子集内取最宽），不是"读范围"。
func (h *H) assertCanTouchUser(c *gin.Context, targetDeptID uint, targetUserID uint, perm string) bool {
	if IsSuperAdmin(c) || PermsOf(c).Has(rbac.WildcardPerm) {
		return true
	}
	cl := claimsFromCtx(c)
	if cl == nil {
		return false
	}
	sc, err := rbac.ResolveWriteForUser(h.DB, int64(cl.UserID), int64(cl.DeptID), perm)
	if err != nil {
		return false
	}
	return sc.ContainsTarget(int64(targetDeptID), int64(targetUserID))
}

// userRoleIDs 取用户的角色 id 列表。
func (h *H) userRoleIDs(userID uint) []uint {
	var ids []uint
	_ = h.DB.Raw(`SELECT role_id FROM user_roles WHERE user_id = ? ORDER BY role_id`, userID).Scan(&ids).Error
	return ids
}

// deptExists 部门是否存在（含停用；停用部门仍可用于存量数据）。
func (h *H) deptExists(id uint) bool {
	if id == 0 {
		return false
	}
	var n int64
	h.DB.Model(&models.Department{}).Where("id = ?", id).Count(&n)
	return n > 0
}
