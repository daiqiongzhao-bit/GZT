package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/session"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ListDepartments 部门列表（超管看全部，部门管/执行者仅看本部门及子孙部门）
func ListDepartments(c *gin.Context) {
	scope := deptScopeIDs(c)
	var list []models.Department
	q := db.DB.Order("id asc")
	if len(scope) > 0 {
		q = q.Where("id IN ?", scope)
	}
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// CreateDepartment 新建部门（仅超管）
func CreateDepartment(c *gin.Context) {
	var d models.Department
	if err := c.ShouldBindJSON(&d); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if d.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "部门名称不能为空"})
		return
	}
	// 上级部门必须存在（parent_id 为 0 表示顶级部门）
	if d.ParentID != 0 {
		var parent models.Department
		if err := db.DB.First(&parent, d.ParentID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "上级部门不存在"})
			return
		}
	}
	if err := db.DB.Create(&d).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "部门名称已存在或创建失败"})
		return
	}
	// 新部门自动生成默认班次（早班/晚班），部门管理员可按实际调整
	db.DB.Create(&models.ShiftConfig{DeptID: d.ID, Name: "早班", StartTime: "09:00", EndTime: "18:00"})
	db.DB.Create(&models.ShiftConfig{DeptID: d.ID, Name: "晚班", StartTime: "13:30", EndTime: "22:00"})
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "创建部门: "+d.Name)
	c.JSON(http.StatusOK, d)
}

// DeleteDepartment 删除部门（仅超管；有子部门或有用户时拒绝）
func DeleteDepartment(c *gin.Context) {
	id := c.Param("id")
	var d models.Department
	if err := db.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "部门不存在"})
		return
	}
	var childCount int64
	db.DB.Model(&models.Department{}).Where("parent_id = ?", id).Count(&childCount)
	if childCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该部门下有子部门，请先删除或转移子部门"})
		return
	}
	var userCount int64
	db.DB.Model(&models.User{}).Where("dept_id = ?", id).Count(&userCount)
	if userCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该部门下还有人员，请先转移或删除人员"})
		return
	}
	if err := db.DB.Delete(&models.Department{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	db.DB.Where("dept_id = ?", id).Delete(&models.ShiftConfig{})
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "删除部门: "+id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListUsers 用户列表（部门隔离：本部门 + 子孙部门）
func ListUsers(c *gin.Context) {
	scope := deptScopeIDs(c)
	var list []models.User
	q := db.DB.Preload("Dept").Order("id asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// mobileRe 大陆手机号：1 开头 + 11 位
var mobileRe = regexp.MustCompile(`^1\d{10}$`)

// validateUserProfile 用户资料规则校验，返回错误信息（空串=通过）：
//  1. 同部门禁重名（不同部门允许同名，部门数据隔离不冲突）
//  2. 手机号强制 11 位 1 开头（手机号唯一用途是企业微信 @，填错不如不填）
// excludeUserID 用于编辑时排除本人
func validateUserProfile(name string, deptID uint, mobile string, excludeUserID uint) string {
	name = strings.TrimSpace(name)
	var cnt int64
	q := db.DB.Model(&models.User{}).Where("name = ? AND dept_id = ?", name, deptID)
	if excludeUserID > 0 {
		q = q.Where("id <> ?", excludeUserID)
	}
	q.Count(&cnt)
	if cnt > 0 {
		return "该部门下已有同名员工，请修改姓名"
	}
	if mobile != "" && !mobileRe.MatchString(mobile) {
		return "手机号须为 11 位数字（1 开头）"
	}
	return ""
}

type userReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	EmpNo    string `json:"emp_no"`
	Mobile   string `json:"mobile"`
	Role     string `json:"role"`
	DeptID   uint   `json:"dept_id"`
	InGroup  *bool  `json:"in_group"`
}

// CreateUser 新建用户（超管/部门管理员可建本部门人员）
func CreateUser(c *gin.Context) {
	var req userReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Password == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "姓名、密码均必填"})
		return
	}
	cl := currentClaims(c)
	// 部门管理员不能提拔超管（与导入逻辑一致）
	if req.Role == string(models.RoleSuperAdmin) && cl.Role != models.RoleSuperAdmin {
		req.Role = string(models.RoleExecutor)
	}
	// 统一规则：登录账号 = 工号。超管可无工号（系统内置 admin），需单独填账号；其余必须工号
	if req.Role == string(models.RoleSuperAdmin) {
		if req.Username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "超级管理员需填写登录账号"})
			return
		}
	} else {
		if req.EmpNo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "工号必填（登录账号将自动设为工号）"})
			return
		}
		req.Username = req.EmpNo
	}
	deptID := req.DeptID
	if !canManageDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权在该部门下创建用户"})
		return
	}
	if msg := validateUserProfile(req.Name, deptID, normalizeMobile(req.Mobile), 0); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	user := models.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Name:         req.Name,
		EmpNo:        req.EmpNo,
		Mobile:       normalizeMobile(req.Mobile),
		Role:         models.Role(req.Role),
		DeptID:       deptID,
	}
	if req.InGroup != nil {
		user.InGroup = *req.InGroup
	}
	if req.InGroup != nil {
		user.InGroup = *req.InGroup
	}
	if user.Role == "" {
		user.Role = models.RoleExecutor
	}
	if err := db.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名可能已存在"})
		return
	}
	addLog(c, cl.UserID, cl.Username, "创建用户: "+user.Name)
	c.JSON(http.StatusOK, user)
}

