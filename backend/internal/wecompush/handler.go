package wecompush

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var timeRe = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

type taskPayload struct {
	Name       string `json:"name"`
	Enabled    *bool  `json:"enabled"`
	DocID      string `json:"doc_id"`
	SheetTitle string `json:"sheet_title"`
	DateField  string `json:"date_field"`
	OffsetDays int    `json:"offset_days"`
	DateCols   any    `json:"date_cols"`
	// Conditions 支持数组或 JSON 字符串，元素形如 {"field","op","value"}
	Conditions any `json:"conditions"`
	// 兼容旧的单条件写法
	ExtraField string `json:"extra_field"`
	ExtraOp    string `json:"extra_op"`
	ExtraValue string `json:"extra_value"`
	Columns    any    `json:"columns"`
	FilePrefix string `json:"file_prefix"`
	MsgTitle   string `json:"msg_title"`
	EmptyText  string `json:"empty_text"`
	GroupName  string `json:"group_name"`
	SendTime   string `json:"send_time"`
	// v0.40.8：文件名 / 消息模板（空 = 兼容原前缀拼接）
	FileTpl     string `json:"file_name_template"`
	MsgTemplate string `json:"msg_template"`
}

// normalizeConditions 把前端传来的条件统一成 JSON 数组字符串 + 结构化切片
func normalizeConditions(v any) (string, []TaskCondition) {
	var list []TaskCondition
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		list = ParseConditions(x)
	case []any:
		b, _ := json.Marshal(x)
		list = ParseConditions(string(b))
	default:
		return "", nil
	}
	out := make([]TaskCondition, 0, len(list))
	for _, c := range list {
		f := strings.TrimSpace(c.Field)
		op := strings.TrimSpace(c.Op)
		if f == "" || op == "" {
			continue
		}
		out = append(out, TaskCondition{Field: f, Op: op, Value: strings.TrimSpace(c.Value)})
	}
	if len(out) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(out)
	return string(b), out
}

// needCondValue 该运算符是否必须填比较值
func needCondValue(op string) bool {
	switch op {
	case "is_null", "not_null":
		return false
	}
	return true
}

// normalizeList 兼容前端传数组或字符串，统一存成 JSON 数组字符串
func normalizeList(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return NormalizeColumns(x)
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if s := strings.TrimSpace(toStr(it)); s != "" {
				out = append(out, s)
			}
		}
		b, _ := json.Marshal(out)
		return string(b)
	default:
		return ""
	}
}

func (p *taskPayload) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("任务名称不能为空")
	}
	if strings.TrimSpace(p.DocID) == "" {
		return fmt.Errorf("文档 ID 不能为空")
	}
	if strings.TrimSpace(p.SheetTitle) == "" {
		return fmt.Errorf("子表名不能为空")
	}
	if strings.TrimSpace(p.DateField) == "" {
		return fmt.Errorf("日期列不能为空")
	}
	if strings.TrimSpace(p.GroupName) == "" {
		return fmt.Errorf("目标群不能为空")
	}
	st := strings.TrimSpace(p.SendTime)
	if st == "" {
		p.SendTime = "09:00"
	} else if !timeRe.MatchString(st) {
		return fmt.Errorf("执行时间格式应为 HH:MM（24 小时制）")
	}
	// 附加条件：运算符必须受支持；比较类条件必须填值
	_, conds := normalizeConditions(p.Conditions)
	if len(conds) == 0 && strings.TrimSpace(p.ExtraOp) != "" {
		_, conds = normalizeConditions([]any{map[string]any{
			"field": p.ExtraField, "op": p.ExtraOp, "value": p.ExtraValue,
		}})
	}
	for _, c := range conds {
		if !condOps[c.Op] {
			return fmt.Errorf("条件「%s」的运算符不合法：%s", c.Field, c.Op)
		}
		if needCondValue(c.Op) && c.Value == "" {
			return fmt.Errorf("条件「%s」需要填写比较值", c.Field)
		}
	}
	if p.OffsetDays < -365 || p.OffsetDays > 365 {
		return fmt.Errorf("提前/延后天数应在 -365 ~ 365 之间")
	}
	return nil
}

