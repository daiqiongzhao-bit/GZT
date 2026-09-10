package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/logger"
	"shiftworkbench/internal/models"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gin-gonic/gin"
)

// ===================== 浏览器 Web Push（v0.13.0） =====================
// 说明：Web Push 要求页面运行在 HTTPS 安全上下文（浏览器强制）。
// 当前若部署在明文 HTTP 下，订阅接口与 SW 均不会生效——本模块保持完整可用，
// 待站点切到 HTTPS（反向代理 + TLS）后无需改任何代码即可启用。

var (
	vapidPub  string
	vapidPriv string
	vapidSubj = "mailto:admin@gzt.local"
)

// pushKeyFile VAPID 密钥文件（与数据库同目录，随数据卷持久化；权限 0600）
// config.C 为 nil（如单测直接构造 gin 引擎、未走 config.Init）时回退到当前目录，
// 避免空指针 panic —— 推送属于可选能力，配置缺失时不应拖垮业务主流程。
func pushKeyFile() string {
	dir := "."
	if config.C != nil && config.C.DBPath != "" {
		if d := filepath.Dir(config.C.DBPath); d != "." {
			dir = d
		}
	}
	if dir == "." {
		return "vapid.json"
	}
	return filepath.Join(dir, "vapid.json")
}

// EnsurePushKeys 加载或生成 VAPID 密钥对（幂等，可在启动与端点调用时反复触发）。
func EnsurePushKeys() error {
	if vapidPub != "" && vapidPriv != "" {
		return nil
	}
	if s := os.Getenv("VAPID_SUBJECT"); strings.TrimSpace(s) != "" {
		vapidSubj = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(strings.ToLower(vapidSubj), "mailto:") {
		vapidSubj = "mailto:admin@gzt.local"
	}
	p := pushKeyFile()
	if b, err := os.ReadFile(p); err == nil {
		var k struct {
			Public  string `json:"public"`
			Private string `json:"private"`
		}
		if json.Unmarshal(b, &k) == nil && k.Public != "" && k.Private != "" {
			vapidPub, vapidPriv = k.Public, k.Private
			return nil
		}
	}
	keysPriv, keysPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return fmt.Errorf("生成 VAPID 密钥失败: %w", err)
	}
	k := struct {
		Public  string `json:"public"`
		Private string `json:"private"`
	}{keysPub, keysPriv}
	b, _ := json.Marshal(k)
	if werr := os.WriteFile(p, b, 0o600); werr != nil {
		logger.Error("push", "VAPID 密钥写入失败: %v", werr)
	}
	vapidPub, vapidPriv = keysPub, keysPriv
	logger.Info("push", "已生成 VAPID 密钥，持久化至 %s", p)
	return nil
}

// GetVapidPublicKey GET /api/push/vapid-public 返回 VAPID 公钥（前端订阅用）
func GetVapidPublicKey(c *gin.Context) {
	if err := EnsurePushKeys(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_key": vapidPub, "subject": vapidSubj})
}

// PushSubscribe POST /api/push/subscribe 保存/更新当前用户的推送订阅
func PushSubscribe(c *gin.Context) {
	if err := EnsurePushKeys(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cl := currentClaims(c)
	var req struct {
		Endpoint string `json:"endpoint"`
		P256dh   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.P256dh = strings.TrimSpace(req.P256dh)
	req.Auth = strings.TrimSpace(req.Auth)
	if req.Endpoint == "" || req.P256dh == "" || req.Auth == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "订阅信息不完整"})
		return
	}
	var sub models.PushSubscription
	if err := db.DB.Where("endpoint = ?", req.Endpoint).First(&sub).Error; err == nil {
		// 已存在（换绑到当前用户/更新密钥）
		db.DB.Model(&models.PushSubscription{}).Where("id = ?", sub.ID).
			Updates(map[string]interface{}{"user_id": cl.UserID, "p256dh": req.P256dh, "auth": req.Auth})
	} else {
		db.DB.Create(&models.PushSubscription{
			UserID:   cl.UserID,
			Endpoint: req.Endpoint,
			P256dh:   req.P256dh,
			Auth:     req.Auth,
		})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PushUnsubscribe POST /api/push/unsubscribe 删除当前用户的订阅
func PushUnsubscribe(c *gin.Context) {
	cl := currentClaims(c)
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Endpoint != "" {
		db.DB.Where("user_id = ? AND endpoint = ?", cl.UserID, req.Endpoint).Delete(&models.PushSubscription{})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// pushToUser 向某用户的全部订阅设备推送站内提醒。无订阅/非 HTTPS 环境自动跳过。
// 404/410 表示订阅失效，顺手清理，避免后续每次都尝试。
// v0.17.0：整体加 recover 兜底 —— 推送是可选的增强能力，
// 任何内部异常都不允许向上冒泡影响发通知/广播等业务主流程。
func pushToUser(userID uint, title, body string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("push", "推送任务异常（已忽略，不影响主流程）: %v", r)
		}
	}()
	if userID == 0 {
		return
	}
	if err := EnsurePushKeys(); err != nil {
		return
	}
	var subs []models.PushSubscription
	if err := db.DB.Where("user_id = ?", userID).Find(&subs).Error; err != nil || len(subs) == 0 {
		return
	}
	payload, _ := json.Marshal(pushPayload{Title: title, Body: body, URL: "/"})
	opts := &webpush.Options{
		Subscriber:      vapidSubj,
		VAPIDPublicKey:  vapidPub,
		VAPIDPrivateKey: vapidPriv,
		TTL:             3600,
	}
	for _, s := range subs {
		sub := &webpush.Subscription{
			Endpoint: s.Endpoint,
			Keys:     webpush.Keys{P256dh: s.P256dh, Auth: s.Auth},
		}
		resp, err := webpush.SendNotification(payload, sub, opts)
		if err != nil {
			// 网络错误等：跳过本次
			continue
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone) {
			db.DB.Delete(&models.PushSubscription{}, s.ID) // 订阅已失效
		}
	}
}
