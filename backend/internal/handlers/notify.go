package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// 企业微信群机器人通知：根据今日班表生成任务提醒，并 @ 当班人员（按手机号）

// todayOnDuty 今日各班次当班人员：shift -> [姓名]（scope>0 仅统计该部门班表）
func todayOnDuty(scope []uint) map[string][]string {
	today := time.Now().Format("2006-01-02")
	// 「今日当班」只算真正上班的人：休息班次不计入当班、不参与推送 @。
	// 口径与 Dashboard 一致（v0.12.7 仅修了 Dashboard，通知侧漏修，
	// 导致休息的人被列进当班列表并收到 @，v0.14.2 补齐）
	q := db.DB.Where("date = ?", today).Where("shift <> ?", "休息")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var schedules []models.Schedule
	q.Find(&schedules)
	byShift := map[string][]string{}
	for _, s := range schedules {
		var people []string
		if err := json.Unmarshal([]byte(s.People), &people); err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, p := range people {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			byShift[s.Shift] = append(byShift[s.Shift], p)
		}
	}
	return byShift
}

// taskShiftPeople 返回某任务应@的当班人员（按任务班次匹配今日班表）
func taskShiftPeople(t models.Task, onDuty map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for _, p := range onDuty[s] {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	switch t.Shift {
	case "早班":
		add("早班")
	case "晚班":
		add("晚班")
	case "早晚":
		add("早班")
		add("晚班")
	case "全员":
		for _, ps := range onDuty {
			for _, p := range ps {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	default:
		add(t.Shift) // 自定义班次（如中班）：按同名匹配班表
	}
	return out
}

// inGroupSet 返回已入群用户姓名集合（这些人会被@，名单中不重复列出）
func inGroupSet() map[string]bool {
	set := map[string]bool{}
	var users []models.User
	db.DB.Where("in_group = ?", true).Find(&users)
	for _, u := range users {
		set[u.Name] = true
		if u.Username != "" {
			set[u.Username] = true
		}
	}
	return set
}

// peopleMobiles 返回企业微信@用的手机号列表
func peopleMobiles(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	var users []models.User
	// v0.15.2：只 @ 已加入通知群的成员（未入群无法被企业微信@到）
	db.DB.Where("name IN ? OR username IN ?", names, names).Where("in_group = ?", true).Find(&users)
	seen := map[string]bool{}
	var mobiles []string
	for _, u := range users {
		m := strings.TrimSpace(u.Mobile)
		if m != "" && !seen[m] {
			seen[m] = true
			mobiles = append(mobiles, m)
		}
	}
	return mobiles
}

// displayPeople 将当班姓名/账号名显示为姓名并按人去重（账号名映射为姓名，避免同人重复列出）
func displayPeople(names []string) []string {
	return displayPeopleEx(names, false)
}

// displayPeopleEx 同 displayPeople；showEmpNo=true 时在姓名后附加工号，如「戴琼照（3275）」
func displayPeopleEx(names []string, showEmpNo bool) []string {
	if len(names) == 0 {
		return nil
	}
	var users []models.User
	db.DB.Where("name IN ? OR username IN ?", names, names).Find(&users)
	shown := map[string]string{}
	empNoOf := map[string]string{}
	for _, u := range users {
		if u.Username != "" && u.Name != "" {
			shown[u.Username] = u.Name
		}
		if u.Name != "" {
			shown[u.Name] = u.Name
			if u.EmpNo != "" {
				empNoOf[u.Name] = u.EmpNo
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		disp := n
		if v, ok := shown[n]; ok && v != "" {
			disp = v
		}
		if seen[disp] {
			continue
		}
		seen[disp] = true
		if showEmpNo {
			if no, ok := empNoOf[disp]; ok && no != "" {
				disp = fmt.Sprintf("%s（%s）", disp, no)
			}
		}
		out = append(out, disp)
	}
	return out
}

// buildReminderContent 生成今日任务提醒文本（scope>0 仅统计该部门任务与班表）
func buildReminderContent(scope []uint) string {
	today := time.Now()
	onDuty := todayOnDuty(scope)
	inGroup := inGroupSet()

	q := db.DB.Order("shift asc, time asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var tasks []models.Task
	q.Find(&tasks)
	// 只取今日应办且未完成的
	var todayTasks []models.Task
	for _, t := range tasks {
		if t.Status == models.TaskStatusDone || !isDueToday(t) {
			continue
		}
		todayTasks = append(todayTasks, t)
	}
	if len(todayTasks) == 0 {
		return ""
	}

	weekName := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[today.Weekday()]
	hr := "────────────────"

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 今日任务提醒 · %d月%d日 %s\n", today.Month(), today.Day(), weekName))
	b.WriteString(hr + "\n")

	// 按班次分组输出：早班 / 晚班 / 早晚 / 全员
	order := []string{"早班", "晚班", "早晚", "全员"}
	total, overdueCnt := 0, 0
	// 跨班次去重：同一个人值多个班次时，只在首次出现的班次列全名，
	// 后续班次标「同上」，避免提醒里同一个名字被反复刷屏
	seenPeople := map[string]bool{}
	for _, shift := range order {
		var items []models.Task
		for _, t := range todayTasks {
			if t.Shift == shift {
				items = append(items, t)
			}
		}
		if len(items) == 0 {
			continue
		}
		names := displayPeopleEx(taskShiftPeople(items[0], onDuty), true)
		if len(names) == 0 {
			b.WriteString(fmt.Sprintf("\n👤 %s 当班：%s\n", shift, "（班表未排班）"))
		} else {
			// v0.15.2：已入群人员会收到@提醒，名单中不重复列出
			var listed []string
			for _, n := range names {
				if !inGroup[n] {
					listed = append(listed, n)
				}
			}
			if len(listed) > 0 {
				var fresh []string
				for _, n := range listed {
					if !seenPeople[n] {
						seenPeople[n] = true
						fresh = append(fresh, n)
					}
				}
				people := strings.Join(fresh, "、")
				if len(fresh) == 0 {
					people = "同上" // 本班次人员在前面的班次里已全部列出
				}
				b.WriteString(fmt.Sprintf("\n👤 %s 当班：%s\n", shift, people))
			}
		}
		for _, t := range items {
			// 时间左对齐补位，保证标题列整齐
			when := "       "
			if t.Time != "" {
				when = t.Time + "  "
			}
			mark := "☐"
			if isOverdue(t) {
				mark = "❗"
				overdueCnt++
			}
			total++
			b.WriteString(fmt.Sprintf("   %s %s%s\n", mark, when, t.Title))
			if t.Note != "" {
				b.WriteString(fmt.Sprintf("         └ %s\n", t.Note))
			}
			// 任务条目之间加空行，避免信息密集、任务难以分辨
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + hr + "\n")
	summary := fmt.Sprintf("共 %d 项待办", total)
	if overdueCnt > 0 {
		summary += fmt.Sprintf(" · 逾期 %d 项", overdueCnt)
	}
	b.WriteString(summary + "\n")
	b.WriteString("完成后记得在系统勾选 ✔")

	return strings.TrimRight(b.String(), "\n")
}

// sendWecomRobot 发送企业微信群机器人消息并 @ 指定手机号
func sendWecomRobot(url, content string, mobiles []string) error {
	payload := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]interface{}{
			"content":             content,
			"mentioned_mobile_list": mobiles,
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.Errcode != 0 {
		return fmt.Errorf("企业微信返回错误 %d: %s", result.Errcode, result.Errmsg)
	}
	return nil
}

// dingtalkSign 钉钉加签：HMAC-SHA256(timestamp + "\n" + secret) base64
func dingtalkSign(secret, timestamp string) string {
	raw := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// sendDingtalk 发送钉钉自定义机器人消息（支持加签）
func sendDingtalk(rawURL, secret, content string) error {
	url := rawURL
	if secret != "" {
		ts := strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10)
		url = fmt.Sprintf("%s&timestamp=%s&sign=%s", rawURL, ts, dingtalkSign(secret, ts))
	}
	payload := map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]interface{}{"content": content},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.Errcode != 0 {
		return fmt.Errorf("钉钉返回错误 %d: %s", result.Errcode, result.Errmsg)
	}
	return nil
}

// feishuSign 飞书加签
func feishuSign(secret, timestamp string) string {
	raw := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// sendFeishu 发送飞书自定义机器人消息（支持加签）
func sendFeishu(rawURL, secret, content string) error {
	url := rawURL
	if secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		url = fmt.Sprintf("%s&timestamp=%s&sign=%s", rawURL, ts, feishuSign(secret, ts))
	}
	payload := map[string]interface{}{
		"msg_type": "text",
		"content":  map[string]interface{}{"text": content},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.Code != 0 {
		return fmt.Errorf("飞书返回错误 %d: %s", result.Code, result.Msg)
	}
	return nil
}

// sendEmailNotify 通过 SMTP 发送今日提醒邮件给配置的通知邮箱
// ErrEmailNotConfigured 未配置 SMTP：邮件通知跳过，既不算成功渠道也不算失败
var ErrEmailNotConfigured = errors.New("未配置 SMTP，跳过邮件通知")

func sendEmailNotify(content string) error {
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	if s.SmtpHost == "" || s.SmtpPort == 0 || s.NotifyEmails == "" {
		// 原先这里返回 nil，调用方会 sent++，导致「推送到 2 个渠道」里
		// 有 1 个其实是没发任何东西的邮件空转，日志和返回值都会虚高
		return ErrEmailNotConfigured
	}
	to := strings.Split(s.NotifyEmails, ",")
	pass, _ := db.Decrypt(s.SmtpPass)
	return sendEmail(s.SmtpHost, s.SmtpPort, s.SmtpUser, pass, s.SmtpFrom, to, "排班工作台 · 今日任务提醒", content)
}

// sendToHookRaw 按类型分发（URL/Secret 为明文）
func sendToHookRaw(rawURL, typ, secret, content string, mobiles []string) error {
	switch typ {
	case "dingtalk":
		return sendDingtalk(rawURL, secret, content)
	case "feishu":
		return sendFeishu(rawURL, secret, content)
	default:
		return sendWecomRobot(rawURL, content, mobiles)
	}
}

// sendToHook 按类型分发到对应渠道（URL/Secret 为密文，需解密）
func sendToHook(h models.Webhook, content string, mobiles []string) error {
	url, err := db.Decrypt(h.URL)
	if err != nil {
		return fmt.Errorf("解密失败")
	}
	secret, _ := db.Decrypt(h.Secret)
	return sendToHookRaw(url, h.Type, secret, content, mobiles)
}

// ancestorDeptIDs 返回部门的祖先部门链（不含自身），如 三亚预订仓(8) → [物流部(7)]
func ancestorDeptIDs(id uint) []uint {
	var out []uint
	cur := id
	for {
		var d models.Department
		if err := db.DB.First(&d, cur).Error; err != nil || d.ParentID == 0 {
			break
		}
		out = append(out, d.ParentID)
		cur = d.ParentID
	}
	return out
}

// hookBelongsToDept 判断 webhook 是否属于目标部门或其祖先部门。
// 业务含义：物流部(7) 的群里要能看到子部门 三亚预订仓(8) 的任务提醒。
func hookBelongsToDept(h models.Webhook, deptID uint) bool {
	if h.DeptID == 0 || h.DeptID == deptID {
		return true
	}
	for _, anc := range ancestorDeptIDs(deptID) {
		if h.DeptID == anc {
			return true
		}
	}
	return false
}

// webhooksForScope 返回范围内可用的 webhook：
// scope 为 nil（超管）时返回全部；否则返回 dept_id 在范围内、或为范围内部门祖先的 webhook。
func webhooksForScope(scope []uint) []models.Webhook {
	var hooks []models.Webhook
	if len(scope) == 0 {
		db.DB.Find(&hooks)
		return hooks
	}
	ok := map[uint]bool{}
	for _, s := range scope {
		ok[s] = true
		for _, a := range ancestorDeptIDs(s) {
			ok[a] = true
		}
	}
	db.DB.Find(&hooks)
	out := hooks[:0]
	for _, h := range hooks {
		if h.DeptID == 0 || ok[h.DeptID] {
			out = append(out, h)
		}
	}
	return out
}

// NotifyTodayHandler POST /api/webhooks/notify 手动推送今日任务提醒到所有 Webhook（按操作者部门隔离）
func NotifyTodayHandler(c *gin.Context) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	scope := deptScopeIDs(c)
	content := buildReminderContent(scope)
	if content == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "sent": 0, "msg": "今日没有待办任务，未发送"})
		return
	}
	// 收集需@人员手机号（所有今日任务涉及当班人员）
	onDuty := todayOnDuty(scope)
	q := db.DB
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var tasks []models.Task
	q.Find(&tasks)
	var allPeople []string
	seen := map[string]bool{}
	for _, t := range tasks {
		if t.Status == models.TaskStatusDone || !isDueToday(t) {
			continue
		}
		for _, p := range taskShiftPeople(t, onDuty) {
			if !seen[p] {
				seen[p] = true
				allPeople = append(allPeople, p)
			}
		}
	}
	mobiles := peopleMobiles(allPeople)

	// 兼容祖先部门：物流部的 webhook 也能收到子部门（三亚预订仓）的任务提醒
	hooks := webhooksForScope(scope)

	sent := 0
	var errs []string
	for _, h := range hooks {
		if err := sendToHook(h, content, mobiles); err != nil {
			errs = append(errs, h.Name+":"+err.Error())
			continue
		}
		sent++
	}
	if e := sendEmailNotify(content); e == nil {
		sent++
	} else if !errors.Is(e, ErrEmailNotConfigured) {
		errs = append(errs, "邮件:"+e.Error())
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, fmt.Sprintf("推送今日任务提醒到 %d 个渠道", sent))
	c.JSON(http.StatusOK, gin.H{"ok": true, "sent": sent, "errors": errs})
}

// StartNotifyScheduler 任务到点推送：每分钟检查，任务到其设置的时间点时推送提醒并 @ 当班人员。
// 每日任务按 time（HH:MM）；单次/月度任务按 deadline（HH:MM）当日到点推送。已完成的跳过。
// 每天 09:00 额外推送一次「今日任务汇总」（自动，无需手动点按钮）。
func StartNotifyScheduler() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var lastMin string
		pushedSummary := map[string]bool{} // key: summary:2026-08-31，避免同一天重复推送
		for range ticker.C {
			now := time.Now()
			mm := now.Format("2006-01-02 15:04")
			if mm == lastMin {
				continue
			}
			lastMin = mm
			// 每天 00:01 兜底执行一次周期重置（接口读取时也会懒执行，这里覆盖无人访问的情况）
			if now.Format("15:04") == "00:01" {
				ResetRecurringTasks()
			}
			pushDueTasks(now)
			// 每天早上 09:00 推送今日任务汇总（含逾期项与当班人员，自动@当班）
			if now.Format("15:04") == "09:00" {
				key := "summary:" + now.Format("2006-01-02")
				if !pushedSummary[key] {
					pushedSummary[key] = true
					pushDailySummary()
				}
			}
		}
	}()
}

// pushDailySummary 每天早上自动推送今日任务汇总到所有 Webhook。
// 汇总按全部门统计（超管视角），内容同手动「推送今日任务提醒」。
func pushDailySummary() {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	content := buildReminderContent(nil)
	if content == "" {
		return
	}
	mobiles := reminderMobiles(nil)
	var hooks []models.Webhook
	db.DB.Find(&hooks)
	sent := 0
	for _, h := range hooks {
		if err := sendToHook(h, content, mobiles); err != nil {
			_ = db.DB.Create(&models.Log{Action: "每日汇总推送失败: " + err.Error()}).Error
			continue
		}
		sent++
	}
	_ = db.DB.Create(&models.Log{Action: fmt.Sprintf("自动推送今日任务汇总到 %d 个渠道", sent)}).Error
}

// reminderMobiles 收集今日待办任务涉及的当班人员手机号（用于自动@）
func reminderMobiles(scope []uint) []string {
	onDuty := todayOnDuty(scope)
	q := db.DB
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var tasks []models.Task
	q.Find(&tasks)
	var allPeople []string
	seen := map[string]bool{}
	for _, t := range tasks {
		if t.Status == models.TaskStatusDone || !isDueToday(t) {
			continue
		}
		for _, p := range taskShiftPeople(t, onDuty) {
			if !seen[p] {
				seen[p] = true
				allPeople = append(allPeople, p)
			}
		}
	}
	return peopleMobiles(allPeople)
}

// pushDueTasks 推送当前分钟到点的任务提醒（部门隔离：任务只推给本部门的 Webhook）
func pushDueTasks(now time.Time) {
	ResetRecurringTasks() // 周期任务跨日/跨月自动回到待办（幂等，同一周期只落库一次）
	hm := now.Format("15:04")
	today := now.Format("2006-01-02")
	var tasks []models.Task
	db.DB.Where("status = ?", models.TaskStatusTodo).Find(&tasks)
	var hooks []models.Webhook
	db.DB.Find(&hooks)
	if len(hooks) == 0 {
		return
	}
	onDuty := todayOnDuty(nil) // 全部门班表：按任务部门匹配 Webhook
	inGroup := inGroupSet()
	for _, t := range tasks {
		due := false
		switch t.Type {
		case models.TaskTypeDaily:
			due = t.Time != "" && t.Time == hm
		case models.TaskTypeOnce:
			if dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local); err == nil {
				due = dl.Format("2006-01-02 15:04") == today+" "+hm
			}
		case models.TaskTypeMonthly:
			if dl, err := time.ParseInLocation("2006-01-02T15:04", t.Deadline, time.Local); err == nil {
				// 按完整日期比较，避免跨月误推（8 月 1 日的任务不该在 9 月 1 日再推）
				due = dl.Format("2006-01-02") == today && dl.Format("15:04") == hm
			}
		}
		if !due {
			continue
		}
		// 艾特对象：当班人员 + 任务负责人
		people := taskShiftPeople(t, onDuty)
		if a := strings.TrimSpace(t.Assignee); a != "" {
			exists := false
			for _, p := range people {
				if p == a {
					exists = true
					break
				}
			}
			if !exists {
				people = append(people, a)
			}
		}
		// v0.15.2：名单只显示未入群的人（@对象由 peopleMobiles 按 in_group 过滤）
		var listed []string
		for _, p := range people {
			if !inGroup[p] {
				listed = append(listed, p)
			}
		}
		mobiles := peopleMobiles(people)
		var b strings.Builder
		b.WriteString(fmt.Sprintf("⏰ 任务到点（%d月%d日 %s）\n", now.Month(), now.Day(), hm))
		b.WriteString(fmt.Sprintf("【%s】%s", t.Shift, t.Title))
		if t.Priority == "high" {
			b.WriteString(" [高优先级]")
		}
		b.WriteString("\n")
		if t.Note != "" {
			b.WriteString("  └ " + t.Note + "\n")
		}
		if len(listed) > 0 {
			b.WriteString("当班：" + strings.Join(displayPeople(listed), "、"))
		}
		content := b.String()
		for _, h := range hooks {
			// 部门隔离：任务只推送到本部门（或全局 dept=0）的 Webhook；
			// v0.12.9：祖先部门的 Webhook 也能收到（物流部群能看到预订仓的任务）
			if !hookBelongsToDept(h, t.DeptID) {
				continue
			}
			if err := sendToHook(h, content, mobiles); err != nil {
				_ = db.DB.Create(&models.Log{Action: "任务到点推送失败: " + err.Error()}).Error
			}
		}
	}
}
