package wecompush

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GetSettings 公开接口：登录页只需要品牌信息，内部配置不对外暴露
func (h *H) GetSettings(c *gin.Context) {
	out := gin.H{"version": AppVersion}
	var items []WpSetting
	h.DB.Find(&items)
	for _, s := range items {
		for _, k := range brandKeys {
			if s.Key == k {
				out[k] = s.Value
			}
		}
	}
	ok(c, out)
}

// AllSettings 需要登录：设置页读取全部配置（含失败通知开关与通知群）
func (h *H) AllSettings(c *gin.Context) {
	out := gin.H{"version": AppVersion}
	var items []WpSetting
	h.DB.Find(&items)
	for _, s := range items {
		out[s.Key] = s.Value
	}
	if _, exists := out[SettingNotifyOnFail]; !exists {
		out[SettingNotifyOnFail] = "false"
	}
	ok(c, out)
}

func (h *H) UpdateSettings(c *gin.Context) {
	var body map[string]string
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, 400, "参数格式不正确")
		return
	}
	for _, k := range settingKeys {
		v, exists := body[k]
		if !exists {
			continue
		}
		if err := h.setSetting(k, strings.TrimSpace(v)); err != nil {
			fail(c, 500, "保存失败："+err.Error())
			return
		}
	}
	ok(c, gin.H{"ok": true})
}

// CLIStatusHandler 环境自检：CLI 是否可用、是否已授权
func (h *H) CLIStatusHandler(c *gin.Context) {
	ok(c, h.CLIStatus())
}

// ListGroups 列出机器人当前可发送的会话，方便用户照抄群名
func (h *H) ListGroups(c *gin.Context) {
	out, err := h.runCLI([]string{"message", "aibot", "sessions", "list"}, 60*time.Second)
	if err != nil {
		fail(c, 502, err.Error())
		return
	}
	data, err := extractJSON(out)
	if err != nil {
		fail(c, 502, err.Error())
		return
	}
	list := []gin.H{}
	if arr, ok2 := data["sessions"].([]any); ok2 {
		for _, s := range arr {
			m, ok3 := s.(map[string]any)
			if !ok3 {
				continue
			}
			list = append(list, gin.H{
				"chat_id":   toStr(m["chat_id"]),
				"chat_name": toStr(m["chat_name"]),
				"chat_type": toStr(m["chat_type"]),
			})
		}
	}
	ok(c, gin.H{"groups": list})
}

// FieldInfo 一个列（字段）的描述
type FieldInfo struct {
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Format  string   `json:"format,omitempty"`
	Options []string `json:"options,omitempty"`
}

func isDateType(t string) bool {
	t = strings.ToLower(t)
	return strings.Contains(t, "date") || strings.Contains(t, "time")
}

// ListFields 读取智能表格的字段（表头/列）信息：
// 列名 + 类型 + 日期格式 + 单选候选值，供前端直接做下拉与条件配置。
func (h *H) ListFields(c *gin.Context) {
	docid := strings.TrimSpace(c.Query("doc_id"))
	sheet := strings.TrimSpace(c.Query("sheet_title"))
	if docid == "" {
		fail(c, 400, "缺少 doc_id")
		return
	}
	payload := map[string]any{"docid": docid, "limit": 200}
	if sheet != "" {
		payload["sheet_title"] = sheet
	}
	b, _ := json.Marshal(payload)
	out, err := h.runCLI([]string{"smartsheet", "fields", "list", "--json", string(b)}, 90*time.Second)
	if err != nil {
		fail(c, 502, err.Error())
		return
	}
	data, err := extractJSON(out)
	if err != nil {
		fail(c, 502, err.Error())
		return
	}
	if ec, has := data["errcode"]; has && toInt(ec) != 0 {
		fail(c, 502, "读取字段失败 errcode="+toStr(ec)+" errmsg="+toStr(data["errmsg"]))
		return
	}

	fields := []FieldInfo{}
	if arr, ok2 := data["fields"].([]any); ok2 {
		for _, f := range arr {
			m, ok3 := f.(map[string]any)
			if !ok3 {
				continue
			}
			it := FieldInfo{Title: toStr(m["field_title"]), Type: toStr(m["field_type"])}
			if p, ok4 := m["property_date_time"].(map[string]any); ok4 {
				it.Format = toStr(p["format"])
			}
			if p, ok4 := m["property_single_select"].(map[string]any); ok4 {
				if opts, ok5 := p["options"].([]any); ok5 {
					for _, o := range opts {
						if om, ok6 := o.(map[string]any); ok6 {
							if t := strings.TrimSpace(toStr(om["text"])); t != "" {
								it.Options = append(it.Options, t)
							}
						}
					}
				}
			}
			if it.Title != "" {
				fields = append(fields, it)
			}
		}
	}

	dateFields := []string{}
	for _, f := range fields {
		if isDateType(f.Type) {
			dateFields = append(dateFields, f.Title)
		}
	}
	ok(c, gin.H{"fields": fields, "date_fields": dateFields, "total": data["total"]})
}
