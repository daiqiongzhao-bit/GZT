package handlers

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ===================== 模板管理 =====================

// taskSampleCSV 任务导入样例 CSV（含表头）
const taskSampleCSV = `标题,班次,类型,时间,优先级,备注,负责人
开门检查,早班,每日,09:00,高,每日开门前安全巡检,
晚班盘点,晚班,每日,21:00,中,,
月底对账,早晚,每月,2026-08-31T17:00,高,当月账务核对,
临时巡检,全员,单次,2026-08-30T15:00,低,,`

// scheduleSampleCSV 班表导入样例 CSV（含表头）
const scheduleSampleCSV = `日期,班次,人员
2026-08-28,早班,林晓;陈默
2026-08-28,晚班,王芳
2026-08-29,早班,林晓`

// DownloadTaskSample GET /api/templates/task-sample 下载任务导入模板样例
func DownloadTaskSample(c *gin.Context) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"task_template.csv\"")
	c.Data(200, "text/csv; charset=utf-8", csvBOM([]byte(taskSampleCSV)))
}

// DownloadScheduleSample GET /api/templates/schedule-sample 下载班表导入模板样例
func DownloadScheduleSample(c *gin.Context) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"schedule_template.csv\"")
	c.Data(200, "text/csv; charset=utf-8", csvBOM([]byte(scheduleSampleCSV)))
}

// ListTemplates GET /api/templates 管理员可查看（任务/班表两类）
func ListTemplates(c *gin.Context) {
	scope := deptScopeIDs(c)
	q := db.DB.Order("type asc, updated_at desc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ? OR dept_id = 0", scope)
	}
	var list []models.Template
	if err := q.Find(&list).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

