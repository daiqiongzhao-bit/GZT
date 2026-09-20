package system

import (
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// 启动期「接口覆盖自检」
//
// 为什么需要它：运行期 UndeclaredRoutes() 只能记录**被调用过**的接口，
// 一个从没被访问的漏声明接口永远不会暴露。而漏声明恰恰是越权的前置条件。
// 启动期扫一遍 gin 的**全量注册表**，才能给出「每个 /api 接口都有明确约束」的确定性证据。
//
// 判定口径：
//   - 路径不以 /api/ 开头 → 不参与（静态资源等）
//   - 命中 publicRoutes → 已显式声明为"公开"（不是遗漏）
//   - 命中 routePerms → 已显式声明所需权限
//   - 其余 → **漏声明**，启动时告警列出（fail-closed 生效时这些接口一律 403）
// ============================================================================

// publicRoutes 显式声明"无需登录"的公开接口。
//
// 把公开接口也写成声明，是为了消灭"未声明"这一模糊态：
// 在 routePerms 之外唯一合法的豁免就是这张表，新增公开接口必须在此登记。
// 与 main.go 的公开路由块（api := r.Group("/api") 下、AuthRequired 之前）保持一致。
var publicRoutes = map[string]bool{
	"POST /api/auth/login":       true, // 登录
	"GET /api/health":            true, // 健康检查
	"GET /api/settings":          true, // 企业信息公开可读（登录页）
	"GET /api/settings/logo":     true, // 企业 Logo（登录页）
	"GET /api/version":           true, // 版本号
	"GET /api/push/vapid-public": true, // Web Push 公钥
	"GET /api/workspace/knowledge_attachments/:key/download": true, // 知识附件（key 为不可猜测随机串）
	"GET /api/workspace/temp-attachments/:key/download":      true, // 中转附件（同上）
}

// RouteAudit 是启动期覆盖自检的结果。
type RouteAudit struct {
	Total      int      `json:"total"`      // 参与的 /api 接口总数
	Public     int      `json:"public"`     // 显式公开数
	Declared   int      `json:"declared"`   // 已声明权限数
	Undeclared []string `json:"undeclared"` // 漏声明清单（应为空）
}

// OK 表示覆盖完整（无漏声明）。
func (a RouteAudit) OK() bool { return len(a.Undeclared) == 0 }

// AuditRegisteredRoutes 对 gin 全量路由表做覆盖自检。
func AuditRegisteredRoutes(routes gin.RoutesInfo) RouteAudit {
	audit := RouteAudit{}
	for _, rt := range routes {
		if !strings.HasPrefix(rt.Path, "/api/") {
			continue
		}
		audit.Total++
		key := rt.Method + " " + rt.Path
		if publicRoutes[key] {
			audit.Public++
			continue
		}
		if _, ok := LookupPerm(rt.Method, rt.Path); ok {
			audit.Declared++
			continue
		}
		audit.Undeclared = append(audit.Undeclared, key)
	}
	sort.Strings(audit.Undeclared)
	return audit
}
