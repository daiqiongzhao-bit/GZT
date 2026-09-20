package system

import (
	"encoding/json"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// 权限变更审计（方案 §11.1）
//
// 复用 GZT 现成的 system_logs（source='rbac'，detail 存 JSON），不新建表。
// 必须落审计的四类操作：
//   1. 角色分配（user_roles 变更）  2. 密码重置
//   3. 数据范围变更（data_scope / role_depts）  4. 角色启停与删除
//
// 约定：before 必须真实回查取得（只记 after 等于无法追责）；
//       审计写入失败不得阻断主流程，只记 warn。
// ============================================================================

// AuditDetail 是落到 system_logs.detail 的 JSON 结构。
type AuditDetail struct {
	OperatorID   uint        `json:"operator_id"`
	OperatorName string      `json:"operator_name"`
	TargetType   string      `json:"target_type"`
	TargetID     uint        `json:"target_id"`
	TargetName   string      `json:"target_name"`
	Action       string      `json:"action"`
	Before       interface{} `json:"before,omitempty"`
	After        interface{} `json:"after,omitempty"`
	IP           string      `json:"ip"`
	At           string      `json:"at"`
}

// writeAudit 落一条 rbac 审计（level=warn，source=rbac）。
func writeAudit(c *gin.Context, targetType string, targetID uint, targetName, action string, before, after interface{}) {
	if db.DB == nil {
		return
	}
	cl := claimsFromCtx(c)
	d := AuditDetail{
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Action:     action,
		Before:     before,
		After:      after,
		At:         time.Now().Format(time.RFC3339),
	}
	if cl != nil {
		d.OperatorID = cl.UserID
		d.OperatorName = cl.Username
	}
	if c != nil {
		d.IP = clientIP(c)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return
	}
	_ = db.DB.Create(&models.SystemLog{
		Level:   "WARN",
		Source:  "rbac",
		Message: messageFor(targetType, action, targetName),
		Detail:  string(b),
	}).Error
}

func messageFor(targetType, action, name string) string {
	switch action {
	case "assign_role":
		return "RBAC 角色分配：" + name
	case "reset_pwd":
		return "RBAC 密码重置：" + name
	case "update_data_scope":
		return "RBAC 角色数据范围变更：" + name
	case "change_status":
		return "RBAC 状态变更：" + name
	case "remove":
		return "RBAC 删除：" + name
	case "update_menu":
		return "RBAC 角色菜单权限变更：" + name
	default:
		return "RBAC 变更(" + targetType + ")：" + name + " " + action
	}
}

// auditViolation 记录"权限不足 / 未声明接口"（观察期的主要产出）。
func auditViolation(c *gin.Context, cl *models.Claims, perm, msg, route string) {
	if db.DB == nil {
		return
	}
	d := map[string]interface{}{
		"perm":  perm,
		"route": route,
		"at":    time.Now().Format(time.RFC3339),
		"stage": "rbac_guard",
	}
	if cl != nil {
		d["operator_id"] = cl.UserID
		d["operator_name"] = cl.Username
		d["role"] = string(cl.Role)
		d["dept_id"] = cl.DeptID
	}
	if c != nil {
		d["method"] = c.Request.Method
		d["ip"] = clientIP(c)
	}
	b, _ := json.Marshal(d)
	_ = db.DB.Create(&models.SystemLog{
		Level:   "WARN",
		Source:  "rbac",
		Message: msg,
		Detail:  string(b),
	}).Error
}

// clientIP 与 handlers.realClientIP 同口径（优先 X-Forwarded-For）。
func clientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return trimSpace(xff[:i])
			}
		}
		return trimSpace(xff)
	}
	if xri := trimSpace(c.GetHeader("X-Real-IP")); xri != "" {
		return xri
	}
	ra := c.Request.RemoteAddr
	for i := len(ra) - 1; i >= 0; i-- {
		if ra[i] == ':' {
			return ra[:i]
		}
	}
	return ra
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
