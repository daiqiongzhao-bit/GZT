package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// BroadcastNotification POST /api/notifications/broadcast 给部门（可多选/全选，含子孙）全部成员发广播通知。
// - 超管：多选任意部门，或选 0=全部部门；
// - 部门管理员：多选本人可管理的部门（默认本人部门，含子部门）；
// 冻结账号不接收。广播含发送者本人（区别于 notifyUser 的"自己操作自己不发"）。
func BroadcastNotification(c *gin.Context) {
	var req struct {
		DeptIDs     []uint `json:"dept_ids"` // 多选部门；超管可含 0 表示全部
		DeptID      uint   `json:"dept_id"`  // 兼容旧单值
		Title       string `json:"title"`
		Content     string `json:"content"`
		Link        string `json:"link"` // 附带超链接（可选，仅 http/https）
		RequireAck  bool   `json:"require_ack"`
		Attachments []struct {
			ID uint `json:"id"`
		} `json:"attachments"` // 临时附件 id（KnowledgeTempAttachment）
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
	cl := currentClaims(c)

	// 兼容：旧单值 dept_id 当作只有一个元素的多选
	if len(req.DeptIDs) == 0 && req.DeptID != 0 {
		req.DeptIDs = []uint{req.DeptID}
	}

	// 权限解析：得出 全部门(all) 或 目标部门集合（顶层，发送时含子孙）
	var all bool
	var chosen []uint
	if canManageAny(c) {
		hasAll := false
		for _, d := range req.DeptIDs {
			if d == 0 {
				hasAll = true
				break
			}
		}
		if hasAll {
			all = true
		} else {
			chosen = req.DeptIDs
		}
	} else {
		// 部门管理员：限定可管理范围
		if len(req.DeptIDs) == 0 {
			req.DeptIDs = []uint{cl.DeptID}
		}
		for _, d := range req.DeptIDs {
			target := d
			if target == 0 {
				target = cl.DeptID
			}
			if !canManageDept(c, target) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权给该部门发通知"})
				return
			}
			chosen = append(chosen, target)
		}
	}
	ids := broadcastTargetIDs(all, chosen)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择接收部门"})
		return
	}

	// 处理附件：把临时附件落库为永久通知附件
	attJSON := ""
	if len(req.Attachments) > 0 {
		attIDs := make([]uint, 0, len(req.Attachments))
		for _, a := range req.Attachments {
			if a.ID != 0 {
				attIDs = append(attIDs, a.ID)
			}
		}
		atts, err := persistBcastAttachments(attIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "附件处理失败: " + err.Error()})
			return
		}
		if b, err := json.Marshal(atts); err == nil {
			attJSON = string(b)
		}
	}

	sent, err := broadcastToDeptIDs(cl.UserID, claimsName(cl), req.Title, req.Content, req.Link, req.RequireAck, attJSON, ids)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	deptName := deptNames(ids)
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("广播通知「%s」给 %s（%d 人%s）", req.Title, deptName, sent, ackLabel(req.RequireAck)))
	c.JSON(http.StatusOK, gin.H{"sent": sent, "dept_name": deptName, "attachments": len(req.Attachments)})
}

// ackLabel 日志用：是否需要确认
func ackLabel(r bool) string {
	if r {
		return "，要求确认"
	}
	return ""
}

// broadcastTargetIDs 由「是否全部门 + 用户所选部门」解析出实际应覆盖的部门 id 集合（含子孙，去重）。
// 该函数不含权限判断，调用方须先完成权限校验。
func broadcastTargetIDs(all bool, depts []uint) []uint {
	idset := map[uint]bool{}
	add := func(deptID uint) {
		for _, id := range descendantDeptIDs(deptID) {
			idset[id] = true
		}
	}
	if all {
		var roots []models.Department
		db.DB.Where("parent_id = ? OR parent_id IS NULL", 0).Find(&roots)
		for _, r := range roots {
			add(r.ID)
		}
	} else {
		for _, d := range depts {
			add(d)
		}
	}
	out := make([]uint, 0, len(idset))
	for id := range idset {
		out = append(out, id)
	}
	return out
}

