package handlers

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// deptScope 返回当前用户可见的部门 ID；0 表示超管可见全部
// （保留兼容：仅超管或无 claim 时返回 0，其余返回本人部门 ID）
func deptScope(c *gin.Context) uint {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return 0
	}
	if cl.Role == models.RoleSuperAdmin {
		return 0
	}
	return cl.DeptID
}

// deptScopeIDs 返回当前用户可见的部门 ID 集合（含子孙）；nil 表示超管可见全部。
// 部门管理员/执行者 = 本部门 + 全部子孙部门。
func deptScopeIDs(c *gin.Context) []uint {
	cl := middleware.GetClaims(c)
	if cl == nil || cl.Role == models.RoleSuperAdmin {
		return nil
	}
	return descendantDeptIDs(cl.DeptID)
}

// managedDeptIDs 返回当前用户可管理的部门 ID 集合；nil 表示超管可管理全部。
// 非超管只能管理本部门及其子孙部门。
func managedDeptIDs(c *gin.Context) []uint {
	cl := middleware.GetClaims(c)
	if cl == nil || cl.Role == models.RoleSuperAdmin {
		return nil
	}
	return descendantDeptIDs(cl.DeptID)
}

// canManageDept 判断当前用户能否管理目标部门（超管任意，其余须在本部门或子孙部门内）
func canManageDept(c *gin.Context, deptID uint) bool {
	ids := managedDeptIDs(c)
	if ids == nil {
		return true
	}
	for _, id := range ids {
		if id == deptID {
			return true
		}
	}
	return false
}

// descendantDeptIDs 返回 deptID 及其全部子孙部门 ID（含自身）。部门数量小，全量查询建树。
func descendantDeptIDs(deptID uint) []uint {
	var all []models.Department
	db.DB.Find(&all)
	children := map[uint][]uint{}
	for _, d := range all {
		children[d.ParentID] = append(children[d.ParentID], d.ID)
	}
	out := []uint{deptID}
	queue := []uint{deptID}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, id := range children[p] {
			out = append(out, id)
			queue = append(queue, id)
		}
	}
	return out
}

// parseDeptID 从 multipart 表单读取 dept_id（导入类接口用）：
// 未传/非法/无权时回退为当前用户部门；超管可指定任意部门。
func parseDeptID(c *gin.Context) uint {
	cl := middleware.GetClaims(c)
	if cl == nil {
		return 0
	}
	s := strings.TrimSpace(c.PostForm("dept_id"))
	if s == "" {
		return cl.DeptID
	}
	id, err := strconv.ParseUint(s, 10, 32)
	if err != nil || id == 0 || !canManageDept(c, uint(id)) {
		return cl.DeptID
	}
	return uint(id)
}

// containsUint 判断切片是否包含指定值
func containsUint(list []uint, v uint) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// currentClaims 便捷获取
func currentClaims(c *gin.Context) *models.Claims {
	return middleware.GetClaims(c)
}

// addLog 记录操作日志（自动带上来源 IP 与 User-Agent；c 可为 nil）
func addLog(c *gin.Context, userID uint, userName, action string) {
	ip, ua := "", ""
	if c != nil {
		ip = c.ClientIP()
		ua = c.Request.UserAgent()
		if len(ua) > 255 {
			ua = ua[:255]
		}
	}
	_ = db.DB.Create(&models.Log{UserID: userID, UserName: userName, Action: action, IP: ip, UA: ua}).Error
}

// normalizeMobile 归一化手机号：去掉空格、横线、括号等分隔符，只保留数字。
// 兼容 +86 / 0086 国际区号前缀（自动剥离），保证企业微信 @ 能按纯数字匹配。
// 例：139 0000 0001 -> 13900000001；+86 139 0000 0001 -> 13900000001
func normalizeMobile(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) == 13 && strings.HasPrefix(out, "86") {
		out = out[2:] // +86 139 7604 1855
	}
	if len(out) == 15 && strings.HasPrefix(out, "0086") {
		out = out[4:] // 0086-13900000001
	}
	return out
}

// unknownScheduleNames 返回班表中「没有系统账号」的人名（按姓名匹配 users）。
// 系统按姓名查手机号做推送 @，无账号的人不会收到任何提醒，导入时务必提示。
func unknownScheduleNames(names map[string]bool) []string {
	if len(names) == 0 {
		return nil
	}
	var users []models.User
	db.DB.Select("name").Find(&users)
	has := map[string]bool{}
	for _, u := range users {
		has[u.Name] = true
	}
	var out []string
	for n := range names {
		if !has[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
