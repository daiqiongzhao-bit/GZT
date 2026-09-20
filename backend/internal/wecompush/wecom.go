package wecompush

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// wecom-cli 调用层
// ---------------------------------------------------------------------------

// cliEnv 给子进程准备环境：CLI 需要 HOME 才能找到 ~/.config/wecom 下的凭据
func (h *H) cliEnv() []string {
	env := os.Environ()
	has := func(k string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, k+"=") {
				return true
			}
		}
		return false
	}
	if !has("HOME") {
		env = append(env, "HOME=/root")
	}
	// 凭据目录可被显式覆盖（见 config.CLIConfig）
	if h.Cfg.CLIConfig != "" && !has("WECOM_CONFIG_DIR") {
		env = append(env, "WECOM_CONFIG_DIR="+h.Cfg.CLIConfig)
	}
	return env
}

// runCLI 执行 wecom-cli 子命令，返回合并后的输出
func (h *H) runCLI(args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(h.Cfg.CLIPath, args...)
	cmd.Env = h.cliEnv()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("无法启动 wecom-cli（%s）：%w", h.Cfg.CLIPath, err)
	}
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		out := buf.String()
		if err != nil {
			return out, fmt.Errorf("wecom-cli 执行失败：%w\n%s", err, truncate(out, 800))
		}
		return out, nil
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return buf.String(), fmt.Errorf("wecom-cli 执行超时（%s）", timeout)
	}
}

// extractJSON 从可能夹带日志的输出里截取 JSON 主体
func extractJSON(text string) (map[string]any, error) {
	i := strings.Index(text, "{")
	j := strings.LastIndex(text, "}")
	if i < 0 || j < 0 || j < i {
		return nil, fmt.Errorf("命令输出里没有 JSON：%s", truncate(text, 500))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text[i:j+1]), &m); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败：%v（原文 %s）", err, truncate(text[i:j+1], 500))
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// 凭据与授权自检
// ---------------------------------------------------------------------------

// CLIStatus 供「设置」页做环境自检
type CLIStatus struct {
	Available  bool   `json:"available"`
	Version    string `json:"version"`
	Authorized bool   `json:"authorized"`
	BotID      string `json:"bot_id"`
	Detail     string `json:"detail"`
	Path       string `json:"path"`
}

func (h *H) CLIStatus() CLIStatus {
	st := CLIStatus{Path: h.Cfg.CLIPath}
	if _, err := os.Stat(h.Cfg.CLIPath); err != nil {
		st.Detail = "找不到 wecom-cli 可执行文件：" + h.Cfg.CLIPath
		return st
	}
	out, err := h.runCLI([]string{"--version"}, 20*time.Second)
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	st.Available = true
	// 原始输出形如 "wecom-cli 1.3.0 (wecom 2026-09-16T12:06:56Z dcf6929)"，
	// 只保留「程序名 + 版本号」，避免版本串过长撑破界面
	st.Version = shortVersion(out)

	authOut, err := h.runCLI([]string{"auth", "show"}, 20*time.Second)
	if err != nil {
		st.Detail = "授权状态检查失败：" + truncate(authOut, 300)
		return st
	}
	st.Authorized = strings.Contains(strings.ToLower(authOut), "authorized") ||
		strings.Contains(authOut, "已授权")
	if !st.Authorized {
		st.Detail = "企业微信未授权（" + strings.TrimSpace(authOut) + "）"
		return st
	}
	st.BotID = parseBotID(authOut)
	st.Detail = "已授权。企业微信未返回明确的过期时间；只要 ~/.config/wecom 下的凭据文件未被删除或失效，即可持续使用。"
	return st
}

