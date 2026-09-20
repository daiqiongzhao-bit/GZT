package system

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ============================================================================
// 用户管理（方案 §3）
//
// 数据权限要点：
//   - 列表：按当前登录人的数据范围自动拼 SQL（§3.3），前端传什么都不信
//   - 单对象：按 id 直取后必须做归属断言（§9.1(2)），这是最容易漏的一环
//   - 导出/下拉等"任何返回数据的接口"同样过滤（§9.3）
// ============================================================================

type userRow struct {
	ID            uint     `json:"id"`
	Username      string   `json:"username"`
	Name          string   `json:"name"`
	EmpNo         string   `json:"emp_no"`
	Mobile        string   `json:"mobile"`
	Role          string   `json:"role"`
	DeptID        uint     `json:"dept_id"`
	DeptName      string   `json:"dept_name"`
	Frozen        bool     `json:"frozen"`
	OnLeave       bool     `json:"on_leave"`
	MustChangePwd bool     `json:"must_change_pwd"`
	TokenVersion  uint     `json:"token_version"`
	LastLoginAt   string   `json:"last_login_at"`
	CreatedAt     string   `json:"created_at"`
	RoleIDs       []uint   `json:"role_ids"`
	RoleNames     []string `json:"role_names"`
}

// ListUsers 分页列出用户；**必然**按当前登录人的数据范围过滤。
func (h *H) ListUsers(c *gin.Context) {
	page := queryInt(c, "page", 1)
	size := queryInt(c, "page_size", 20)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}

	scope, err := ScopeOf(c)
	if err != nil {
		fail(c, http.StatusInternalServerError, "解析数据范围失败："+err.Error())
		return
	}

	q := h.DB.Model(&models.User{}).Scopes(scope.Filter("users.dept_id", "users.id"))
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("username LIKE ? OR name LIKE ? OR emp_no LIKE ? OR mobile LIKE ?", like, like, like, like)
	}
	if d := strings.TrimSpace(c.Query("dept_id")); d != "" {
		// 前端传的 dept_id 只作为"缩小范围"的条件，不能突破 scope（scope 已注入，无法绕过）
		q = q.Where("dept_id = ?", d)
	}
	if s := strings.TrimSpace(c.Query("frozen")); s == "0" || s == "1" {
		q = q.Where("frozen = ?", s == "1")
	}

	// ★ 分页 count 与 list 必须用同一条件，否则总数会泄漏行数（方案 §9.3）
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(c, http.StatusInternalServerError, "统计失败："+err.Error())
		return
	}

	var users []models.User
	if err := q.Order("dept_id, id").Limit(size).Offset((page - 1) * size).Find(&users).Error; err != nil {
		fail(c, http.StatusInternalServerError, "查询失败："+err.Error())
		return
	}

	deptNames := h.deptNameMap()
	roleMap := h.userRoleMap()
	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		r := userRow{
			ID: u.ID, Username: u.Username, Name: u.Name, EmpNo: u.EmpNo, Mobile: u.Mobile,
			Role: string(u.Role), DeptID: u.DeptID, DeptName: deptNames[u.DeptID],
			Frozen: u.Frozen, OnLeave: u.OnLeave, MustChangePwd: u.MustChangePwd,
			TokenVersion: u.TokenVersion, CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if u.LastLoginAt != nil {
			r.LastLoginAt = u.LastLoginAt.Format("2006-01-02 15:04:05")
		}
		if rm, ok := roleMap[u.ID]; ok {
			r.RoleIDs = rm.ids
			r.RoleNames = rm.names
		}
		rows = append(rows, r)
	}
	ok(c, gin.H{"list": rows, "total": total, "page": page, "page_size": size})
}

type roleBrief struct {
	ids   []uint
	names []string
}

func (h *H) userRoleMap() map[uint]roleBrief {
	out := map[uint]roleBrief{}
	var rows []struct {
		UserID   uint
		RoleID   uint
		RoleName string
	}
	_ = h.DB.Raw(`SELECT ur.user_id AS user_id, r.id AS role_id, r.role_name AS role_name
	              FROM user_roles ur JOIN roles r ON r.id = ur.role_id
	              ORDER BY ur.user_id, r.role_sort`).Scan(&rows).Error
	for _, r := range rows {
		b := out[r.UserID]
		b.ids = append(b.ids, r.RoleID)
		b.names = append(b.names, r.RoleName)
		out[r.UserID] = b
	}
	return out
}

func (h *H) deptNameMap() map[uint]string {
	out := map[uint]string{}
	var ds []models.Department
	_ = h.DB.Find(&ds).Error
	for _, d := range ds {
		out[d.ID] = d.Name
	}
	return out
}