// DeleteUser 删除用户
func DeleteUser(c *gin.Context) {
	id := c.Param("id")
	cl := currentClaims(c)
	// 部门管理员只能删本部门及子孙部门内的人员
	if cl.Role != models.RoleSuperAdmin {
		var u models.User
		if db.DB.First(&u, id).Error == nil && !canManageDept(c, u.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除其他部门用户"})
			return
		}
	}
	if err := db.DB.Delete(&models.User{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "删除用户: "+id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type userUpdateReq struct {
	Name   string  `json:"name"`
	EmpNo  *string `json:"emp_no"` // 指针：不传则不改，传 null 表示清空
	Mobile string  `json:"mobile"`
	Role   string  `json:"role"`
	DeptID uint    `json:"dept_id"`
	Frozen *bool   `json:"frozen"`
	InGroup *bool  `json:"in_group"`
}

// UpdateUser 编辑用户（姓名/角色/部门/冻结状态）
func UpdateUser(c *gin.Context) {
	id := c.Param("id")
	cl := currentClaims(c)
	var u models.User
	if err := db.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	// 部门隔离：部门管理员只能改本部门及子孙部门；且不能操作超管
	if cl.Role != models.RoleSuperAdmin {
		if !canManageDept(c, u.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权操作其他部门用户"})
			return
		}
	}
	if u.Role == models.RoleSuperAdmin && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作超级管理员"})
		return
	}
	var req userUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	// 超管可指定任意部门；部门管仅本部门及子孙部门
	if cl.Role != models.RoleSuperAdmin {
		if req.DeptID > 0 && !canManageDept(c, req.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权把用户移到该部门"})
			return
		}
	}
	oldName, oldEmpNo, oldMobile := u.Name, u.EmpNo, u.Mobile
	oldRole, oldDept, oldFrozen := u.Role, u.DeptID, u.Frozen
	if req.Name != "" {
		u.Name = req.Name
	}
	if req.EmpNo != nil {
		if *req.EmpNo == "" && u.Role != models.RoleSuperAdmin {
			c.JSON(http.StatusBadRequest, gin.H{"error": "工号不能为空（登录账号 = 工号）"})
			return
		}
		if *req.EmpNo != u.EmpNo {
			u.EmpNo = *req.EmpNo
			// 账号 = 工号：改工号即改登录账号，令旧 token 失效强制重新登录
			if u.Username != *req.EmpNo {
				u.Username = *req.EmpNo
				db.DB.Model(&u).Update("token_version", gorm.Expr("token_version + 1"))
				session.RemoveByUser(u.ID)
			}
		}
	}
	if req.Mobile != "" {
		u.Mobile = normalizeMobile(req.Mobile)
	}
	if req.Role != "" {
		u.Role = models.Role(req.Role)
	}
	if req.DeptID > 0 {
		u.DeptID = req.DeptID
	}
	if req.Frozen != nil {
		u.Frozen = *req.Frozen
		// 冻结时令其所有已签发 token 失效，并清除在线会话
		if *req.Frozen {
			db.DB.Model(&u).Update("token_version", gorm.Expr("token_version + 1"))
			session.RemoveByUser(u.ID)
		}
	}
	if req.InGroup != nil {
		u.InGroup = *req.InGroup
	}
	if req.InGroup != nil {
		u.InGroup = *req.InGroup
	}
	// 同部门禁重名 + 手机号 11 位（编辑后按最新姓名/部门/手机号校验）
	if msg := validateUserProfile(u.Name, u.DeptID, u.Mobile, u.ID); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	if err := db.DB.Save(&u).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "编辑用户: "+u.Name)
	// 站内通知：管理员改了我的信息（改自己不发）
	var changes []string
	if u.Name != oldName {
		changes = append(changes, "姓名")
	}
	if u.EmpNo != oldEmpNo {
		changes = append(changes, "工号")
	}
	if u.Mobile != oldMobile {
		changes = append(changes, "手机号")
	}
	if u.Role != oldRole {
		changes = append(changes, "角色")
	}
	if u.DeptID != oldDept {
		changes = append(changes, "部门")
	}
	if req.Frozen != nil && u.Frozen != oldFrozen {
		if u.Frozen {
			changes = append(changes, "冻结状态（已冻结）")
		} else {
			changes = append(changes, "冻结状态（已解冻）")
		}
	}
	if len(changes) > 0 {
		notifyUser(u.ID, "user", "个人信息变更",
			fmt.Sprintf("%s 于 %s 修改了你的%s", claimsName(cl), time.Now().Format("2006-01-02 15:04"), strings.Join(changes, "、")),
			cl.UserID, claimsName(cl))
	}
	c.JSON(http.StatusOK, u)
}

// ResetPassword 重置用户密码（超管/部门管可重置本部门）
func ResetPassword(c *gin.Context) {
	id := c.Param("id")
	cl := currentClaims(c)
	var u models.User
	if err := db.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	if cl.Role != models.RoleSuperAdmin {
		if !canManageDept(c, u.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权操作其他部门用户"})
			return
		}
		if u.Role == models.RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权操作超级管理员"})
			return
		}
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能为空"})
		return
	}
	if !validPassword(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少 6 位"})
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	// 重置密码后让旧 token 全部失效
	if err := db.DB.Model(&u).Updates(map[string]interface{}{
		"password_hash": string(hash),
		"token_version": gorm.Expr("token_version + 1"),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "重置用户密码: "+u.Name)
	// 站内通知：密码被管理员重置（自己重置自己不发）
	notifyUser(u.ID, "password", "密码已重置",
		fmt.Sprintf("%s 于 %s 重置了你的登录密码，如非本人操作请联系管理员", claimsName(cl), time.Now().Format("2006-01-02 15:04")),
		cl.UserID, claimsName(cl))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
