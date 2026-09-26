package openapi

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/system"
)

var reParam = regexp.MustCompile(`:([^/]+)`)

// toOASPath 把 Gin 路由（:id）转换成 OpenAPI 风格（{id}）
func toOASPath(p string) string {
	return reParam.ReplaceAllString(p, "{$1}")
}

// moduleOf 根据路径前缀推断业务模块（用于分组与标签）
func moduleOf(p string) string {
	switch {
	case strings.HasPrefix(p, "/api/wecom-push"), strings.HasPrefix(p, "/wecom"):
		return "企微推送"
	case strings.HasPrefix(p, "/workspace/knowledge"), strings.HasPrefix(p, "/api/knowledge"):
		return "知识库"
	case strings.HasPrefix(p, "/api/schedules"):
		return "排班管理"
	case strings.HasPrefix(p, "/api/tasks"):
		return "任务管理"
	case strings.HasPrefix(p, "/api/users"), strings.HasPrefix(p, "/api/roles"),
		strings.HasPrefix(p, "/api/departments"), strings.HasPrefix(p, "/api/sessions"):
		return "用户与权限"
	case strings.HasPrefix(p, "/api/system-logs"), strings.HasPrefix(p, "/api/backups"),
		strings.HasPrefix(p, "/api/settings"), strings.HasPrefix(p, "/api/rbac"):
		return "系统管理"
	case strings.HasPrefix(p, "/api/worklogs"), strings.HasPrefix(p, "/workspace/worklog"):
		return "工作日志"
	case strings.HasPrefix(p, "/api/auth-logs"), strings.HasPrefix(p, "/api/auth"),
		strings.HasPrefix(p, "/api/me"):
		return "认证与审计"
	case strings.HasPrefix(p, "/api/dashboard"):
		return "工作台看板"
	case strings.HasPrefix(p, "/api/push"):
		return "Web 推送"
	case strings.HasPrefix(p, "/api/broadcast"), strings.HasPrefix(p, "/api/notifications"):
		return "通知与广播"
	case strings.HasPrefix(p, "/api/shift"):
		return "排班引擎"
	default:
		return "其他"
	}
}

func verbOf(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return "查询"
	case "POST":
		return "创建/提交"
	case "PUT":
		return "更新"
	case "DELETE":
		return "删除"
	case "PATCH":
		return "局部更新"
	default:
		return strings.ToUpper(method)
	}
}

// Spec 生成 OpenAPI 3.0 文档结构。paths 覆盖 routeperm 中声明的全量接口，
// 每个 operation 附带 x-permission 扩展字段标注所需权限（@auth 表示登录即可）。
func Spec() map[string]interface{} {
	perms := system.AllRoutePerms()

	ver := "unknown"
	if config.C != nil {
		ver = config.C.AppVersion
	}

	// 按 path 聚合 method -> perm
	type op struct {
		method string
		perm   string
	}
	byPath := map[string][]op{}
	paths := []string{}
	for key, perm := range perms {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			continue
		}
		method, p := strings.ToUpper(parts[0]), parts[1]
		oasPath := toOASPath(p)
		if _, ok := byPath[oasPath]; !ok {
			paths = append(paths, oasPath)
		}
		byPath[oasPath] = append(byPath[oasPath], op{method, perm})
	}
	sort.Strings(paths)

	pathsObj := map[string]interface{}{}
	for _, p := range paths {
		methods := byPath[p]
		pathItem := map[string]interface{}{}
		for _, o := range methods {
			mod := moduleOf(p)
			summary := fmt.Sprintf("%s · %s", mod, verbOf(o.method))
			operation := map[string]interface{}{
				"tags":        []string{mod},
				"summary":     summary,
				"operationId": fmt.Sprintf("%s_%s", strings.ToLower(o.method), strings.NewReplacer("/", "_", "{", "", "}", "").Replace(p)),
				"responses": map[string]interface{}{
					"200": map[string]interface{}{"description": "成功"},
					"401": map[string]interface{}{"description": "未登录"},
					"403": map[string]interface{}{"description": "权限不足"},
				},
				"x-permission": o.perm,
			}
			pathItem[strings.ToLower(o.method)] = operation
		}
		pathsObj[p] = pathItem
	}

	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "GZT 内部工作台 API",
			"version":     ver,
			"description": "企业内部排班 / 任务 / 通知 / 知识库 / 交接工作台。鉴权方式：登录后由服务端写入 Cookie（token），或请求头 Authorization: Bearer <token>。所有接口（公开项除外）均需在 routeperm 中声明权限，未声明默认拒绝（fail-closed）。",
		},
		"servers": []map[string]interface{}{
			{"url": "/api", "description": "相对根路径"},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"cookieAuth": map[string]interface{}{"type": "apiKey", "in": "cookie", "name": "token"},
				"bearerAuth": map[string]interface{}{"type": "http", "scheme": "bearer"},
			},
		},
		"security": []map[string]interface{}{{"cookieAuth": []string{}}},
		"paths":   pathsObj,
	}
}