// GetUser 详情：按 id 直取 → 必须做归属断言。
func (h *H) GetUser(c *gin.Context) {
	id := queryID(c, "id")
	var u models.User
	if err := h.DB.First(&u, id).Error; err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if !h.assertCanTouchUser(c, u.DeptID, u.ID, PSysUserList) {
		fail(c, http.StatusForbidden, "无权查看该用户")
		return
	}
	ok(c, gin.H{"user": u, "role_ids": h.userRoleIDs(u.ID)})
}

type userSaveReq struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	EmpNo    string `json:"emp_no"`
	Mobile   string `json:"mobile"`
	DeptID   uint   `json:"dept_id"`
	RoleIDs  []uint `json:"role_ids"`
	OnLeave  bool   `json:"on_leave"`
}

// CreateUser 新增用户：角色必填（方案 §13.4 部分采纳）。
func (h *H) CreateUser(c *gin.Context) {
	var req userSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Name = strings.TrimSpace(req.Name)

	roleIDs := toInt64(req.RoleIDs)
	if err := rbac.ValidateUserSave(rbac.UserSaveReq{
		Username: req.Username, DeptID: int64(req.DeptID), RoleIDs: roleIDs, IsCreate: true,
	}); err != nil {
		badReq(c, err.Error())
		return
	}
	// 归属断言：不能把用户建到自己的管理范围之外
	if !h.assertCanTouchUser(c, req.DeptID, 0, PSysUserAdd) {
		fail(c, http.StatusForbidden, "无权在目标部门下创建用户")
		return
	}
	// 提权防护：只能分配"权限不高于自己"的角色（垂直校验，方案 §8.3）
	if err := h.assertCanGrant(c, req.RoleIDs); err != nil {
		fail(c, http.StatusForbidden, err.Error())
		return
	}
	if !h.rolesAllExist(req.RoleIDs) {
		badReq(c, "存在无效的角色 ID")
		return
	}
	pwd := strings.TrimSpace(req.Password)
	if len(pwd) < 6 {
		badReq(c, "初始密码长度至少 6 位")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "密码加密失败")
		return
	}
	firstRole := models.RoleExecutor
	if len(req.RoleIDs) > 0 {
		if k := h.roleKeyOf(req.RoleIDs[0]); k != "" {
			firstRole = models.Role(k)
		}
	}
	u := models.User{
		Username: req.Username, PasswordHash: string(hash), Name: nvl(req.Name, req.Username),
		EmpNo: req.EmpNo, Mobile: req.Mobile, Role: firstRole, DeptID: req.DeptID,
		MustChangePwd: true, OnLeave: req.OnLeave,
	}
	err = rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		var n int64
		tx.Model(&models.User{}).Where("username = ?", u.Username).Count(&n)
		if n > 0 {
			return fmt.Errorf("登录名已存在")
		}
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		for _, rid := range req.RoleIDs {
			if err := tx.Create(&models.SysUserRole{UserID: u.ID, RoleID: rid}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(c, http.StatusBadRequest, "创建失败："+err.Error())
		return
	}
	writeAudit(c, "user", u.ID, u.Username, "create", nil,
		gin.H{"username": u.Username, "dept_id": u.DeptID, "role_ids": req.RoleIDs})
	ok(c, gin.H{"id": u.ID})
}

// UpdateUser 编辑用户。
func (h *H) UpdateUser(c *gin.Context) {
	var req userSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	if req.ID == 0 {
		badReq(c, "缺少用户 ID")
		return
	}
	var u models.User
	if err := h.DB.First(&u, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if !h.assertCanTouchUser(c, u.DeptID, u.ID, PSysUserEdit) {
		fail(c, http.StatusForbidden, "无权修改该用户")
		return
	}
	before := gin.H{"name": u.Name, "emp_no": u.EmpNo, "mobile": u.Mobile, "dept_id": u.DeptID,
		"role_ids": h.userRoleIDs(u.ID)}

	// 提权防护：改角色绑定同样受"不得授予高于自己"的垂直校验约束
	if err := h.assertCanGrant(c, req.RoleIDs); err != nil {
		fail(c, http.StatusForbidden, err.Error())
		return
	}

	if req.DeptID != 0 && req.DeptID != u.DeptID {
		// 换部门：新部门也必须在自己的写边界内
		if !h.assertCanTouchUser(c, req.DeptID, 0, PSysUserEdit) {
			fail(c, http.StatusForbidden, "无权把用户移动到该部门")
			return
		}
		u.DeptID = req.DeptID
	}
	if v := strings.TrimSpace(req.Name); v != "" {
		u.Name = v
	}
	u.EmpNo = req.EmpNo
	u.Mobile = req.Mobile
	u.OnLeave = req.OnLeave

	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("id = ?", u.ID).Updates(map[string]interface{}{
			"name": u.Name, "emp_no": u.EmpNo, "mobile": u.Mobile, "dept_id": u.DeptID, "on_leave": u.OnLeave,
		}).Error; err != nil {
			return err
		}
		if len(req.RoleIDs) > 0 {
			// 改角色绑定属于高危动作：仅超管（perms 已限制），且不允许摘掉最后一个超管
			if !h.rolesAllExist(req.RoleIDs) {
				return fmt.Errorf("存在无效的角色 ID")
			}
			if err := h.applyRoles(tx, u.ID, req.RoleIDs); err != nil {
				return err
			}
			// 双读灰度：同步一份 users.role（取排序最靠前的角色），保持旧代码路径可用
			if k := h.roleKeyOf(req.RoleIDs[0]); k != "" {
				if err := tx.Model(&models.User{}).Where("id = ?", u.ID).
					Update("role", k).Error; err != nil {
					return err
				}
			}
		}
		// 权限相关变更 → 递增该用户 token_version（旧令牌失效 + 权限缓存重建）
		return bumpUsers(tx, []uint{u.ID})
	})
	if err != nil {
		fail(c, http.StatusBadRequest, "更新失败："+err.Error())
		return
	}
	writeAudit(c, "user", u.ID, u.Username, "update", before,
		gin.H{"name": u.Name, "emp_no": u.EmpNo, "mobile": u.Mobile, "dept_id": u.DeptID,
			"role_ids": h.userRoleIDs(u.ID)})
	ok(c, gin.H{"id": u.ID})
}

