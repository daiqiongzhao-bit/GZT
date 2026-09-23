package wecompush

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// 同一任务不允许并发执行（定时触发与手动触发可能撞车）
var taskLocks sync.Map

func lockTask(id uint) func() {
	v, _ := taskLocks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// RunResult 一次执行的结果摘要
type RunResult struct {
	Log *WpLog `json:"log"`
}

// ExecuteTask 执行一条任务：查表 → 筛选 → 生成 Excel → 发到群。
//
// trigger: schedule / manual / catchup；dryRun=true 时只查数据并生成本地文件，不发送。
func (h *H) ExecuteTask(t *WpTask, trigger string, dryRun bool) *WpLog {
	unlock := lockTask(t.ID)
	defer unlock()

	start := time.Now()
	loc := h.Cfg.Loc
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	targetDate := time.Now().In(loc).AddDate(0, 0, t.OffsetDays).Format("2006-01-02")

	entry := &WpLog{
		TaskID:   t.ID,
		TaskName: t.Name,
		RunAt:    time.Now(),
		Trigger:  trigger,
	}
	if dryRun {
		entry.Trigger = "dry-run"
	}

	finalize := func(status, msg, fname string, count int) *WpLog {
		entry.Status = status
		entry.Message = msg
		entry.FileName = fname
		entry.Count = count
		entry.DurationMs = time.Since(start).Milliseconds()
		h.DB.Create(entry)

		now := time.Now()
		upd := map[string]any{
			"last_run_at": now,
			"last_status": status,
			"last_count":  count,
			"last_error":  "",
		}
		if status == "failed" {
			upd["last_error"] = msg
			// 失败提醒：异步发送，不拖慢本次执行；试跑不发（NotifyFailure 内部拦截）
			go h.NotifyFailure(t, entry.Trigger, msg)
		}
		h.DB.Model(&WpTask{}).Where("id = ?", t.ID).Updates(upd)
		return entry
	}

	// 1) 组装筛选 SQL（服务端筛选，日期两侧都格式化成字符串比较）
	sql, err := BuildSelectSQL(DateQuery{
		DocID:      t.DocID,
		SheetTitle: t.SheetTitle,
		DateField:  t.DateField,
		Columns:    t.Columns,
		Conditions: ParseConditions(t.Conditions),
		Today:      time.Now().In(loc).Format("2006-01-02"),
		Loc:        loc,
		ExtraField: t.ExtraField,
		ExtraOp:    t.ExtraOp,
		ExtraValue: t.ExtraValue,
	}, targetDate)
	if err != nil {
		return finalize("failed", "组装查询失败："+err.Error(), "", 0)
	}

	// 2) 取数
	rows, err := h.FetchRows(sql, t.DocID)
	if err != nil {
		return finalize("failed", "读取表格失败："+err.Error(), "", 0)
	}
	n := len(rows)

	title := t.MsgTitle
	if strings.TrimSpace(title) == "" {
		title = "【定时提醒】"
	}

	// 3) 0 条：只发一条文本提示
	if n == 0 {
		if dryRun {
			return finalize("empty", fmt.Sprintf("[试跑] 目标日期 %s 命中 0 条，正式执行时会发送提示语。", targetDate), "", 0)
		}
		chatID, err := h.FindChatID(t.GroupName)
		if err != nil {
			return finalize("failed", "找不到目标群："+err.Error(), "", 0)
		}
		text := strings.TrimSpace(t.EmptyText)
		if text == "" {
			text = fmt.Sprintf("%s %s 今日暂无符合条件的记录。", title, targetDate)
		} else {
			// v0.40.8：0 条提示语同样支持变量
			text = RenderTpl(text, map[string]string{
				"title": title, "date": targetDate, "count": "0",
				"filename": "", "task": t.Name,
			})
		}
		if err := h.SendMarkdown(chatID, text); err != nil {
			return finalize("failed", "发送提示失败："+err.Error(), "", 0)
		}
		return finalize("empty", fmt.Sprintf("%s 命中 0 条，已发送无数据提示。", targetDate), "", 0)
	}

	// 4) 生成 Excel
	headers := ParseColumns(t.Columns)
	if len(headers) == 0 {
		headers = h.deriveHeaders(rows, t.DocID, t.SheetTitle)
	}
	dateCols := map[string]bool{}
	for _, c := range ParseColumns(t.DateCols) {
		dateCols[c] = true
	}
	if strings.TrimSpace(t.DateField) != "" {
		dateCols[t.DateField] = true
	}

	body := make([][]any, 0, n)
	for _, r := range rows {
		line := make([]any, len(headers))
		for i, hd := range headers {
			line[i] = cellToString(r[hd], dateCols[hd], loc)
		}
		body = append(body, line)
	}

	prefix := strings.TrimSpace(t.FilePrefix)
	if prefix == "" {
		prefix = "明细"
	}
	// v0.40.8：文件名模板（{date} {task} {prefix}），空 = 兼容旧「前缀_日期.xlsx」
	base := ""
	if ft := strings.TrimSpace(t.FileTpl); ft != "" {
		base = RenderTpl(ft, map[string]string{
			"date": targetDate, "task": t.Name, "prefix": prefix,
		})
	} else {
		base = fmt.Sprintf("%s_%s", prefix, targetDate)
	}
	base = SanitizeFileName(strings.TrimSuffix(base, ".xlsx"))
	fname := base + ".xlsx"
	fpath := h.Cfg.OutPath(fname)
	if err := BuildXLSX(fpath, prefix, headers, body); err != nil {
		return finalize("failed", "生成 Excel 失败："+err.Error(), "", n)
	}

	if dryRun {
		return finalize("success", fmt.Sprintf("[试跑] 目标日期 %s 命中 %d 条，Excel 已生成于 %s（未发送）。", targetDate, n, fpath), fname, n)
	}

	// 5) 投递：找群 → 上传 → 发文件 → 发说明
	chatID, err := h.FindChatID(t.GroupName)
	if err != nil {
		return finalize("failed", "找不到目标群："+err.Error(), fname, n)
	}
	mediaID, err := h.UploadMedia(fpath)
	if err != nil {
		return finalize("failed", "上传附件失败："+err.Error(), fname, n)
	}
	if err := h.SendFile(chatID, mediaID); err != nil {
		return finalize("failed", "发送附件失败："+err.Error(), fname, n)
	}
	// v0.40.8：消息模板（{title} {date} {count} {filename} {task}），空 = 兼容旧默认文案
	text := strings.TrimSpace(t.MsgTemplate)
	if text == "" {
		text = fmt.Sprintf("%s %s 共 **%d** 条\n明细见上方附件《%s》。", title, targetDate, n, fname)
	} else {
		text = RenderTpl(text, map[string]string{
			"title": title, "date": targetDate, "count": fmt.Sprintf("%d", n),
			"filename": fname, "task": t.Name,
		})
	}
	if err := h.SendMarkdown(chatID, text); err != nil {
		return finalize("failed", "附件已发送，但说明消息发送失败："+err.Error(), fname, n)
	}

	return finalize("success", fmt.Sprintf("已向「%s」推送 %d 条（%s）。", t.GroupName, n, targetDate), fname, n)
}

// isRecordIDKey 判断是否为记录 ID 类字段（RECORD_ID 及其变体，如 vkfnip_RECORD_ID）
func isRecordIDKey(k string) bool {
	return k == "RECORD_ID" || strings.HasSuffix(k, "_RECORD_ID")
}

// deriveHeaders 用户未指定输出列时，推导输出表头。
// 优先按智能表格自身的列顺序（fields list 的返回顺序即表格列顺序），
// 并排除 RECORD_ID 及其变体；字段列表取不到时回退为按名称排序。
func (h *H) deriveHeaders(rows []map[string]any, docID, sheetTitle string) []string {
	if len(rows) == 0 {
		return nil
	}
	first := rows[0]
	if titles, err := h.sheetFieldOrder(docID, sheetTitle); err == nil && len(titles) > 0 {
		out := make([]string, 0, len(titles))
		for _, t := range titles {
			if isRecordIDKey(t) {
				continue
			}
			if _, ok := first[t]; ok {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]string, 0, len(first))
	for k := range first {
		if isRecordIDKey(k) {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sheetFieldOrder 调 wecom-cli 拿到表格列（字段）的自然顺序
func (h *H) sheetFieldOrder(docID, sheetTitle string) ([]string, error) {
	if strings.TrimSpace(docID) == "" {
		return nil, fmt.Errorf("缺少 doc_id")
	}
	payload := map[string]any{"docid": docID, "limit": 200}
	if strings.TrimSpace(sheetTitle) != "" {
		payload["sheet_title"] = sheetTitle
	}
	b, _ := json.Marshal(payload)
	out, err := h.runCLI([]string{"smartsheet", "fields", "list", "--json", string(b)}, 90*time.Second)
	if err != nil {
		return nil, err
	}
	data, err := extractJSON(out)
	if err != nil {
		return nil, err
	}
	if ec, has := data["errcode"]; has && toInt(ec) != 0 {
		return nil, fmt.Errorf("读取字段失败 errcode=%v", ec)
	}
	titles := []string{}
	if arr, ok := data["fields"].([]any); ok {
		for _, f := range arr {
			if m, ok2 := f.(map[string]any); ok2 {
				if t := strings.TrimSpace(toStr(m["field_title"])); t != "" {
					titles = append(titles, t)
				}
			}
		}
	}
	return titles, nil
}