func (p *taskPayload) apply(t *WpTask) {
	t.Name = strings.TrimSpace(p.Name)
	if p.Enabled != nil {
		t.Enabled = *p.Enabled
	}
	t.DocID = ExtractDocID(p.DocID) // v0.40.8：允许粘贴完整链接，统一提取 docid
	t.SheetTitle = strings.TrimSpace(p.SheetTitle)
	t.DateField = strings.TrimSpace(p.DateField)
	t.OffsetDays = p.OffsetDays
	t.DateCols = normalizeList(p.DateCols)
	if t.DateCols == "" {
		t.DateCols = normalizeList([]any{t.DateField})
	}

	// 多条件：新结构优先，旧字段作为兼容
	condJSON, conds := normalizeConditions(p.Conditions)
	if condJSON == "" && strings.TrimSpace(p.ExtraOp) != "" {
		condJSON, conds = normalizeConditions([]any{map[string]any{
			"field": p.ExtraField, "op": p.ExtraOp, "value": p.ExtraValue,
		}})
	}
	t.Conditions = condJSON
	if len(conds) > 0 {
		// 同步镜像到旧字段，便于直接看库/兼容旧版本
		t.ExtraField, t.ExtraOp, t.ExtraValue = conds[0].Field, conds[0].Op, conds[0].Value
	} else {
		t.ExtraField, t.ExtraOp, t.ExtraValue = "", "", ""
	}
	t.Columns = normalizeList(p.Columns)
	t.FilePrefix = strings.TrimSpace(p.FilePrefix)
	t.MsgTitle = strings.TrimSpace(p.MsgTitle)
	// v0.40.8：模板字段（文件名做安全清洗；消息模板仅裁空白，正文允许任意字符）
	if ft := strings.TrimSpace(p.FileTpl); ft != "" {
		t.FileTpl = SanitizeFileName(ft)
	}
	t.MsgTemplate = strings.TrimSpace(p.MsgTemplate)
	t.EmptyText = strings.TrimSpace(p.EmptyText)
	t.GroupName = strings.TrimSpace(p.GroupName)
	t.SendTime = strings.TrimSpace(p.SendTime)
}

func (h *H) ListTasks(c *gin.Context) {
	var items []WpTask
	if err := h.DB.Order("id desc").Find(&items).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, items)
}

func (h *H) CreateTask(c *gin.Context) {
	var p taskPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, 400, "参数格式不正确")
		return
	}
	if err := p.validate(); err != nil {
		fail(c, 400, err.Error())
		return
	}
	t := &WpTask{Enabled: true}
	p.apply(t)
	if err := h.DB.Create(t).Error; err != nil {
		fail(c, 500, "保存失败："+err.Error())
		return
	}
	ok(c, t)
}

func (h *H) UpdateTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t WpTask
	if err := h.DB.First(&t, id).Error; err != nil {
		fail(c, 404, "任务不存在")
		return
	}
	var p taskPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, 400, "参数格式不正确")
		return
	}
	if err := p.validate(); err != nil {
		fail(c, 400, err.Error())
		return
	}
	p.apply(&t)
	if err := h.DB.Save(&t).Error; err != nil {
		fail(c, 500, "保存失败："+err.Error())
		return
	}
	ok(c, t)
}

func (h *H) DeleteTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.DB.Delete(&WpTask{}, id).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

// ToggleTask 只切换启用状态
func (h *H) ToggleTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t WpTask
	if err := h.DB.First(&t, id).Error; err != nil {
		fail(c, 404, "任务不存在")
		return
	}
	h.DB.Model(&t).UpdateColumn("enabled", !t.Enabled)
	t.Enabled = !t.Enabled
	ok(c, t)
}

type runReq struct {
	DryRun bool `json:"dry_run"`
}

// RunTask 立即执行一次（同步返回结果，前端超时已放宽）
func (h *H) RunTask(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t WpTask
	if err := h.DB.First(&t, id).Error; err != nil {
		fail(c, 404, "任务不存在")
		return
	}
	var req runReq
	_ = c.ShouldBindJSON(&req)

	entry := h.ExecuteTask(&t, "manual", req.DryRun)
	status := http.StatusOK
	if entry.Status == "failed" {
		status = http.StatusBadGateway
	}
	c.JSON(status, entry)
}