// DeleteUser 删除用户：内置保护 + 归属断言 + 最后一个超管断言（三入口之一）。
func (h *H) DeleteUser(c *gin.Context) {
	id := queryID(c, "id")
	var u models.User
	if err := h.DB.First(&u, id).Error; err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if !h.assertCanTouchUser(c, u.DeptID, u.ID, PSysUserRemove) {
		fail(c, http.StatusForbidden, "无权删除该用户")
		return
	}
	if cl := claimsFromCtx(c); cl != nil && cl.UserID == u.ID {
		badReq(c, "不能删除当前登录账号")
		return
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		// ★ 入口 1/3：删除用户
		if h.isSuperAdminUser(tx, u.ID) {
			if err := rbac.AssertNotLastSuperAdmin(tx, int64(u.ID)); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ?", u.ID).Delete(&models.SysUserRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.User{}, u.ID).Error
	})
	if err != nil {
		fail(c, http.StatusBadRequest, "删除失败："+err.Error())
		return
	}
	InvalidatePerm(u.ID)
	writeAudit(c, "user", u.ID, u.Username, "remove", gin.H{"username": u.Username, "dept_id": u.DeptID}, nil)
	ok(c, nil)
}

// ResetPwd 重置密码：随机密码 + must_change_pwd=1 + 递增 token_version（踢下线）。
func (h *H) ResetPwd(c *gin.Context) {
	var req struct {
		ID       uint   `json:"id"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var u models.User
	if err := h.DB.First(&u, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if !h.assertCanTouchUser(c, u.DeptID, u.ID, PSysUserResetPwd) {
		fail(c, http.StatusForbidden, "无权重置该用户密码")
		return
	}
	pwd := strings.TrimSpace(req.Password)
	if pwd == "" {
		pwd = randomPassword(10)
	}
	if len(pwd) < 6 {
		badReq(c, "密码长度至少 6 位")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "密码加密失败")
		return
	}
	err = rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("id = ?", u.ID).Updates(map[string]interface{}{
			"password_hash": string(hash), "must_change_pwd": true,
		}).Error; err != nil {
			return err
		}
		return bumpUsers(tx, []uint{u.ID})
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "重置失败："+err.Error())
		return
	}
	writeAudit(c, "user", u.ID, u.Username, "reset_pwd", nil, gin.H{"must_change_pwd": true})
	ok(c, gin.H{"password": pwd, "must_change_pwd": true})
}

// ChangeStatus 启用/禁用用户（= frozen）。禁用即踢下线。
func (h *H) ChangeStatus(c *gin.Context) {
	var req struct {
		ID     uint `json:"id"`
		Frozen bool `json:"frozen"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var u models.User
	if err := h.DB.First(&u, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if !h.assertCanTouchUser(c, u.DeptID, u.ID, PSysUserEdit) {
		fail(c, http.StatusForbidden, "无权修改该用户状态")
		return
	}
	if cl := claimsFromCtx(c); cl != nil && cl.UserID == u.ID && req.Frozen {
		badReq(c, "不能禁用当前登录账号")
		return
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		// ★ 入口 2/3：停用用户
		if req.Frozen && h.isSuperAdminUser(tx, u.ID) {
			if err := rbac.AssertNotLastSuperAdmin(tx, int64(u.ID)); err != nil {
				return err
			}
		}
		return tx.Model(&models.User{}).Where("id = ?", u.ID).
			Update("frozen", req.Frozen).Error
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	_ = bumpUsers(h.DB, []uint{u.ID}) // 状态变更即令旧令牌失效
	writeAudit(c, "user", u.ID, u.Username, "change_status",
		gin.H{"frozen": u.Frozen}, gin.H{"frozen": req.Frozen})
	ok(c, nil)
}

// AuthRole 分配角色（多选）。
//
// 权限：`system:user:authRole`（超管与部门管理员均持有，因为它是前缀授权的一部分）。
// 因此真正的安全边界不在权限点，而在**垂直校验** —— 见 assertCanGrant。还要叠加
// 归属断言（目标用户须在自己的数据范围内）与"最后一个超管"保护，三者缺一不可。
func (h *H) AuthRole(c *gin.Context) {
	var req struct {
		ID      uint   `json:"id"`
		RoleIDs []uint `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var u models.User
	if err := h.DB.First(&u, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if !h.assertCanTouchUser(c, u.DeptID, u.ID, PSysUserAuthRole) {
		fail(c, http.StatusForbidden, "无权修改该用户角色")
		return
	}
	if len(req.RoleIDs) == 0 {
		badReq(c, "请至少为用户分配 1 个角色")
		return
	}
	if !h.rolesAllExist(req.RoleIDs) {
		badReq(c, "存在无效的角色 ID")
		return
	}
	// 提权防护（防御纵深）：内置部门管理员已被 DenyExact 排除 authRole，
	// 但自定义角色可能持有它。此时若不校验"待授角色是否在自己可分配范围内"，
	// 持有者就能给下属授予超级管理员。assertCanGrant 对超管直接放行。
	if err := h.assertCanGrant(c, req.RoleIDs); err != nil {
		fail(c, http.StatusForbidden, err.Error())
		return
	}
	before := gin.H{"role_ids": h.userRoleIDs(u.ID)}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		// ★ 入口 3/3：解绑角色（若操作后不再有超管身份则拒绝）
		if h.isSuperAdminUser(tx, u.ID) && !h.roleIDsIncludeSuper(tx, req.RoleIDs) {
			if err := rbac.AssertNotLastSuperAdmin(tx, int64(u.ID)); err != nil {
				return err
			}
		}
		if err := h.applyRoles(tx, u.ID, req.RoleIDs); err != nil {
			return err
		}
		if k := h.roleKeyOf(req.RoleIDs[0]); k != "" {
			if err := tx.Model(&models.User{}).Where("id = ?", u.ID).Update("role", k).Error; err != nil {
				return err
			}
		}
		return bumpUsers(tx, []uint{u.ID})
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	writeAudit(c, "user", u.ID, u.Username, "assign_role", before, gin.H{"role_ids": req.RoleIDs})
	ok(c, nil)
}

// ---------------------------------------------------------------- 内部工具

func (h *H) applyRoles(tx *gorm.DB, userID uint, roleIDs []uint) error {
	if err := tx.Where("user_id = ?", userID).Delete(&models.SysUserRole{}).Error; err != nil {
		return err
	}
	for _, rid := range roleIDs {
		if err := tx.Create(&models.SysUserRole{UserID: userID, RoleID: rid}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *H) rolesAllExist(ids []uint) bool {
	if len(ids) == 0 {
		return false
	}
	var n int64
	h.DB.Model(&models.SysRole{}).Where("id IN ?", ids).Count(&n)
	return int(n) == len(ids)
}

func (h *H) roleKeyOf(id uint) string {
	var r models.SysRole
	if err := h.DB.Select("role_key").First(&r, id).Error; err != nil {
		return ""
	}
	return r.RoleKey
}

func (h *H) isSuperAdminUser(tx *gorm.DB, userID uint) bool {
	var n int64
	tx.Raw(`SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id = ur.role_id
	        WHERE ur.user_id = ? AND r.role_key = ?`, userID, models.RoleKeySuperAdmin).Scan(&n)
	return n > 0
}

func (h *H) roleIDsIncludeSuper(tx *gorm.DB, ids []uint) bool {
	if len(ids) == 0 {
		return false
	}
	var n int64
	tx.Raw(`SELECT COUNT(*) FROM roles WHERE id IN ? AND role_key = ?`,
		ids, models.RoleKeySuperAdmin).Scan(&n)
	return n > 0
}

func toInt64(ids []uint) []int64 {
	out := make([]int64, 0, len(ids))
	for _, x := range ids {
		out = append(out, int64(x))
	}
	return out
}

func nvl(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

const pwdAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789@#%"

func randomPassword(n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(pwdAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			out[i] = pwdAlphabet[i%len(pwdAlphabet)]
			continue
		}
		out[i] = pwdAlphabet[v.Int64()]
	}
	return string(out)
}
