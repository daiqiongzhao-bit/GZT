package service

import (
	"encoding/json"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// Seed 首次启动时初始化演示数据
func Seed() {
	var userCount int64
	db.DB.Model(&models.User{}).Count(&userCount)
	if userCount > 0 {
		return
	}

	// 默认部门
	dept := models.Department{Name: "客服部"}
	db.DB.Create(&dept)
	deptOps := models.Department{Name: "运维部"}
	db.DB.Create(&deptOps)

	// 默认超级管理员（admin / admin123），便于首次登录；生产建议登录后修改密码
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	super := models.User{
		Username:     "admin",
		PasswordHash: string(hash),
		Name:         "系统管理员",
		Role:         models.RoleSuperAdmin,
		DeptID:       dept.ID,
	}
	db.DB.Create(&super)

	// 演示班表
	today := time.Now()
	fmtDate := func(d time.Time) string { return d.Format("2006-01-02") }
	people1, _ := json.Marshal([]string{"林晓", "陈默"})
	people2, _ := json.Marshal([]string{"陈默"})
	people3, _ := json.Marshal([]string{"林晓"})
	people4, _ := json.Marshal([]string{"陈默"})
	db.DB.Create(&models.Schedule{Date: fmtDate(today), Shift: "早班", People: string(people1), DeptID: dept.ID})
	db.DB.Create(&models.Schedule{Date: fmtDate(today.AddDate(0, 0, 1)), Shift: "晚班", People: string(people2), DeptID: dept.ID})
	db.DB.Create(&models.Schedule{Date: fmtDate(today.AddDate(0, 0, 2)), Shift: "早班", People: string(people3), DeptID: dept.ID})
	db.DB.Create(&models.Schedule{Date: fmtDate(today.AddDate(0, 0, -1)), Shift: "夜班", People: string(people4), DeptID: dept.ID})

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