// broadcastToDeptIDs 向「已展开的部门集合」的全部非冻结成员发送广播通知，并触发 Web Push。
// actorID/actorName 为广播发送者（历史/定时广播用 Creator；界面发送用当前用户）。
// 返回实际接收人数。
func broadcastToDeptIDs(actorID uint, actorName, title, content, link string, requireAck bool, attJSON string, deptIDs []uint) (int, error) {
	var users []models.User
	if err := db.DB.Where("dept_id IN ? AND frozen = ?", deptIDs, false).Find(&users).Error; err != nil {
		return 0, err
	}
	if len(users) == 0 {
		return 0, fmt.Errorf("所选部门无有效成员")
	}
	bid := genBroadcastID(actorID)
	now := time.Now()
	rows := make([]models.Notification, 0, len(users))
	for _, u := range users {
		rows = append(rows, models.Notification{
			UserID:      u.ID,
			Kind:        "broadcast",
			BroadcastID: bid,
			Title:       title,
			Content:     content,
			Link:        link,
			Attachments: attJSON,
			RequireAck:  requireAck,
			ActorID:     actorID,
			ActorName:   actorName,
			Read:        false,
			CreatedAt:   now,
		})
	}
	if err := db.DB.Create(&rows).Error; err != nil {
		return 0, err
	}
	// 站内通知已生成，再尝试 Web Push 触达（无订阅自动跳过，不影响主流程）
	for _, u := range users {
		pushToUser(u.ID, title, pushBody(content))
	}
	return len(users), nil
}

// pushBody 生成 Web Push 的正文（截断，避免超长）。
func pushBody(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "您有一条新的站内通知"
	}
	r := []rune(content)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return content
}

// persistBcastAttachments 把中转临时附件复制到通知附件永久目录，返回元数据列表。
// 仅复制、不删除临时文件（临时文件由既有 24h 回收逻辑清理）。
func persistBcastAttachments(tempIDs []uint) ([]models.NotifAttachment, error) {
	if len(tempIDs) == 0 {
		return nil, nil
	}
	var temps []models.KnowledgeTempAttachment
	db.DB.Where("id IN ?", tempIDs).Find(&temps)
	if len(temps) == 0 {
		return nil, nil
	}
	dir := notifyAttachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tDir := tempAttachmentDir()
	out := make([]models.NotifAttachment, 0, len(temps))
	for _, t := range temps {
		src := filepath.Join(tDir, filepath.Base(t.StoredName))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(dir, filepath.Base(t.StoredName))
		if err := copyFile(src, dst); err != nil {
			continue
		}
		out = append(out, models.NotifAttachment{
			FileName:   t.FileName,
			StoredName: t.StoredName,
			Mime:       t.Mime,
			Size:       t.Size,
		})
	}
	return out, nil
}