// parseBotID 从 auth show 输出里解析 Bot ID
func parseBotID(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "bot id:") {
			return strings.TrimSpace(line[7:])
		}
		if idx := strings.Index(strings.ToLower(line), "bot id"); idx >= 0 {
			parts := strings.SplitN(line[idx:], ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 智能表格读取
// ---------------------------------------------------------------------------

func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func quoteStr(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}

// TaskCondition 一个附加筛选条件
type TaskCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// 支持的运算符（前端下拉与之保持一致）
var condOps = map[string]bool{
	"is_null": true, "not_null": true, "eq": true, "ne": true,
	"contains": true, "not_contains": true, "in": true, "date_eq": true,
}

// ParseConditions 解析任务里存的 JSON 条件数组，顺手丢掉不完整的行
func ParseConditions(s string) []TaskCondition {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var raw []TaskCondition
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil
	}
	out := make([]TaskCondition, 0, len(raw))
	for _, c := range raw {
		f := strings.TrimSpace(c.Field)
		op := strings.TrimSpace(c.Op)
		if f == "" || op == "" {
			continue
		}
		out = append(out, TaskCondition{Field: f, Op: op, Value: strings.TrimSpace(c.Value)})
	}
	return out
}

// splitList 把「甲,乙、丙」这类输入切成候选值
func splitList(s string) []string {
	isSep := func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
	}
	out := []string{}
	for _, p := range strings.FieldsFunc(s, isSep) {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// conditionSQL 把单个条件翻译成 SQL 片段。
//
// 关键技巧：智能表格的日期列底层是 Excel 序列号，直接与日期字符串比较不会命中。
// 所以比较类条件一律写成「原值比较 OR 日期格式化后比较」两个分支，
// 不必事先知道列的类型，就能同时兼容文本列、单选列与日期列。
func conditionSQL(c TaskCondition, today string, loc *time.Location) (string, error) {
	col := quoteIdent(c.Field)
	dateExpr := "DATE_FORMAT(" + col + `, "%Y-%m-%d")`
	switch c.Op {
	case "is_null":
		return col + " IS NULL", nil
	case "not_null":
		return col + " IS NOT NULL", nil
	case "eq":
		return "((" + col + " = " + quoteStr(c.Value) + ") OR (" + dateExpr + " = " + quoteStr(c.Value) + "))", nil
	case "ne":
		return "((" + col + " <> " + quoteStr(c.Value) + ") AND (" + dateExpr + " <> " + quoteStr(c.Value) + "))", nil
	case "contains":
		return "((" + col + " LIKE " + quoteStr("%"+c.Value+"%") + ") OR (" + dateExpr + " LIKE " + quoteStr("%"+c.Value+"%") + "))", nil
	case "not_contains":
		return "(" + col + " NOT LIKE " + quoteStr("%"+c.Value+"%") + ")", nil
	case "in":
		items := splitList(c.Value)
		if len(items) == 0 {
			return "", fmt.Errorf("条件「%s 属于」缺少候选值", c.Field)
		}
		quoted := make([]string, 0, len(items))
		for _, it := range items {
			quoted = append(quoted, quoteStr(it))
		}
		return col + " IN (" + strings.Join(quoted, ", ") + ")", nil
	case "date_eq":
		n, err := strconv.Atoi(strings.TrimSpace(c.Value))
		if err != nil {
			n = 0
		}
		base := today
		if loc == nil {
			loc = time.FixedZone("CST", 8*3600)
		}
		if t, perr := time.ParseInLocation("2006-01-02", today, loc); perr == nil {
			base = t.AddDate(0, 0, n).Format("2006-01-02")
		}
		return dateExpr + " = " + quoteStr(base), nil
	default:
		return "", fmt.Errorf("不支持的运算符：%s", c.Op)
	}
}

// BuildSelectSQL 组装服务端筛选 SQL。
func BuildSelectSQL(t DateQuery, targetDate string) (string, error) {
	if strings.TrimSpace(t.DocID) == "" {
		return "", fmt.Errorf("缺少文档 ID")
	}
	if strings.TrimSpace(t.SheetTitle) == "" {
		return "", fmt.Errorf("缺少子表名")
	}
	if strings.TrimSpace(t.DateField) == "" {
		return "", fmt.Errorf("缺少日期列")
	}

	cols := ParseColumns(t.Columns)
	sel := []string{"RECORD_ID"}
	if len(cols) == 0 {
		sel = append(sel, "*")
	} else {
		for _, c := range cols {
			sel = append(sel, quoteIdent(c))
		}
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(sel, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdent(t.SheetTitle))
	sb.WriteString(" WHERE DATE_FORMAT(")
	sb.WriteString(quoteIdent(t.DateField))
	sb.WriteString(`, "%Y-%m-%d") = `)
	sb.WriteString(quoteStr(targetDate))

	// 附加条件：优先用新结构（可多条），否则回退到旧字段
	conds := t.Conditions
	if len(conds) == 0 && strings.TrimSpace(t.ExtraField) != "" && strings.TrimSpace(t.ExtraOp) != "" {
		conds = []TaskCondition{{Field: t.ExtraField, Op: t.ExtraOp, Value: t.ExtraValue}}
	}
	for _, c := range conds {
		frag, err := conditionSQL(c, t.Today, t.Loc)
		if err != nil {
			return "", err
		}
		sb.WriteString(" AND " + frag)
	}

	sb.WriteString(" LIMIT 500")
	return sb.String(), nil
}

// DateQuery 是筛选所需的字段子集（WpTask 实现该形状即可）
type DateQuery struct {
	DocID      string
	SheetTitle string
	DateField  string
	Columns    string
	Conditions []TaskCondition
	Today      string
	Loc        *time.Location

	// 兼容旧数据
	ExtraField string
	ExtraOp    string
	ExtraValue string
}

// FetchRows 执行查询并返回记录行（每行是 列名→值 的映射）
func (h *H) FetchRows(sql, docID string) ([]map[string]any, error) {
	out, err := h.runCLI([]string{"smartsheet", "records", "query", "--docid", docID, "--sql", sql}, 180*time.Second)
	if err != nil {
		return nil, err
	}
	data, err := extractJSON(out)
	if err != nil {
		return nil, err
	}
	if ec, ok := data["errcode"]; ok && toInt(ec) != 0 {
		return nil, fmt.Errorf("查询返回错误 errcode=%v errmsg=%v", ec, data["errmsg"])
	}
	rows := []map[string]any{}
	if vals, ok := data["values"].([]any); ok {
		for _, blk := range vals {
			m, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			if rs, ok := m["rows"].([]any); ok {
				for _, rr := range rs {
					if row, ok := rr.(map[string]any); ok {
						rows = append(rows, row)
					}
				}
			}
		}
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// 会话与发送
// ---------------------------------------------------------------------------

type session struct {
	ChatID   string `json:"chat_id"`
	ChatName string `json:"chat_name"`
	ChatType string `json:"chat_type"`
}

// FindChatID 按群名精确匹配可发送会话。
// 机器人只能给「近期有消息往来」的会话发消息，故匹配不到时会给出可读的原因。
func (h *H) FindChatID(groupName string) (string, error) {
	out, err := h.runCLI([]string{"message", "aibot", "sessions", "list"}, 60*time.Second)
	if err != nil {
		return "", err
	}
	data, err := extractJSON(out)
	if err != nil {
		return "", err
	}
	var names []string
	if arr, ok := data["sessions"].([]any); ok {
		for _, s := range arr {
			m, ok := s.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(toStr(m["chat_name"]))
			names = append(names, name)
			if name == strings.TrimSpace(groupName) {
				return toStr(m["chat_id"]), nil
			}
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("机器人当前没有任何可发送的会话。请先把机器人拉进群「%s」并在群里 @ 一次机器人", groupName)
	}
	return "", fmt.Errorf("可发送会话里没有群「%s」。当前可用：%s", groupName, strings.Join(names, "、"))
}

// UploadMedia 上传文件换取 media_id
func (h *H) UploadMedia(path string) (string, error) {
	payload, _ := json.Marshal(map[string]any{"file_path": path, "type": "file"})
	out, err := h.runCLI([]string{"media", "upload", "--json", string(payload)}, 180*time.Second)
	if err != nil {
		return "", err
	}
	data, err := extractJSON(out)
	if err != nil {
		return "", err
	}
	if mid := toStr(data["media_id"]); mid != "" {
		return mid, nil
	}
	if d, ok := data["data"].(map[string]any); ok {
		if mid := toStr(d["media_id"]); mid != "" {
			return mid, nil
		}
	}
	if ec, ok := data["errcode"]; ok && toInt(ec) != 0 {
		return "", fmt.Errorf("上传失败 errcode=%v errmsg=%v", ec, data["errmsg"])
	}
	return "", fmt.Errorf("上传未返回 media_id：%s", truncate(out, 400))
}

func (h *H) sendPayload(payload map[string]any) error {
	b, _ := json.Marshal(payload)
	out, err := h.runCLI([]string{"message", "aibot", "send", "--json", string(b)}, 120*time.Second)
	if err != nil {
		return err
	}
	data, jerr := extractJSON(out)
	if jerr != nil {
		return nil // 命令本身成功但没有 JSON 输出，视为成功
	}
	if ec, ok := data["errcode"]; ok && toInt(ec) != 0 {
		return fmt.Errorf("发送失败 errcode=%v errmsg=%v", ec, data["errmsg"])
	}
	return nil
}

// SendFile 发送文件消息
func (h *H) SendFile(chatID, mediaID string) error {
	return h.sendPayload(map[string]any{
		"chat_id":  chatID,
		"msg_type": "file",
		"file":     map[string]any{"media_id": mediaID},
	})
}

// SendMarkdown 发送 markdown 文本消息
func (h *H) SendMarkdown(chatID, content string) error {
	return h.sendPayload(map[string]any{
		"chat_id":  chatID,
		"msg_type": "markdown",
		"markdown": map[string]any{"content": content},
	})
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// shortVersion 从 "wecom-cli 1.3.0 (wecom 2026-… dcf6929)" 里取出 "wecom-cli 1.3.0"
func shortVersion(s string) string {
	v := strings.TrimSpace(s)
	if i := strings.IndexAny(v, "(\n\r"); i > 0 {
		v = strings.TrimSpace(v[:i])
	}
	if len([]rune(v)) > 40 {
		v = truncate(v, 40)
	}
	return v
}

func toStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}

// excelSerialBase Excel 1900 日期系统的换算基准（配合闰年 bug 偏移）
var excelSerialBase = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// cellToString 把单元格值转成适合写入 Excel 的内容。
// 对日期列，数值 → 按 Excel 序列号还原成 YYYY-MM-DD（时间戳归零时省略时间）。
func cellToString(v any, isDate bool, loc *time.Location) any {
	if v == nil {
		return ""
	}
	if f, ok := v.(float64); ok {
		if isDate && f > 0 {
			t := excelSerialBase.AddDate(0, 0, int(f)).In(loc)
			if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
				return t.Format("2006-01-02")
			}
			return t.Format("2006-01-02 15:04")
		}
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.TrimSpace(toStr(v))
}
