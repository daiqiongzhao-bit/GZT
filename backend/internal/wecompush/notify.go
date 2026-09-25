package wecompush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 设置键
const (
	SettingNotifyOnFail = "wp_notify_on_failure" // "true" / "false"
	SettingNotifyGroup  = "wp_notify_group"      // 接收失败提醒的会话名
	// v0.41.1：失败告警的独立 webhook 通道。
	// 与上面两个键刻意解耦——企微群通道依赖"已授权企微 + 选了会话"，而 webhook 只要填个
	// URL 就能用（可指向自建告警系统 / 钉钉 / 飞书 / 短信网关等）。两者互不影响：
	// 群没配不影响 webhook，webhook 没填也不影响群。
	SettingAlertWebhook = "wp_alert_webhook"
)

// brandKeys 登录页需要的品牌信息（公开）
var brandKeys = []string{"company_name", "slogan", "copyright"}

// settingKeys 可通过接口写入的全部设置键（带 wp_ 前缀，避免与 GZT 既有设置键冲突）
var settingKeys = []string{
	"company_name", "slogan", "copyright",
	SettingNotifyOnFail, SettingNotifyGroup, SettingAlertWebhook,
}

func (h *H) getSetting(key string) string {
	var s WpSetting
	if err := h.DB.Where("key = ?", key).First(&s).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(s.Value)
}

func (h *H) setSetting(key, value string) error {
	var s WpSetting
	if err := h.DB.Where("key = ?", key).First(&s).Error; err != nil {
		return h.DB.Create(&WpSetting{Key: key, Value: value}).Error
	}
	return h.DB.Model(&s).Update("value", value).Error
}

// NotifyOnFailEnabled 失败通知是否开启
func (h *H) NotifyOnFailEnabled() bool {
	v := strings.ToLower(h.getSetting(SettingNotifyOnFail))
	return v == "true" || v == "1" || v == "on" || v == "yes"
}

func triggerText(trigger string) string {
	switch trigger {
	case "schedule":
		return "定时触发"
	case "manual":
		return "手动运行"
	case "dry-run":
		return "试跑"
	case "catchup":
		return "漏跑补偿"
	default:
		return trigger
	}
}

func (h *H) nowText() string {
	loc := h.Cfg.Loc
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return time.Now().In(loc).Format("2006-01-02 15:04:05")
}

// NotifyFailure 任务执行失败时告警。
//
// v0.41.1 起有**两条互不相干**的通道，任一配置好就会发：
//  1. 企微群提醒（受「失败通知」开关 + 通知群控制）
//  2. Webhook（只要填了 URL 就发，可对接自建告警/钉钉/飞书等）
//
// 试跑（dry-run）属于界面上的交互操作，结果当场可见，两条通道都不发，免得打扰。
func (h *H) NotifyFailure(t *WpTask, trigger, reason string) {
	if trigger == "dry-run" {
		return
	}
	name := "(未知任务)"
	if t != nil {
		name = t.Name
	}
	h.notifyGroupFailure(name, trigger, reason)
	h.notifyWebhookFailure(name, trigger, reason)
}

// notifyGroupFailure 通道一：企微群 markdown 提醒（v0.41.1 前既有的唯一通道）
func (h *H) notifyGroupFailure(name, trigger, reason string) {
	if !h.NotifyOnFailEnabled() {
		return
	}
	group := h.getSetting(SettingNotifyGroup)
	if group == "" {
		log.Printf("[wp失败通知] 已开启但未配置通知群，跳过")
		return
	}
	chatID, err := h.FindChatID(group)
	if err != nil {
		log.Printf("[wp失败通知] 找不到通知群「%s」：%v", group, err)
		return
	}
	text := fmt.Sprintf(
		"⚠️ **企微推送任务执行失败**\n> 任务：%s\n> 触发：%s\n> 时间：%s\n> 原因：%s\n\n请到 GZT「企微推送」页查看详情。",
		name, triggerText(trigger), h.nowText(), truncate(reason, 300),
	)
	if err := h.SendMarkdown(chatID, text); err != nil {
		log.Printf("[wp失败通知] 发送失败：%v", err)
		return
	}
	log.Printf("[wp失败通知] 已提醒「%s」：%s（%s）", group, name, reason)
}

// notifyWebhookFailure 通道二：失败告警 webhook（v0.41.1 新增的独立通道）
//
// 两个设计要点：
//   · 异步：投递在 goroutine 里做。告警通道挂掉/超时绝不能拖慢或卡死推送任务本身，
//     这是"告警"这种旁路能力的基本自律。
//   · 只认 URL：不读 NotifyOnFailEnabled 开关，也不依赖企微授权。填了就发，
//     这样"没接企微但想收到告警"的场景也能用。
func (h *H) notifyWebhookFailure(name, trigger, reason string) {
	url := strings.TrimSpace(h.getSetting(SettingAlertWebhook))
	if url == "" {
		return
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		log.Printf("[wp失败告警webhook] URL 不是 http/https，跳过：%s", url)
		return
	}
	payload, err := json.Marshal(map[string]string{
		"event":        "wecom_push_task_failed",
		"task":         name,
		"trigger":      trigger,
		"trigger_text": triggerText(trigger),
		"time":         h.nowText(),
		"reason":       truncate(reason, 500),
	})
	if err != nil {
		log.Printf("[wp失败告警webhook] 序列化失败：%v", err)
		return
	}
	go func(u string, body []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			log.Printf("[wp失败告警webhook] 构造请求失败：%v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[wp失败告警webhook] 投递失败：%v", err)
			return
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body) // 读掉并丢弃响应体，避免连接无法复用
		if resp.StatusCode >= 300 {
			log.Printf("[wp失败告警webhook] 目标返回非 2xx：%d", resp.StatusCode)
			return
		}
		log.Printf("[wp失败告警webhook] 已投递：%s（%s）", name, triggerText(trigger))
	}(url, payload)
}

// SendTestNotify 设置页的「发送测试提醒」按钮
func (h *H) SendTestNotify(c *gin.Context) {
	var body struct {
		Group string `json:"group"`
	}
	_ = c.ShouldBindJSON(&body)
	group := strings.TrimSpace(body.Group)
	if group == "" {
		group = h.getSetting(SettingNotifyGroup)
	}
	// v0.41.1：只配了 webhook 没配群时，测试按钮应当测 webhook 而不是报"请先选择会话"——
	// 否则独立通道配好了却无法自检，只能等真出故障才知道通不通。
	if group == "" {
		if wh := strings.TrimSpace(h.getSetting(SettingAlertWebhook)); wh != "" {
			h.notifyWebhookFailure("（测试）连通性验证", "manual",
				"这是一条失败告警通道的测试消息，收到说明 webhook 配置正常。")
			ok(c, gin.H{"ok": true, "message": "测试消息已投递到告警 webhook（后台异步发送，请到目标端确认）"})
			return
		}
		fail(c, 400, "请先选择接收提醒的会话，或配置告警 Webhook 地址")
		return
	}
	chatID, err := h.FindChatID(group)
	if err != nil {
		fail(c, 502, "找不到可发送会话："+err.Error())
		return
	}
	text := fmt.Sprintf("✅ **企微推送失败提醒通道测试**\n> 时间：%s\n\n收到本消息说明「任务失败提醒」配置正常。", h.nowText())
	if err := h.SendMarkdown(chatID, text); err != nil {
		fail(c, 502, "发送失败："+err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "message": "测试消息已发送到「" + group + "」"})
}