// UpsertTemplate POST /api/templates 超管可新增/替换模板
func UpsertTemplate(c *gin.Context) {
	var req struct {
		ID      uint   `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Type != "task" && req.Type != "schedule" {
		c.JSON(400, gin.H{"error": "模板类型必须为 task 或 schedule"})
		return
	}
	if req.Name == "" || req.Content == "" {
		c.JSON(400, gin.H{"error": "模板名称与内容均不能为空"})
		return
	}
	cl := currentClaims(c)
	deptID := cl.DeptID
	if cl.Role == models.RoleSuperAdmin {
		deptID = 0 // 超管模板为全局共享
	}
	var t models.Template
	if req.ID > 0 {
		if err := db.DB.First(&t, req.ID).Error; err != nil {
			c.JSON(404, gin.H{"error": "模板不存在"})
			return
		}
		// 仅超管可改
		t.Name = req.Name
		t.Content = req.Content
		t.Type = req.Type
	} else {
		t = models.Template{
			Type:      req.Type,
			Name:      req.Name,
			Content:   req.Content,
			DeptID:    deptID,
			CreatedBy: cl.Username,
		}
	}
	if err := db.DB.Save(&t).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "修改模板: "+req.Name)
	c.JSON(200, t)
}

// DeleteTemplate DELETE /api/templates/:id 超管可删除
func DeleteTemplate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	cl := currentClaims(c)
	var t models.Template
	if err := db.DB.First(&t, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "模板不存在"})
		return
	}
	if err := db.DB.Delete(&t).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "删除模板: "+t.Name)
	c.JSON(200, gin.H{"ok": true})
}

// DownloadTemplate GET /api/templates/:id/download 管理员可下载自定义模板
func DownloadTemplate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	scope := deptScopeIDs(c)
	q := db.DB
	if len(scope) > 0 {
		q = q.Where("dept_id IN ? OR dept_id = 0", scope)
	}
	var t models.Template
	if err := q.First(&t, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "模板不存在"})
		return
	}
	fn := "task_template.csv"
	if t.Type == "schedule" {
		fn = "schedule_template.csv"
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+fn+"\"")
	c.Data(200, "text/csv; charset=utf-8", csvBOM([]byte(t.Content)))
}

// csvBOM 为 CSV 添加 UTF-8 BOM，避免 Excel 中文乱码
func csvBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b
	}
	out := make([]byte, 0, len(b)+3)
	out = append(out, 0xEF, 0xBB, 0xBF)
	return append(out, b...)
}

// ===================== CSV 导入 =====================

// ImportTasksCSV POST /api/tasks/import 接收 multipart CSV，批量建任务
func ImportTasksCSV(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请上传文件（CSV 或 Excel）"})
		return
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "文件读取失败"})
		return
	}
	defer f.Close()
	deptID := parseDeptID(c)
	cl := currentClaims(c)
	if isXLSX(file.Filename) {
		created, failed, errs, colMap := importTasksFromXLSX(c, f, cl, deptID, file.Filename)
		c.JSON(200, gin.H{"created": created, "failed": failed, "errors": errs, "columns": colMap})
		return
	}
	created, failed, errs := importTasksFromCSV(c, f, cl, deptID)
	c.JSON(200, gin.H{"created": created, "failed": failed, "errors": errs})
}

func importTasksFromCSV(c *gin.Context, f multipart.File, cl *models.Claims, deptID uint) (int, int, []string) {
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		return 0, 0, []string{"CSV 解析失败: " + err.Error()}
	}
	super := cl.Role == models.RoleSuperAdmin
	typeMap := map[string]string{"每日": "daily", "每天": "daily", "daily": "daily", "每月": "monthly", "monthly": "monthly", "单次": "once", "临时": "once", "once": "once"}
	shiftMap := map[string]string{"早班": "早班", "晚班": "晚班", "早晚": "早晚", "早晚班": "早晚", "全员": "全员", "所有人": "全员"}
	created, failed := 0, 0
	var errs []string
	started := false
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		if !started {
			// 跳过表头（含“标题”）
			if strings.Contains(row[0], "标题") {
				started = true
				continue
			}
			started = true
		}
		title := strings.TrimSpace(row[0])
		if title == "" {
			continue
		}
		shift := shiftMap[strings.TrimSpace(getCol(row, 1))]
		if shift == "" {
			shift = "全员"
		}
		typ := typeMap[strings.TrimSpace(getCol(row, 2))]
		if typ == "" {
			typ = "daily"
		}
		when := strings.TrimSpace(getCol(row, 3))
		prio := strings.TrimSpace(getCol(row, 4))
		if prio == "" {
			prio = "medium"
		}
		note := strings.TrimSpace(getCol(row, 5))
		assignee := strings.TrimSpace(getCol(row, 6))
		t := models.Task{
			Title:      title,
			Type:       typ,
			Shift:      shift,
			Priority:   prio,
			Note:       note,
			DeptID:     deptID,
			Status:     models.TaskStatusTodo,
			Assignee:   assignee,
			AssigneeID: resolveAssigneeID(assignee, deptID, super),
		}
		if typ == "daily" {
			t.Time = normalizeClock(when)
		} else {
			t.Deadline = normalizeTaskDeadline(typ, when)
		}
		if err := db.DB.Create(&t).Error; err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行[%s]失败: %s", i+1, title, err.Error()))
			continue
		}
		created++
	}
	if created > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("CSV 导入任务 %d 条", created))
	}
	return created, failed, errs
}

func getCol(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

// normalizeClock 归一为 HH:MM，无法识别则返回空。
// 兼容写法：9:00 / 09:00 / 09:00:00 / 9点 / 9:00（Excel「h:mm」格式会给出一位小时）/ 0.375（Excel 时间小数）
func normalizeClock(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Excel 时间小数：一天的比例，0.375 → 09:00
	if v, err := strconv.ParseFloat(s, 64); err == nil && v >= 0 && v < 1 {
		total := int(math.Round(v * 24 * 60))
		return fmt.Sprintf("%02d:%02d", (total/60)%24, total%60)
	}
	s = strings.NewReplacer("：", ":", "点", ":", "时", ":", "分", "", "秒", "").Replace(s)
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return ""
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return ""
	}
	mi := 0
	if p := strings.TrimSpace(parts[1]); p != "" {
		if mi, err = strconv.Atoi(p); err != nil {
			return ""
		}
	}
	if h < 0 || h > 23 || mi < 0 || mi > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", h, mi)
}

// normalizeDeadline 接受 YYYY-MM-DDTHH:MM 或 YYYY-MM-DD HH:MM；只给日期时补 00:00
func normalizeDeadline(s string) string {
	return normalizeDeadlineAt(s, "00:00")
}

// normalizeDeadlineAt 同上，可指定缺省时间（月度任务用 monthlyDueTime = 09:00）
func normalizeDeadlineAt(s string, defTime string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Replace(s, " ", "T", 1)
	if _, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local); err == nil {
		return s
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t.Format("2006-01-02") + "T" + defTime
	}
	return ""
}

// normalizeTaskDeadline 按任务类型归一化截止时间：
// 月度任务缺省时间补 09:00（当天上班后完成即可），其余类型补 00:00
func normalizeTaskDeadline(taskType, s string) string {
	if taskType == models.TaskTypeMonthly {
		return normalizeDeadlineAt(s, monthlyDueTime)
	}
	return normalizeDeadline(s)
}

// ImportSchedulesCSV POST /api/schedules/import 接收 multipart CSV，批量建班表
func ImportSchedulesCSV(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请上传文件（CSV 或 Excel）"})
		return
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "文件读取失败"})
		return
	}
	defer f.Close()
	deptID := parseDeptID(c)
	cl := currentClaims(c)
	if isXLSX(file.Filename) {
		created, failed, errs, unknown, ym := importSchedulesFromXLSX(c, f, cl, deptID, file.Filename)
		c.JSON(200, gin.H{"created": created, "failed": failed, "errors": errs, "unknown_names": unknown, "month": ym})
		return
	}
	created, failed, errs, unknown := importSchedulesFromCSV(c, f, cl, deptID)
	c.JSON(200, gin.H{"created": created, "failed": failed, "errors": errs, "unknown_names": unknown})
}

func importSchedulesFromCSV(c *gin.Context, f multipart.File, cl *models.Claims, deptID uint) (int, int, []string, []string) {
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		return 0, 0, []string{"CSV 解析失败: " + err.Error()}, nil
	}
	created, failed := 0, 0
	var errs []string
	impNames := map[string]bool{}
	started := false
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		if !started {
			if strings.Contains(row[0], "日期") {
				started = true
				continue
			}
			started = true
		}
		date := strings.TrimSpace(getCol(row, 0))
		shift := strings.TrimSpace(getCol(row, 1))
		peopleRaw := strings.TrimSpace(getCol(row, 2))
		if date == "" || shift == "" || peopleRaw == "" {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行字段不完整，已跳过", i+1))
			continue
		}
		// 人员支持 ; 或 , 分隔
		parts := strings.FieldsFunc(peopleRaw, func(r rune) bool { return r == ';' || r == ',' || r == ' ' || r == '、' })
		var people []string
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				people = append(people, p)
			}
		}
		if len(people) == 0 {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行无有效人员，已跳过", i+1))
			continue
		}
		for _, p := range people {
			impNames[p] = true
		}
		// 一人一天只在一个班次：导入即替换该批人员当天其他班次的旧记录
		replacePersonShifts(date, deptID, people, 0)
		peopleJSON, _ := json.Marshal(people)
		s := models.Schedule{Date: date, Shift: shift, People: string(peopleJSON), DeptID: deptID}
		if err := db.DB.Create(&s).Error; err != nil {
			failed++
			errs = append(errs, fmt.Sprintf("第%d行[%s %s]失败: %s", i+1, date, shift, err.Error()))
			continue
		}
		created++
	}
	if created > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("CSV 导入班表 %d 条", created))
		names := make([]string, 0, len(impNames))
		for n := range impNames {
			names = append(names, n)
		}
		notifyPeopleByName(names, "schedule", "班表更新",
			fmt.Sprintf("%s 于 %s 批量更新了你的班表（%d 条）", claimsName(cl), time.Now().Format("2006-01-02 15:04"), created),
			cl.UserID, claimsName(cl))
	}
	return created, failed, errs, unknownScheduleNames(impNames)
}

// ===================== 导出 =====================

// ExportTasksCSV GET /api/tasks/export 导出当前可见部门任务为 CSV
func ExportTasksCSV(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）

	scope := deptScopeIDs(c)
	q := db.DB.Order("created_at desc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var list []models.Task
	if err := q.Find(&list).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var buf bytes.Buffer
	buf.Write(csvBOM(nil))
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ID", "标题", "类型", "班次", "时间", "截止", "优先级", "状态", "负责人", "完成人", "创建时间"})
	for _, t := range list {
		typ := map[string]string{"daily": "每日", "monthly": "每月", "once": "单次"}[t.Type]
		status := map[string]string{"done": "已完成", "todo": "待办"}[t.Status]
		_ = w.Write([]string{
			strconv.Itoa(int(t.ID)), t.Title, typ, t.Shift, t.Time, t.Deadline, t.Priority,
			status, t.Assignee, t.CompletedBy, t.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	w.Flush()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"tasks_export.csv\"")
	c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
}

// ExportLogsCSV GET /api/logs/export 导出操作日志为 CSV（支持与列表一致的筛选）
func ExportLogsCSV(c *gin.Context) {
	q := db.DB.Order("created_at desc")
	if v := c.Query("user_name"); v != "" {
		q = q.Where("user_name LIKE ?", "%"+v+"%")
	}
	if v := c.Query("action"); v != "" {
		q = q.Where("action LIKE ?", "%"+v+"%")
	}
	if v := c.Query("from"); v != "" {
		q = q.Where("created_at >= ?", v)
	}
	if v := c.Query("to"); v != "" {
		q = q.Where("created_at <= ?", v)
	}
	var list []models.Log
	if err := q.Find(&list).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var buf bytes.Buffer
	buf.Write(csvBOM(nil))
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ID", "时间", "操作人", "操作内容"})
	for _, l := range list {
		_ = w.Write([]string{strconv.Itoa(int(l.ID)), l.CreatedAt.Format("2006-01-02 15:04:05"), l.UserName, l.Action})
	}
	w.Flush()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"logs_export.csv\"")
	c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
}

// ExportSchedulesCSV GET /api/schedules/export 导出当前可见部门班表为 CSV
func ExportSchedulesCSV(c *gin.Context) {
	scope := deptScopeIDs(c)
	q := db.DB.Order("date asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var list []models.Schedule
	if err := q.Find(&list).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var buf bytes.Buffer
	buf.Write(csvBOM(nil))
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ID", "日期", "班次", "人员"})
	for _, s := range list {
		var people []string
		_ = json.Unmarshal([]byte(s.People), &people)
		_ = w.Write([]string{strconv.Itoa(int(s.ID)), s.Date, s.Shift, strings.Join(people, ";")})
	}
	w.Flush()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"schedules_export.csv\"")
	c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
}

type batchReq struct {
	IDs    []uint `json:"ids"`
	Action string `json:"action"` // complete | reopen | delete
}

// BatchTasks POST /api/tasks/batch 批量完成/重开/删除
func BatchTasks(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）

	var req batchReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(400, gin.H{"error": "请选择至少一条任务"})
		return
	}
	if req.Action != "complete" && req.Action != "reopen" && req.Action != "delete" {
		c.JSON(400, gin.H{"error": "未知操作"})
		return
	}
	cl := currentClaims(c)
	scope := deptScopeIDs(c)
	done, skipped := 0, 0
	for _, id := range req.IDs {
		var t models.Task
		if err := db.DB.First(&t, id).Error; err != nil {
			skipped++
			continue
		}
		if len(scope) > 0 && !containsUint(scope, t.DeptID) {
			skipped++
			continue
		}
		switch req.Action {
		case "delete":
			if err := db.DB.Delete(&t).Error; err != nil {
				skipped++
				continue
			}
			addLog(c, cl.UserID, cl.Username, "批量删除任务: "+t.Title)
		case "complete":
			if t.Status == models.TaskStatusDone {
				done++
				continue
			}
			t.Status = models.TaskStatusDone
			t.CompletedBy = cl.Username
			now := time.Now()
			t.CompletedAt = now
			db.DB.Save(&t)
			_ = db.DB.Create(&models.TaskCompletion{
				TaskID: t.ID, TaskTitle: t.Title, UserID: cl.UserID, UserName: cl.Username,
				DeptID: t.DeptID, CompletedAt: now,
			}).Error
			addLog(c, cl.UserID, cl.Username, "批量完成任务: "+t.Title)
		case "reopen":
			if t.Status == models.TaskStatusTodo {
				done++
				continue
			}
			t.Status = models.TaskStatusTodo
			t.CompletedBy = ""
			db.DB.Save(&t)
			addLog(c, cl.UserID, cl.Username, "批量重开任务: "+t.Title)
		}
		done++
	}
	c.JSON(200, gin.H{"ok": true, "processed": done, "skipped": skipped})
}

// ===================== 邮件通知配置 =====================

type smtpReq struct {
	SmtpHost     string `json:"smtp_host"`
	SmtpPort     int    `json:"smtp_port"`
	SmtpUser     string `json:"smtp_user"`
	SmtpPass     string `json:"smtp_pass"`
	SmtpFrom     string `json:"smtp_from"`
	NotifyEmails string `json:"notify_emails"`
}

// UpdateSMTP POST /api/settings/smtp 超管配置邮件通知（保存前自动连通性校验）
func UpdateSMTP(c *gin.Context) {
	var req smtpReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请求格式错误"})
		return
	}
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	// 生效配置：新值覆盖旧值；密码留空则沿用已存储的
	host, port := req.SmtpHost, req.SmtpPort
	user, from := req.SmtpUser, req.SmtpFrom
	emails := req.NotifyEmails
	pass := req.SmtpPass
	if pass == "" {
		pass, _ = db.Decrypt(s.SmtpPass)
	}
	if from == "" {
		from = user
	}
	// 保存前连通性校验：主机+端口+接收邮箱齐全时先发测试邮件，失败则不保存
	if host != "" && port != 0 && emails != "" {
		to := strings.Split(emails, ",")
		if err := sendEmail(host, port, user, pass, from, to, "排班工作台配置校验", "✅ 邮件配置保存成功，这是一封自动校验邮件。"); err != nil {
			c.JSON(400, gin.H{"error": "SMTP 校验失败（配置未保存）: " + err.Error()})
			return
		}
	}
	s.SmtpHost = req.SmtpHost
	s.SmtpPort = req.SmtpPort
	s.SmtpUser = req.SmtpUser
	s.SmtpFrom = from
	s.NotifyEmails = req.NotifyEmails
	if req.SmtpPass != "" {
		if enc, e := db.Encrypt(req.SmtpPass); e == nil {
			s.SmtpPass = enc
		}
	}
	if err := db.DB.Save(&s).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "更新邮件通知配置")
	c.JSON(200, gin.H{"ok": true})
}

// UpdateLogRetention POST /api/settings/log-retention 设置审计日志保留天数（仅超管）
func UpdateLogRetention(c *gin.Context) {
	var req struct {
		Days int `json:"days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Days < 0 || req.Days > 3650 {
		c.JSON(400, gin.H{"error": "保留天数需在 0-3650 之间（0 表示永久保留）"})
		return
	}
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	s.LogRetentionDays = req.Days
	if err := db.DB.Save(&s).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, fmt.Sprintf("更新日志保留策略: %d 天", req.Days))
	c.JSON(200, gin.H{"ok": true})
}