// PreviewSQL 返回将要执行的 SQL，便于用户核对筛选逻辑
func (h *H) PreviewSQL(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t WpTask
	if err := h.DB.First(&t, id).Error; err != nil {
		fail(c, 404, "任务不存在")
		return
	}
	loc := h.Cfg.Loc
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	target := time.Now().In(loc).AddDate(0, 0, t.OffsetDays).Format("2006-01-02")
	sql, err := BuildSelectSQL(DateQuery{
		DocID: t.DocID, SheetTitle: t.SheetTitle, DateField: t.DateField,
		Columns:    t.Columns,
		Conditions: ParseConditions(t.Conditions),
		Today:      time.Now().In(loc).Format("2006-01-02"),
		Loc:        loc,
		ExtraField: t.ExtraField, ExtraOp: t.ExtraOp, ExtraValue: t.ExtraValue,
	}, target)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"sql": sql, "target_date": target})
}

func (h *H) ListLogs(c *gin.Context) {
	q := h.DB.Model(&WpLog{}).Order("id desc")
	if tid := c.Query("task_id"); tid != "" && tid != "0" {
		q = q.Where("task_id = ?", tid)
	}
	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var items []WpLog
	if err := q.Limit(limit).Find(&items).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, items)
}

// DownloadFile 下载某次运行生成的 Excel（运行日志「文件」列的可点击下载后端）。
// v0.40.8：文件本来就落在 OutDir 里，但一直没有取回入口；这里按文件名从导出目录读取。
// 安全：filepath.Base 剥掉任何路径成分 + 仅允许 .xlsx + 必须真实存在。
func (h *H) DownloadFile(c *gin.Context) {
	name := filepath.Base(c.Param("name"))
	if name == "" || name == "." || name == "/" || !strings.HasSuffix(strings.ToLower(name), ".xlsx") {
		fail(c, 400, "文件名不合法")
		return
	}
	path := h.Cfg.OutPath(name)
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		fail(c, 404, "文件不存在或已清理")
		return
	}
	// 中文文件名双写（RFC5987），与系统其它下载一致，避免乱码/下划线
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, name, url.QueryEscape(name)))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.File(path)
}

// Summary 概览页统计
func (h *H) Summary(c *gin.Context) {
	loc := h.Cfg.Loc
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	y, m, d := time.Now().In(loc).Date()
	todayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)

	var taskTotal, taskEnabled int64
	h.DB.Model(&WpTask{}).Count(&taskTotal)
	h.DB.Model(&WpTask{}).Where("enabled = ?", true).Count(&taskEnabled)

	var todayRuns int64
	h.DB.Model(&WpLog{}).
		Where("run_at >= ? AND trigger IN (?,?,?)", todayStart, "schedule", "manual", "catchup").
		Count(&todayRuns)
	var sum struct{ Total int64 }
	h.DB.Model(&WpLog{}).
		Select("COALESCE(SUM(count),0) AS total").
		Where("run_at >= ? AND status = ? AND trigger <> ?", todayStart, "success", "dry-run").
		Scan(&sum)
	todayPushed := sum.Total

	var recent []WpLog
	h.DB.Order("id desc").Limit(8).Find(&recent)

	var tasks []WpTask
	h.DB.Where("enabled = ?", true).Find(&tasks)
	nextAt, nextTask := nextRun(tasks, loc)

	st := h.CLIStatus()
	ok(c, gin.H{
		"task_total":   taskTotal,
		"task_enabled": taskEnabled,
		"today_runs":   todayRuns,
		"today_pushed": todayPushed,
		"recent_logs":  recent,
		"next_run_at":  nextAt,
		"next_task":    nextTask,
		"cli":          st,
		"server_time":  time.Now().In(loc).Format("2006-01-02 15:04:05"),
	})
}

// nextRun 计算所有启用任务里最近的一次触发时刻
func nextRun(tasks []WpTask, loc *time.Location) (string, string) {
	var best time.Time
	var name string
	now := time.Now().In(loc)
	for _, t := range tasks {
		parts := strings.Split(t.SendTime, ":")
		if len(parts) != 2 {
			continue
		}
		hh, _ := strconv.Atoi(parts[0])
		mm, _ := strconv.Atoi(parts[1])
		cand := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
		if !cand.After(now) {
			cand = cand.AddDate(0, 0, 1)
		}
		if best.IsZero() || cand.Before(best) {
			best = cand
			name = t.Name
		}
	}
	if best.IsZero() {
		return "", ""
	}
	return best.Format("2006-01-02 15:04"), name
}
