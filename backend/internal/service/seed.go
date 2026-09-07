package service

import (
	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// Seed 首次启动时初始化基础数据（仅当用户表为空、即全新空库时执行一次）。
// 只创建管理端必需的默认部门与超管账号；**不再种任何演示人员/演示班表**，
// 避免部署到正式环境后还要手动清理林晓/陈默等演示范例。已导入过数据的库不受影响。
func Seed() {
	var userCount int64
	db.DB.Model(&models.User{}).Count(&userCount)
	if userCount > 0 {
		return
	}

	// 默认部门：信息部（admin 默认归属）、客服部、运维部
	deptIT := models.Department{Name: "信息部"}
	db.DB.Create(&deptIT)
	db.DB.Create(&models.Department{Name: "客服部"})
	db.DB.Create(&models.Department{Name: "运维部"})

	// 默认超级管理员（admin / admin123），便于首次登录；must_change_pwd=true 强制首次登录即修改为强密码
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	super := models.User{
		Username:      "admin",
		PasswordHash:  string(hash),
		Name:          "系统管理员",
		Role:          models.RoleSuperAdmin,
		DeptID:        deptIT.ID,
		MustChangePwd: true,
	}
	db.DB.Create(&super)

	// 企业设置
	db.DB.Create(&models.Setting{
		ID:          1,
		CompanyName: "企业排班任务工作台",
		Slogan:      "三端同步 · 安全可控 · 无限扩展",
		Copyright:   "© 2026 企业排班任务工作台",
		Version:     config.C.AppVersion,
	})

	db.DB.Create(&models.Log{UserID: super.ID, UserName: super.Name, Action: "系统初始化"})
}