// deptNames 取部门 ID 集合里顶层部门的名字，用顿号连接，用于日志/提示。
func deptNames(ids []uint) string {
	var depts []models.Department
	db.DB.Where("id IN ?", ids).Find(&depts)
	names := []string{}
	for _, d := range depts {
		if d.ParentID == 0 {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 && len(depts) > 0 {
		names = append(names, depts[0].Name)
	}
	return strings.Join(names, "、")
}

// notifyAttachmentDir 返回通知附件永久存储目录（与数据库同盘，便于随数据目录一起备份/迁移）。
func notifyAttachmentDir() string {
	dir := filepath.Dir(config.C.DBPath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	if dir != "/" {
		dir = strings.TrimRight(dir, "/")
	}
	return filepath.Join(dir, "notify_attachments")
}

// DownloadNotifAttachment GET /api/notifications/attachments/:key/download
// 通知附件下载（接收人本人或超管可见）；stored_name 不可猜测。
func DownloadNotifAttachment(c *gin.Context) {
	key := filepath.Base(c.Param("key"))
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var notifs []models.Notification
	db.DB.Where("attachments LIKE ?", "%"+key+"%").Find(&notifs)
	allowed := cl.Role == models.RoleSuperAdmin
	if !allowed {
		for _, n := range notifs {
			if n.UserID == cl.UserID {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该附件"})
		return
	}
	// 从匹配通知里解析出文件名/类型
	var meta models.NotifAttachment
	found := false
	for _, n := range notifs {
		var list []models.NotifAttachment
		if err := json.Unmarshal([]byte(n.Attachments), &list); err != nil {
			continue
		}
		for _, a := range list {
			if a.StoredName == key {
				meta = a
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	p := filepath.Join(notifyAttachmentDir(), key)
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		return
	}
	if found && meta.Mime != "" {
		c.Header("Content-Type", meta.Mime)
	}
	if found && meta.FileName != "" {
		c.Header("Content-Disposition", "inline; filename*=UTF-8''"+url.QueryEscape(meta.FileName))
	}
	c.File(p)
}

// canManageAny 判断当前用户是否超管（可管理任意部门）。用于广播的目标部门约束。
func canManageAny(c *gin.Context) bool {
	cl := currentClaims(c)
	if cl == nil {
		return false
	}
	return cl.Role == models.RoleSuperAdmin
}

// genBroadcastID 生成一次广播的聚合键，把同一次广播发给多人的通知归为一组，便于做送达/已读统计。
func genBroadcastID(actorID uint) string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("bc_%d_%d_%x", actorID, time.Now().UnixNano(), buf)
}

// ListBroadcastStats GET /api/notifications/broadcasts
// 返回当前用户可见的广播事件及送达/已读/确认统计。超管看全部；部门管理员只看自己发的。
func ListBroadcastStats(c *gin.Context) {
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	q := db.DB.Model(&models.Notification{}).
		Where("kind = ? AND broadcast_id <> ?", "broadcast", "")
	if cl.Role != models.RoleSuperAdmin {
		q = q.Where("actor_id = ?", cl.UserID)
	}
	type statRow struct {
		BroadcastID string
		Title       string
		Content     string
		ActorName   string
		CreatedAt   time.Time
		Total       int
		ReadCount   int
		AckCount    int
		AckReqSum   int // SUM(require_ack)，>0 表示该广播要求确认
	}
	var rows []statRow
	err := q.Select("broadcast_id, MIN(title) as title, MIN(content) as content, MIN(actor_name) as actor_name, created_at, COUNT(*) as total, SUM(CASE WHEN \"read\" THEN 1 ELSE 0 END) as read_count, SUM(CASE WHEN ack THEN 1 ELSE 0 END) as ack_count, SUM(CASE WHEN require_ack THEN 1 ELSE 0 END) as ack_req_sum").
		Group("broadcast_id, created_at").
		Order("created_at DESC").
		Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		read := r.ReadCount
		if read < 0 {
			read = 0
		}
		ack := r.AckCount
		if ack < 0 {
			ack = 0
		}
		total := r.Total
		if total < read {
			total = read
		}
		if total < ack {
			total = ack
		}
		out = append(out, gin.H{
			"broadcast_id": r.BroadcastID,
			"title":        r.Title,
			"content":      r.Content,
			"actor_name":   r.ActorName,
			"created_at":   r.CreatedAt,
			"require_ack":  r.AckReqSum > 0,
			"total":        total,
			"read":         read,
			"unread":       total - read,
			"ack":          ack,
			"no_ack":       total - ack,
		})
	}
	c.JSON(http.StatusOK, out)
}

// BroadcastUnreadList GET /api/notifications/broadcasts/:bid/unread
// 返回某次广播尚未阅读的成员名单（姓名/工号/部门）。超管可看全部；部门管理员仅可看自己发的广播。
func BroadcastUnreadList(c *gin.Context) {
	broadcastMembers(c, false)
}

// BroadcastUnackedList GET /api/notifications/broadcasts/:bid/unacked
// 返回某次广播尚未「确认收到」的成员名单（含已读未确认的人，read 字段区分）。仅当该广播要求确认时前端才会展示。
func BroadcastUnackedList(c *gin.Context) {
	broadcastMembers(c, true)
}

// broadcastMembers 名单查询公共实现：byAck=true 查未确认(ack=false)，false 查未读(read=false)。
func broadcastMembers(c *gin.Context, byAck bool) {
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	bid := c.Param("bid")
	cond := "read = ?"
	val := false
	if byAck {
		cond = "ack = ?"
	}
	q := db.DB.Table("notifications n").
		Joins("JOIN users u ON u.id = n.user_id").
		Joins("LEFT JOIN departments d ON d.id = u.dept_id").
		Where("n.broadcast_id = ? AND n."+cond, bid, val).
		Select("u.name as name, u.emp_no as emp_no, d.name as dept_name, n.read as read")
	if cl.Role != models.RoleSuperAdmin {
		q = q.Where("n.actor_id = ?", cl.UserID)
	}
	var list []struct {
		Name     string `json:"name"`
		EmpNo    string `json:"emp_no"`
		DeptName string `json:"dept_name"`
		Read     bool   `json:"read"`
	}
	if err := q.Order("u.dept_id, u.name").Scan(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// NudgeBroadcast POST /api/notifications/broadcasts/:bid/nudge
// 对某次广播仍未处理（未读；要求确认时含已读未确认）的成员发一条催办通知。
// 仅超管或该广播的发送人可调用。
func NudgeBroadcast(c *gin.Context) {
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	bid := c.Param("bid")
	var orig models.Notification
	if err := db.DB.Where("broadcast_id = ?", bid).Order("id asc").First(&orig).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "广播不存在"})
		return
	}
	if cl.Role != models.RoleSuperAdmin && orig.ActorID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权催办该广播"})
		return
	}
	// 找到仍未处理的人
	q := db.DB.Model(&models.Notification{}).Where("broadcast_id = ?", bid)
	if orig.RequireAck {
		q = q.Where("ack = ?", false)
	} else {
		q = q.Where("read = ?", false)
	}
	var userIDs []uint
	if err := q.Pluck("user_id", &userIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	need := "查看"
	if orig.RequireAck {
		need = "确认收到"
	}
	now := time.Now()
	rows := make([]models.Notification, 0, len(userIDs))
	for _, uid := range userIDs {
		rows = append(rows, models.Notification{
			UserID:    uid,
			Kind:      "broadcast_nudge",
			Title:     "催办：" + trimRune(orig.Title, 60),
			Content:   fmt.Sprintf("您有一条广播通知「%s」仍待%s，请尽快处理。", trimRune(orig.Title, 40), need),
			ActorID:   orig.ActorID,
			ActorName: orig.ActorName,
			Read:      false,
			CreatedAt: now,
		})
	}
	if len(rows) == 0 {
		c.JSON(http.StatusOK, gin.H{"sent": 0, "all_done": true})
		return
	}
	if err := db.DB.Create(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i, uid := range userIDs {
		pushToUser(uid, rows[i].Title, "请尽快处理，点击查看详情")
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("催办广播「%s」（%d 人）", orig.Title, len(rows)))
	c.JSON(http.StatusOK, gin.H{"sent": len(rows), "all_done": false})
}

// trimRune 按 rune 截断字符串（避免截断中文产生乱码）。
func trimRune(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

// BackfillBroadcastIDs 历史数据回填：v0.12.0 之前的广播通知没有 broadcast_id。
// 同一次广播的每条通知 created_at 完全相同（创建时统一取 now），
// 故按 (actor_id,title,content,link,created_at) 聚类回填聚合键，使历史广播也能出现在统计面板。
// 幂等：只处理 broadcast_id 为空的行，可安全在每次启动时调用。
func BackfillBroadcastIDs() {
	type grp struct {
		ActorID   uint
		Title     string
		Content   string
		Link      string
		CreatedAt time.Time
		IDs       string // group_concat(id)
	}
	var gs []grp
	if err := db.DB.Raw(`SELECT actor_id, title, content, link, created_at, group_concat(id) AS ids
		FROM notifications
		WHERE kind = 'broadcast' AND (broadcast_id IS NULL OR broadcast_id = '')
		GROUP BY actor_id, title, content, link, created_at`).Scan(&gs).Error; err != nil {
		return
	}
	for _, g := range gs {
		if g.ActorID == 0 || g.IDs == "" {
			continue
		}
		bid := genBroadcastID(g.ActorID)
		db.DB.Model(&models.Notification{}).
			Where("id IN ? AND (broadcast_id IS NULL OR broadcast_id = '')", strings.Split(g.IDs, ",")).
			Update("broadcast_id", bid)
	}
}
