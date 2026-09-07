package handlers

import (
	"net/http"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// ExportUsersXLSX GET /api/users/export 导出当前可见部门的人员为 Excel(.xlsx)。
// 权限同 ListUsers（部门管/执行者=本部门+子孙部门，超管=全部）。
//
// v0.8.0 起改为与「人员导入模板」同构的 xlsx：前 7 列与导入模板完全一致
// （姓名/登录账号/初始密码/工号/手机号/角色/部门），因此导出结果可直接在
// 另一台服务器上再次导入。其中「初始密码」列统一留空：
//   - 导入到空系统 → 全部按新员工创建，密码使用默认初始密码（导入模板说明中有写）；
//   - 导入到已有账号的系统 → 仅更新资料，不会改动原密码。
func ExportUsersXLSX(c *gin.Context) {
	scope := deptScopeIDs(c)
	q := db.DB.Preload("Dept").Order("id asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var list []models.User
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	f := excelize.NewFile()
	sheet := "人员名单"
	f.SetSheetName("Sheet1", sheet)
	f.SetCellValue(sheet, "A1", "人员名单导出（与导入模板同构）：初始密码列留空；在另一台服务器导入时请自行填写新的初始密码或留空使用默认密码")
	headers := []string{"姓名*", "登录账号(可空)", "初始密码", "工号*", "手机号", "角色", "部门", "已入群", "已冻结", "创建时间"}
	for j, h := range headers {
		col, _ := excelize.CoordinatesToCellName(1+j, 2)
		f.SetCellValue(sheet, col, h)
	}
	widths := []float64{12, 16, 12, 12, 14, 14, 18, 10, 10, 20}
	for j, w := range widths {
		name, _ := excelize.ColumnNumberToName(j + 1)
		f.SetColWidth(sheet, name, name, w)
	}
	for i, u := range list {
		deptName := ""
		if u.Dept != nil {
			deptName = u.Dept.Name
		}
		row := []interface{}{
			u.Name,
			u.Username,
			"", // 初始密码留空：已有账号导入不改密码；新账号使用默认初始密码
			u.EmpNo,
			u.Mobile,
			roleLabel(u.Role),
			deptName,
			yesNo(u.InGroup),
			yesNo(u.Frozen),
			u.CreatedAt.Format("2006-01-02 15:04"),
		}
		for j, v := range row {
			col, _ := excelize.CoordinatesToCellName(1+j, 3+i)
			f.SetCellValue(sheet, col, v)
		}
	}
	fname := "users_export_" + time.Now().Format("20060102_1504") + ".xlsx"
	writeXLSX(c, f, fname)
}

// roleLabel 角色可读文案
func roleLabel(r models.Role) string {
	switch r {
	case models.RoleSuperAdmin:
		return "超级管理员"
	case models.RoleDeptAdmin:
		return "部门管理员"
	case models.RoleExecutor:
		return "执行者"
	}
	return string(r)
}

// yesNo 布尔转 是/否
func yesNo(b bool) string {
	if b {
		return "是"
	}
	return "否"
}
