package handlers

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ExportUsersCSV GET /api/users/export 导出当前可见部门的人员列表为 CSV。
// 权限同 ListUsers（部门管/执行者=本部门+子孙部门，超管=全部）。
func ExportUsersCSV(c *gin.Context) {
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
	var buf bytes.Buffer
	buf.Write(csvBOM(nil))
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ID", "账号", "工号", "姓名", "手机号", "角色", "部门", "已入群", "已冻结", "创建时间"})
	for _, u := range list {
		deptName := ""
		if u.Dept != nil {
			deptName = u.Dept.Name
		}
		_ = w.Write([]string{
			strconv.Itoa(int(u.ID)),
			u.Username,
			u.EmpNo,
			u.Name,
			u.Mobile,
			roleLabel(u.Role),
			deptName,
			yesNo(u.InGroup),
			yesNo(u.Frozen),
			u.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	w.Flush()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="users_export.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
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