// TestEmail POST /api/settings/test-email 发送测试邮件
func TestEmail(c *gin.Context) {
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	if s.SmtpHost == "" || s.SmtpPort == 0 || s.NotifyEmails == "" {
		c.JSON(400, gin.H{"error": "请先完整配置 SMTP 主机、端口与接收邮箱"})
		return
	}
	to := strings.Split(s.NotifyEmails, ",")
	pass, _ := db.Decrypt(s.SmtpPass)
	if err := sendEmail(s.SmtpHost, s.SmtpPort, s.SmtpUser, pass, s.SmtpFrom, to, "排班工作台测试邮件", "这是一封来自排班工作台的测试邮件，配置成功！"); err != nil {
		c.JSON(500, gin.H{"error": "发送失败: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "msg": "测试邮件已发送"})
}

// sendEmail 发送邮件，自动区分 465(隐式TLS) 与 587/25(STARTTLS)
func sendEmail(host string, port int, user, pass, from string, to []string, subject, body string) error {
	var addr string
	if !strings.Contains(host, ":") {
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	} else {
		addr = host
	}
	header := textproto.MIMEHeader{}
	header.Set("From", from)
	header.Set("To", strings.Join(to, ", "))
	header.Set("Subject", subject)
	header.Set("MIME-Version", "1.0")
	header.Set("Content-Type", "text/plain; charset=UTF-8")
	var msg bytes.Buffer
	for k, vv := range header {
		msg.WriteString(k + ": " + vv[0] + "\r\n")
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	auth := smtp.PlainAuth("", user, pass, strings.Split(addr, ":")[0])
	if port == 465 {
		// 隐式 TLS
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		tlsConn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{ServerName: strings.Split(addr, ":")[0]})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(tlsConn, strings.Split(addr, ":")[0])
		if err != nil {
			return err
		}
		defer client.Quit()
		if err := client.Auth(auth); err != nil {
			return err
		}
		if err := client.Mail(from); err != nil {
			return err
		}
		for _, rcpt := range to {
			if err := client.Rcpt(strings.TrimSpace(rcpt)); err != nil {
				return err
			}
		}
		wc, err := client.Data()
		if err != nil {
			return err
		}
		_, err = wc.Write(msg.Bytes())
		if err != nil {
			return err
		}
		return wc.Close()
	}
	// STARTTLS
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Quit()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(nil); err != nil {
			return err
		}
	}
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(strings.TrimSpace(rcpt)); err != nil {
			return err
		}
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	_, err = wc.Write(msg.Bytes())
	if err != nil {
		return err
	}
	return wc.Close()
}

// ===================== 登录失败限流 =====================

type loginAttempt struct {
	count     int
	firstTime time.Time
	lockUntil time.Time
}

var (
	loginMutex    sync.Mutex
	loginAttempts = map[string]*loginAttempt{} // key: username|ip
)

// checkLoginThrottle 校验登录限流：同一 账号+IP 10 分钟内失败 5 次，锁定 15 分钟
// 返回 (locked bool, retryAfter string)
func checkLoginThrottle(key string) (bool, string) {
	loginMutex.Lock()
	defer loginMutex.Unlock()
	now := time.Now()
	a, ok := loginAttempts[key]
	if !ok {
		return false, ""
	}
	if now.Before(a.lockUntil) {
		left := int(a.lockUntil.Sub(now).Seconds())
		return true, strconv.Itoa(left)
	}
	return false, ""
}

// recordLoginFailure 记录一次登录失败
func recordLoginFailure(key string) {
	loginMutex.Lock()
	defer loginMutex.Unlock()
	now := time.Now()
	a, ok := loginAttempts[key]
	if !ok || now.Sub(a.firstTime) > 10*time.Minute {
		loginAttempts[key] = &loginAttempt{count: 1, firstTime: now}
		return
	}
	a.count++
	if a.count >= 5 {
		a.lockUntil = now.Add(15 * time.Minute)
	}
}

// resetLoginFailure 登录成功后清除限流计数
func resetLoginFailure(key string) {
	loginMutex.Lock()
	defer loginMutex.Unlock()
	delete(loginAttempts, key)
}

// UnlockLogin POST /api/auth/unlock 超管手动解除某账号的登录锁定（清除该账号所有 IP 的锁定记录）
func UnlockLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Username) == "" {
		c.JSON(400, gin.H{"error": "请提供要解锁的账号"})
		return
	}
	uname := strings.TrimSpace(req.Username)
	loginMutex.Lock()
	cleared := 0
	for k := range loginAttempts {
		if strings.HasPrefix(k, uname+"|") {
			delete(loginAttempts, k)
			cleared++
		}
	}
	loginMutex.Unlock()
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "解除登录锁定: "+uname)
	c.JSON(200, gin.H{"ok": true, "cleared": cleared})
}

// ===================== 重置密码后端强度校验 =====================

// validPassword 密码至少 6 位
func validPassword(p string) bool {
	return len(p) >= 6
}
