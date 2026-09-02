package handlers

import (
	"net/http"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

func ListWebhooks(c *gin.Context) {
	scope := deptScopeIDs(c)
	var list []models.Webhook
	q := db.DB.Order("id asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 仅超管可看到解密后的真实地址；其他人仅看到脱敏标识
	cl := currentClaims(c)
	for i := range list {
		if cl.Role != models.RoleSuperAdmin {
			list[i].URL = maskURL(list[i].URL)
		} else {
			if plain, err := db.Decrypt(list[i].URL); err == nil {
				list[i].URL = plain
			}
		}
	}
	c.JSON(http.StatusOK, list)
}

type webhookReq struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
	DeptID uint   `json:"dept_id"`
}

// CreateWebhook 新建 Webhook，地址与密钥加密存储
func CreateWebhook(c *gin.Context) {
	var req webhookReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook 地址不能为空"})
		return
	}
	if req.Type == "" {
		req.Type = "wecom"
	}
	// 保存前连通性校验：先发测试消息（8 秒超时），失败则不保存
	type hk struct{ err error }
	done := make(chan hk, 1)
	go func() { done <- hk{sendToHookRaw(req.URL, req.Type, req.Secret, "✅ 排班工作台配置校验：渠道连通正常。", nil)} }()
	select {
	case r := <-done:
		if r.err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook 校验失败（未保存）: " + r.err.Error()})
			return
		}
	case <-time.After(8 * time.Second):
		c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook 校验超时（8 秒），请检查地址是否正确"})
		return
	}
	cl := currentClaims(c)
	deptID := req.DeptID
	if cl.Role != models.RoleSuperAdmin {
		deptID = cl.DeptID
	}
	enc, err := db.Encrypt(req.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加密失败"})
		return
	}
	w := models.Webhook{Name: req.Name, Type: req.Type, URL: enc, DeptID: deptID}
	if req.Secret != "" {
		if sec, e := db.Encrypt(req.Secret); e == nil {
			w.Secret = sec
		}
	}
	if err := db.DB.Create(&w).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "新建 Webhook: "+req.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UpdateWebhook PUT /webhooks/:id 编辑 Webhook（改名称/类型/地址/密钥/所属部门）
func UpdateWebhook(c *gin.Context) {
	var req webhookReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	var w models.Webhook
	if err := db.DB.First(&w, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Webhook 不存在"})
		return
	}
	if !canManageDept(c, w.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改其他部门 Webhook"})
		return
	}
	typ := req.Type
	if typ == "" {
		typ = "wecom"
	}
	cl := currentClaims(c)
	// 改了地址才重新做连通性校验；URL 未变（前端回填原值）则跳过。
	// 非超管看到的 URL 是脱敏的（••••••），不允许改地址。
	urlChanged := false
	if cl.Role == models.RoleSuperAdmin {
		plainURL, _ := db.Decrypt(w.URL)
		urlChanged = req.URL != "" && req.URL != plainURL
	}
	if urlChanged {
		type hk struct{ err error }
		done := make(chan hk, 1)
		go func() { done <- hk{sendToHookRaw(req.URL, typ, req.Secret, "✅ 排班工作台配置校验：渠道连通正常。", nil)} }()
		select {
		case r := <-done:
			if r.err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook 校验失败（未保存）: " + r.err.Error()})
				return
			}
		case <-time.After(8 * time.Second):
			c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook 校验超时（8 秒），请检查地址是否正确"})
			return
		}
	}
	deptID := req.DeptID
	if cl.Role != models.RoleSuperAdmin {
		deptID = w.DeptID // 非超管不允许改部门
	}
	if deptID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择 Webhook 所属部门"})
		return
	}
	w.Name = req.Name
	if w.Name == "" {
		w.Name = "未命名渠道"
	}
	w.Type = typ
	w.DeptID = deptID
	if urlChanged {
		if enc, err := db.Encrypt(req.URL); err == nil {
			w.URL = enc
		}
	}
	if req.Secret != "" { // 留空表示不修改密钥
		if sec, e := db.Encrypt(req.Secret); e == nil {
			w.Secret = sec
		}
	}
	if err := db.DB.Save(&w).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "编辑 Webhook: "+w.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteWebhook 删除 Webhook
func DeleteWebhook(c *gin.Context) {
	if err := db.DB.Delete(&models.Webhook{}, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "删除 Webhook: "+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func maskURL(s string) string {
	if len(s) <= 8 {
		return "••••••"
	}
	return s[:4] + "••••••" + s[len(s)-4:]
}

// TestWebhook POST /api/webhooks/test 发送测试消息验证配置
func TestWebhook(c *gin.Context) {
	var req struct {
		URL    string `json:"url"`
		Type   string `json:"type"`
		Secret string `json:"secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写 Webhook 地址"})
		return
	}
	if req.Type == "" {
		req.Type = "wecom"
	}
	if err := sendToHookRaw(req.URL, req.Type, req.Secret, "✅ 这是一条来自排班工作台的测试消息，渠道配置正常。", nil); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "msg": "测试消息已发送，请到对应群确认"})
}
