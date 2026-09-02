package handlers

import (
	"encoding/csv"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
)

// ===================== 人员批量导入 =====================

// userTemplateHeaders 人员导入模板的固定列（顺序即列序）
var userTemplateHeaders = []string{"姓名*", "登录账号(可空)", "初始密码", "工号*", "手机号", "角色", "部门"}

// getUserTemplateXLSX 生成人员导入模板（含说明与示例行）
func getUserTemplateXLSX() *excelize.File {
	f := excelize.NewFile()
	sheet := "人员名单"
	f.SetSheetName("Sheet1", sheet)
	f.SetCellValue(sheet, "A1", "人员导入模板：工号即登录账号。工号/账号已存在则更新资料，不存在则新建")
	for j, h := range userTemplateHeaders {
		col, _ := excelize.CoordinatesToCellName(1+j, 2)
		f.SetCellValue(sheet, col, h)
	}
	sample := [][]string{
		{"张三", "", "Cdf123456", "A001", "13800000001", "执行者", ""},
		{"李四", "", "Cdf123456", "A002", "13800000002", "部门管理员", ""},
	}
	for i, r := range sample {
		for j, v := range r {
			col, _ := excelize.CoordinatesToCellName(1+j, 3+i)
			f.SetCellValue(sheet, col, v)
		}
	}
	f.SetCellValue(sheet, "A6", "填写说明：")
	f.SetCellValue(sheet, "A7", "1. 姓名、工号必填；登录账号 = 工号（登录账号列可留空，填了也以工号为准）。初始密码留空则默认 123456（至少 6 位）。")
	f.SetCellValue(sheet, "A8", "2. 角色填 执行者 / 部门管理员 / 超级管理员（留空默认执行者）。")
	f.SetCellValue(sheet, "A9", "3. 部门填系统内已有部门名称（留空则导入到当前操作人所在部门）。")
	f.SetCellValue(sheet, "A10", "4. 工号或登录账号已存在时按表内资料更新该员工，不会重复建号。")
	return f
}

