package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/logger"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ===================== 定时广播（v0.13.0） =====================
// 到点自动向目标部门发送站内广播。Repeat: once 单次 / daily 每天 / weekly 按周。
// SendAt 记录「下次触发时间」，发送成功后按 repeat 前移；调度器为单 goroutine 顺序执行，
// 天然避免重复触发。

// CreateScheduledBroadcast POST /api/scheduled-broadcasts 预约一条定时广播（部门管/超管）
func CreateScheduledBroadcast(c *gin.Context) {
	var req struct {
		DeptIDs    []uint `json:"dept_ids"`
		All        bool   `json:"all"` // 仅超管可用
		Title      string `json:"title"`
		Content    string `json:"content"`
		Link       string `json:"link"`
		RequireAck bool   `json:"require_ack"`
		SendAt     string `json:"send_at"` // RFC3339 或 "YYYY-MM-DD HH:MM"
		Repeat     string `json:"repeat"`  // once|daily|weekly
		WeekDays   []int  `json:"week_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Link = sanitizeLink(req.Link)
	if req.Title == "" || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题与内容均必填"})
		return
	}
	if req.Repeat == "" {
		req.Repeat = "once"
	}
	if req.Repeat != "once" && req.Repeat != "daily" && req.Repeat != "weekly" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repeat 仅支持 once/daily/weekly"})
		return
	}
	if req.Repeat == "weekly" && len(req.WeekDays) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "按周重复需勾选星期"})
		return
	}
	// 解析发送时间：兼容 RFC3339 与本地格式
	sendAt, perr := parseFlexTime(req.SendAt)
	if perr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "发送时间格式错误（示例 2026-09-10 08:00）"})
		return
	}

	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// 权限：超管可 all / 任意部门；部门管理员仅可本部门及子孙，且不可 all
	if req.All && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权发送全部部门"})
		return
	}
	chosen := req.DeptIDs
	if !req.All && cl.Role != models.RoleSuperAdmin {
		if len(chosen) == 0 {
			chosen = []uint{cl.DeptID}
		}
		for _, d := range chosen {
			if d == 0 {
				d = cl.DeptID
			}
			if !canManageDept(c, d) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权给该部门预约广播"})
				return
			}
		}
	}
	idsB, _ := json.Marshal(chosen)
	wd := make([]string, 0, len(req.WeekDays))
	for _, w := range req.WeekDays {
		if w >= 1 && w <= 7 {
			wd = append(wd, strconv.Itoa(w))
		}
	}
	row := models.ScheduledBroadcast{
		Title:       req.Title,
		Content:     req.Content,
		Link:        req.Link,
		RequireAck:  req.RequireAck,
		AllDepts:    req.All,
		DeptIDs:     string(idsB),
		Repeat:      req.Repeat,
		WeekDays:    strings.Join(wd, ","),
		SendAt:      sendAt,
		CreatorID:   cl.UserID,
		CreatorName: claimsName(cl),
		Active:      true,
	}
	if err := db.DB.Create(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("预约定时广播「%s」于 %s（%s）", req.Title, row.SendAt.Format("2006-01-02 15:04"), row.Repeat))
	c.JSON(http.StatusOK, scheduledBroadcastView(row))
}

// parseFlexTime 解析发送时间：优先 RFC3339，其次本地格式 "YYYY-MM-DD HH:MM"
func parseFlexTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad format")
}

// ListScheduledBroadcasts GET /api/scheduled-broadcasts 待发列表（超管全部；部门管理员看自己预约的）
func ListScheduledBroadcasts(c *gin.Context) {
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	q := db.DB.Model(&models.ScheduledBroadcast{})
	if cl.Role != models.RoleSuperAdmin {
		q = q.Where("creator_id = ?", cl.UserID)
	}
	var rows []models.ScheduledBroadcast
	if err := q.Order("active desc, send_at asc").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, scheduledBroadcastView(r))
	}
	c.JSON(http.StatusOK, out)
}

// scheduledBroadcastView 输出前端视图（补部门名展示）
func scheduledBroadcastView(r models.ScheduledBroadcast) gin.H {
	var ids []uint
	_ = json.Unmarshal([]byte(r.DeptIDs), &ids)
	name := "全部部门"
	if !r.AllDepts {
		name = deptNames(broadcastTargetIDs(false, ids))
		if name == "" {
			name = "未指定"
		}
	}
	view := gin.H{
		"id":          r.ID,
		"title":       r.Title,
		"content":     r.Content,
		"link":        r.Link,
		"require_ack": r.RequireAck,
		"all":         r.AllDepts,
		"dept_name":   name,
		"repeat":      r.Repeat,
		"week_days":   r.WeekDays,
		"send_at":     r.SendAt,
		"creator":     r.CreatorName,
		"active":      r.Active,
	}
	return view
}

// DeleteScheduledBroadcast DELETE /api/scheduled-broadcasts/:id 取消定时广播（超管或创建人）
func DeleteScheduledBroadcast(c *gin.Context) {
	cl := currentClaims(c)
	id, _ := strconv.Atoi(c.Param("id"))
	var row models.ScheduledBroadcast
	if err := db.DB.First(&row, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "定时广播不存在"})
		return
	}
	if cl.Role != models.RoleSuperAdmin && row.CreatorID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该预约"})
		return
	}
	db.DB.Delete(&models.ScheduledBroadcast{}, id)
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("取消定时广播「%s」", row.Title))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// StartScheduledBroadcastScheduler 定时广播调度器：单 goroutine 每 30s 检查一次，
// SendAt 已到的就发送并把 SendAt 前移到下一次（once 发送后停用）。
func StartScheduledBroadcastScheduler() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("schedule", "定时广播调度器发生 panic: %v", r)
		}
	}()
	for {
		now := time.Now()
		var due []models.ScheduledBroadcast
		if err := db.DB.Where("active = ? AND send_at <= ?", true, now).Find(&due).Error; err == nil {
			for i := range due {
				fireScheduledBroadcast(&due[i])
			}
		}
		time.Sleep(30 * time.Second)
	}
}

// fireScheduledBroadcast 触发一条到期定时广播，并推进/停用。
func fireScheduledBroadcast(b *models.ScheduledBroadcast) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("schedule", "触发定时广播「%s」panic: %v", b.Title, r)
		}
	}()
	ids := broadcastTargetIDs(b.AllDepts, mustUintList(b.DeptIDs))
	sent, err := broadcastToDeptIDs(b.CreatorID, b.CreatorName, b.Title, b.Content, b.Link, b.RequireAck, "", ids)
	upd := map[string]interface{}{}
	if err != nil {
		logger.Error("schedule", "定时广播「%s」发送失败: %v", b.Title, err)
	} else {
		logger.Info("schedule", "定时广播「%s」已发送 %d 人", b.Title, sent)
	}
	// 推进下一次触发时间
	switch b.Repeat {
	case "daily":
		upd["send_at"] = b.SendAt.AddDate(0, 0, 1)
		upd["active"] = true
	case "weekly":
		upd["send_at"] = nextWeekly(b.SendAt, b.WeekDays)
		upd["active"] = true
	default: // once：无论成败，到点即停（避免无限重试）
		upd["active"] = false
	}
	db.DB.Model(&models.ScheduledBroadcast{}).Where("id = ?", b.ID).Updates(upd)
}

// mustUintList 解析 DeptIDs JSON
func mustUintList(s string) []uint {
	var out []uint
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// nextWeekly 求下一个匹配星期（1=周一…7=周日）的同一时刻；不匹配则兜底明天。
func nextWeekly(from time.Time, weekDays string) time.Time {
	wds := map[int]bool{}
	for _, p := range strings.Split(weekDays, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n >= 1 && n <= 7 {
			wds[n] = true
		}
	}
	t := from.AddDate(0, 0, 1)
	for i := 0; i < 7; i++ {
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7 // 周日
		}
		if wds[wd] {
			return time.Date(t.Year(), t.Month(), t.Day(), from.Hour(), from.Minute(), 0, 0, from.Location())
		}
		t = t.AddDate(0, 0, 1)
	}
	return from.AddDate(0, 0, 1)
}
