package system

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// 全局拦截开关（方案 §12.2 灰度推进的「回滚闸」）
//
// 为什么单开一个入口、并且用**硬性超管校验**而不是走 GuardByPath：
//
//	GuardByPath 的判定结果本身受 EnforceMode 影响。如果把开关做成一个普通权限点，
//	那么在 log（只记不拦）观察期内，任何登录用户都能把开关切到 off —— 自相矛盾。
//	因此这里显式断言 IsSuperAdmin，绕过模式分支，任何模式下都必须是真的超管。
// ============================================================================

// SetEnforce 切换拦截模式：off / log / on。仅超级管理员。
func (h *H) SetEnforce(c *gin.Context) {
	cl := claimsFromCtx(c)
	if cl == nil {
		fail(c, http.StatusUnauthorized, "未认证")
		return
	}
	if !IsSuperAdmin(c) {
		writeAudit(c, "enforce", 0, "全局拦截开关", "denied", nil, gin.H{"by": cl.Username})
		fail(c, http.StatusForbidden, "仅超级管理员可切换权限拦截开关")
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != ModeOff && mode != ModeLog && mode != ModeOn {
		badReq(c, "mode 只能是 off / log / on")
		return
	}

	before := EnforceMode()
	if before == mode {
		ok(c, gin.H{"mode": mode, "unchanged": true})
		return
	}
	if err := SetEnforceMode(mode); err != nil {
		fail(c, http.StatusInternalServerError, "切换失败："+err.Error())
		return
	}
	writeAudit(c, "enforce", 0, "全局拦截开关", "update",
		gin.H{"mode": before}, gin.H{"mode": mode})
	ok(c, gin.H{"mode": mode, "before": before})
}