// DownloadUserTemplateXLSX GET /templates/user-template 人员导入模板
func DownloadUserTemplateXLSX(c *gin.Context) {
	f := getUserTemplateXLSX()
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="user-template.xlsx"`)
	_ = f.Write(c.Writer)
}

// roleFromLabel 把中文角色名映射为系统角色
func roleFromLabel(s string) models.Role {
	switch strings.TrimSpace(s) {
	case "超级管理员", "super_admin", "超管":
		return models.RoleSuperAdmin
	case "部门管理员", "dept_admin", "管理员":
		return models.RoleDeptAdmin
	case "执行者", "executor", "员工", "":
		return models.RoleExecutor
	}
	return models.Role(strings.TrimSpace(s))
}

// ImportUsers POST /users/import 批量导入人员（xlsx 固定模板 / csv）
func ImportUsers(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要上传的文件"})
		return
	}
	if file.Size > 5<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件不能超过 5MB"})
		return
	}
	lower := strings.ToLower(file.Filename)
	var rows [][]string
	if strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xls") {
		rows, err = readUserXLSX(file)
	} else if strings.HasSuffix(lower, ".csv") {
		rows, err = readUserCSV(file)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .xlsx 或 .csv 文件"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cl := currentClaims(c)
	// 部门名 → ID（导入时按名称匹配）
	var depts []models.Department
	db.DB.Find(&depts)
	deptByName := map[string]uint{}
	for _, d := range depts {
		deptByName[strings.TrimSpace(d.Name)] = d.ID
	}

	const defaultPwd = "123456"
	created, updated, failed := 0, 0, 0
	var errs []string

	for i, row := range rows {
		lineNo := i + 1
		get := func(idx int) string {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}
		name := get(0)
		username := get(1)
		password := get(2)
		empNo := get(3)
		mobile := get(4)
		roleLabel := get(5)
		deptLabel := get(6)

		if name == "" {
			// 纯空行 → 跳过
			if strings.TrimSpace(strings.Join(row, "")) == "" {
				continue
			}
			failed++
			errs = append(errs, fmt.Sprintf("第%d行 姓名必填", lineNo))
			continue
		}
		// 模板尾部的「填写说明」行：只有姓名列有字、其余全空 → 视为说明行跳过
		if strings.TrimSpace(strings.Join(row[1:], "")) == "" {
			continue
		}
		// 统一规则：登录账号 = 工号。工号空则回填账号列（兼容旧模板），两者都空则报错
		if empNo == "" {
			if username != "" {
				empNo = username
			} else {
				failed++
				errs = append(errs, fmt.Sprintf("第%d行 工号必填（登录账号 = 工号）", lineNo))
				continue
			}
		}
		username = empNo
		if password == "" {
			password = defaultPwd
		}
		if len(password) < 6 {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行 密码至少 6 位", lineNo))
			continue
		}

		deptID := cl.DeptID
		if deptLabel != "" {
			if id, ok := deptByName[deptLabel]; ok {
				deptID = id
			} else {
				failed++
				errs = append(errs, fmt.Sprintf("第%d行 部门「%s」不存在", lineNo, deptLabel))
				continue
			}
		}
		if !canManageDept(c, deptID) {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行 无权在部门「%s」下建人", lineNo, deptLabel))
			continue
		}
		role := roleFromLabel(roleLabel)
		if role != models.RoleSuperAdmin && role != models.RoleDeptAdmin && role != models.RoleExecutor {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行 角色「%s」无法识别", lineNo, roleLabel))
			continue
		}
		// 部门管理员不能提拔超管
		if role == models.RoleSuperAdmin && cl.Role != models.RoleSuperAdmin {
			role = models.RoleExecutor
		}

		var u models.User
		res := db.DB.Where("username = ? OR emp_no = ?", username, username).First(&u) // 兼容历史账号≠工号的数据
		if res.Error == nil {
			// 已存在 → 按表内资料更新
			if cl.Role != models.RoleSuperAdmin && !canManageDept(c, u.DeptID) {
				failed++
				errs = append(errs, fmt.Sprintf("第%d行 无权修改其他部门人员「%s」", lineNo, name))
				continue
			}
			u.Name = name
			u.EmpNo = empNo
			u.Mobile = normalizeMobile(mobile)
			u.Role = role
			u.DeptID = deptID
			if msg := validateUserProfile(name, deptID, u.Mobile, u.ID); msg != "" {
				failed++
				errs = append(errs, fmt.Sprintf("第%d行 %s", lineNo, msg))
				continue
			}
			if err := db.DB.Save(&u).Error; err != nil {
				failed++
				errs = append(errs, fmt.Sprintf("第%d行 更新失败: %s", lineNo, err.Error()))
				continue
			}
			updated++
			notifyUser(u.ID, "user", "个人信息变更",
				fmt.Sprintf("%s 于 %s 通过批量导入更新了你的个人信息", claimsName(cl), time.Now().Format("2006-01-02 15:04")),
				cl.UserID, claimsName(cl))
			continue
		}

		if msg := validateUserProfile(name, deptID, normalizeMobile(mobile), 0); msg != "" {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行 %s", lineNo, msg))
			continue
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		nu := models.User{
			Username:     username,
			PasswordHash: string(hash),
			Name:         name,
			EmpNo:        empNo,
			Mobile:       normalizeMobile(mobile),
			Role:         role,
			DeptID:       deptID,
		}
		if err := db.DB.Create(&nu).Error; err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行 创建失败（登录账号可能重复）: %s", lineNo, username))
			continue
		}
		created++
		notifyUser(nu.ID, "user", "账号已创建",
			fmt.Sprintf("%s 于 %s 为你创建了系统账号，初始密码为「%s」，请登录后修改", claimsName(cl), time.Now().Format("2006-01-02 15:04"), password),
			cl.UserID, claimsName(cl))
	}

	if created+updated > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("批量导入人员：新建 %d 人、更新 %d 人", created, updated))
	}
	c.JSON(http.StatusOK, gin.H{
		"created": created,
		"updated": updated,
		"failed":  failed,
		"errors":  errs,
	})
}

// readUserXLSX 读取人员模板：跳过表头（前 2 行），返回数据行
func readUserXLSX(file *multipart.FileHeader) ([][]string, error) {
	fh, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	f, err := excelize.OpenReader(fh)
	if err != nil {
		return nil, fmt.Errorf("无法解析 Excel 文件")
	}
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("Excel 中没有工作表")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败")
	}
	out := make([][]string, 0, len(rows))
	for i, r := range rows {
		if i < 2 {
			continue // 第 1 行说明、第 2 行表头
		}
		// 补齐到 7 列
		for len(r) < 7 {
			r = append(r, "")
		}
		out = append(out, r[:7])
	}
	return out, nil
}

// readUserCSV 读取 CSV：首行为表头则跳过
func readUserCSV(file *multipart.FileHeader) ([][]string, error) {
	fh, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	rd := csv.NewReader(fh)
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败，请用模板下载的 xlsx")
	}
	out := make([][]string, 0, len(rows))
	for i, r := range rows {
		if i == 0 && len(r) > 0 && (strings.Contains(r[0], "姓名") || strings.Contains(r[0], "人员")) {
			continue
		}
		for len(r) < 7 {
			r = append(r, "")
		}
		out = append(out, r[:7])
	}
	return out, nil
}
