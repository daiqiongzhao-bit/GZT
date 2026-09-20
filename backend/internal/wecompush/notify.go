package wecompush

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 设置键
const (
	SettingNotifyOnFail = "wp_notify_on_failure" // "true" / "false"
	SettingNotifyGroup  = "wp_notify_group"      // 接收失败提醒的会话名
)

// brandKeys 登录页需要的品牌信息（公开）
var brandKeys = []string{"company_name", "slogan", "copyright"}

// settingKeys 可通过接口写入的全部设置键（带 wp_ 前缀，避免与 GZT 既有设置键冲突）
var settingKeys = []string{
	"company_name", "slogan", "copyright",
	SettingNotifyOnFail, SettingNotifyGroup,
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

// NotifyFailure 任务执行失败时给管理员发一条企微提醒。
//
// 只在「失败通知」开关打开时发送；试跑属于交互操作，结果会直接显示在界面上，
// 因此不发提醒，避免打扰。
func (h *H) NotifyFailure(t *WpTask, trigger, reason string) {
	if !h.NotifyOnFailEnabled() {
		return
	}
	if trigger == "dry-run" {
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
	name := "(未知任务)"
	if t != nil {
		name = t.Name
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
	if group == "" {
		fail(c, 400, "请先选择接收提醒的会话")
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
