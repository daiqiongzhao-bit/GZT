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

// realClientIP 返回真实客户端 IP。
// 部署在 Docker 端口映射（docker -p / compose ports）下，c.ClientIP() 恒为网桥网关
// 地址（如 172.18.0.1），并非真实用户 IP；若前置了反代（Cloudflare / nginx / caddy），
// 反代会透传 X-Forwarded-For（首个即原始客户端）或 X-Real-IP，优先取之，否则回退 RemoteAddr。
func realClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		xff = strings.TrimSpace(xff)
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return xff
	}
	if xri := strings.TrimSpace(c.GetHeader("X-Real-IP")); xri != "" {
		return xri
	}
	// RemoteAddr 形如 1.2.3.4:5678 或 [::1]:5678，去掉端口
	if ra := c.Request.RemoteAddr; ra != "" {
		if i := strings.LastIndex(ra, ":"); i > 0 {
			return ra[:i]
		}
		return ra
	}
	return ""
}

// addLog 记录操作日志（自动带上来源 IP 与 User-Agent；c 可为 nil）
func addLog(c *gin.Context, userID uint, userName, action string) {
	ip, ua, client := "", "", ""
	if c != nil {
		ip = realClientIP(c)
		ua = c.Request.UserAgent()
		if len(ua) > 255 {
			ua = ua[:255]
		}
		// v0.0.6：操作来源。鉴权请求用令牌里签发的 client（最准）；
		// 未鉴权请求（登录等）读前端显式头 X-Client-Type；再无则兜底 UA 嗅探。
		if cl := middleware.GetClaims(c); cl != nil && cl.Client != "" {
			client = string(cl.Client)
		} else if x := c.GetHeader("X-Client-Type"); x != "" {
			client = x
		} else {
			client = sniffClient(ua)
		}
	}
	_ = db.DB.Create(&models.Log{UserID: userID, UserName: userName, Action: action, IP: ip, UA: ua, Client: client}).Error
}

// clientLabel 将 client 枚举映射为界面可读的「来源」；空/未知返回空串由前端兜底。
// 注意：入库存原始枚举，仅展示时用此函数。
func clientLabel(client string) string {
	switch client {
	case "web":
		return "网页"
	case "pwa":
		return "PWA"
	case "extension":
		return "插件"
	}
	return ""
}

// sniffClient 兜底：GZTExt 是扩展插件自定义 UA 标记（插件暂无独立指纹时兜底用）
func sniffClient(ua string) string {
	if strings.Contains(ua, "GZTExt") {
		return "extension"
	}
	return "unknown"
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
