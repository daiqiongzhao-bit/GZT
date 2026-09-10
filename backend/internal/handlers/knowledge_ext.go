package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ==================== 知识库升级：检索 / 标签 / 回收站 / 统计 / 评论 / 版本 / 模板 / 导入导出 / 双向链接 ====================
// 本文件只在「迷你知识库」模块内新增能力，不改动任务/排班/通知/日志/交接/广播等其他模块。

// ---------- 1. FTS5 全文检索（中文按字分词） ----------

// isCJK 判定一个字符是否属于 CJK/全角范围（统一按单字切分，便于中文子串检索）
func isCJK(r rune) bool {
	return r >= 0x2E80 && r <= 0xFFEF
}

// ftsTokens 将文本切成 FTS5 token：CJK 逐字、连续字母数字成词、其余作为分隔。
func ftsTokens(s string) []string {
	var toks []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			toks = append(toks, buf.String())
			buf.Reset()
		}
	}
	for _, r := range s {
		if isCJK(r) {
			flush()
			toks = append(toks, string(r))
		} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			buf.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

// ftsJoin 把文本预处理成 FTS5 索引串（CJK 逐字、词以空格分隔）
func ftsJoin(s string) string {
	return strings.Join(ftsTokens(s), " ")
}

// ftsMatchQuery 把用户关键词构造成安全的 FTS5 MATCH 串（每个 token 双引号包裹，规避特殊字符）
func ftsMatchQuery(kw string) string {
	toks := ftsTokens(kw)
	if len(toks) == 0 {
		return ""
	}
	qs := make([]string, 0, len(toks))
	for _, t := range toks {
		t = strings.ReplaceAll(t, `"`, `""`)
		qs = append(qs, `"`+t+`"`)
	}
	return strings.Join(qs, " ")
}

// ensureFTS 创建 knowledge_fts 虚拟表（若不存在）
func ensureFTS() {
	db.DB.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(title, content, tags, tokenize='unicode61')`)
}

// ftsUpsert 重建某条目的 FTS 索引
func ftsUpsert(e models.KnowledgeEntry) {
	db.DB.Exec("DELETE FROM knowledge_fts WHERE rowid = ?", e.ID)
	db.DB.Exec("INSERT INTO knowledge_fts(rowid, title, content, tags) VALUES (?,?,?,?)",
		e.ID, ftsJoin(e.Title), ftsJoin(e.Content), ftsJoin(e.Tags))
}

// ftsDelete 删除某条目的 FTS 索引
func ftsDelete(id uint) {
	db.DB.Exec("DELETE FROM knowledge_fts WHERE rowid = ?", id)
}

// initKnowledgeSearch 启动时确保 FTS 表存在并回填存量数据（仅当索引为空时）
func initKnowledgeSearch() {
	ensureFTS()
	var cnt int64
	db.DB.Raw("SELECT count(*) FROM knowledge_fts").Scan(&cnt)
	if cnt > 0 {
		return
	}
	var entries []models.KnowledgeEntry
	db.DB.Find(&entries) // 带 DeletedAt 模型自动排除软删
	for _, e := range entries {
		ftsUpsert(e)
	}
}

// InitKnowledge 知识库升级模块初始化入口（由 main.go 在 db.Init 之后调用）
func InitKnowledge() {
	initKnowledgeSearch()
	seedKnowledgeTemplates()
	// 引用关系仅当为空时重建，避免每次启动都全量扫描（日常保存已实时维护）
	var linkCnt int64
	db.DB.Model(&models.KnowledgeLink{}).Count(&linkCnt)
	if linkCnt == 0 {
		rebuildAllLinks()
	}
}

// ---------- 2. 标签 / 标签筛选 ----------

// parseTags 解析 tags 字段（JSON 数组优先，退化按逗号分隔）
func parseTags(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, t := range arr {
			t = strings.TrimSpace(t)
			if t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normTags 把字符串切片规范化为去重后的 JSON 数组串
func normTags(arr []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(arr))
	for _, t := range arr {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// normKbScope 归一化知识条目可见范围（v0.16.0）。
// 合法值：private（仅自己）/ department（同部门）/ public（全公司）。
// public 仅超级管理员可设置，普通用户即便传 public 也会降级为 department；
// 空串或非法值一律按 department（与历史默认行为一致）。
func normKbScope(cl *models.Claims, v string) string {
	switch strings.TrimSpace(v) {
	case string(models.ScopePrivate):
		return string(models.ScopePrivate)
	case string(models.ScopePublic):
		if cl != nil && cl.Role == models.RoleSuperAdmin {
			return string(models.ScopePublic)
		}
		return string(models.ScopeDepartment)
	default:
		return string(models.ScopeDepartment)
	}
}

// ListKnowledgeTags 当前可见范围内的全部标签（去重，用于筛选）
func ListKnowledgeTags(c *gin.Context) {
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	var entries []models.KnowledgeEntry
	q.Find(&entries)
	set := map[string]bool{}
	for _, e := range entries {
		for _, t := range parseTags(e.Tags) {
			set[t] = true
		}
	}
	tags := make([]string, 0, len(set))
	for t := range set {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	c.JSON(http.StatusOK, tags)
}

// ---------- 3. 详情（含阅读量自增） ----------

// GetKnowledge GET /workspace/knowledge/:id 知识条目详情，打开即 +1 阅读量
func GetKnowledge(c *gin.Context) {
	e, ok := loadVisibleKnowledge(c)
	if !ok {
		return
	}
	db.DB.Model(&models.KnowledgeEntry{}).Where("id = ?", e.ID).
		UpdateColumn("view_count", gorm.Expr("view_count + 1"))
	var fresh models.KnowledgeEntry
	db.DB.First(&fresh, e.ID)
	c.JSON(http.StatusOK, fresh)
}

// ---------- 4. 回收站（软删除） ----------

// ListKnowledgeTrash GET /workspace/knowledge/trash 当前可见范围内的回收站条目
func ListKnowledgeTrash(c *gin.Context) {
	q := db.DB.Unscoped().Model(&models.KnowledgeEntry{}).Where("deleted_at IS NOT NULL")
	q = scopeVisibleQ(c, q)
	var list []models.KnowledgeEntry
	q.Order("deleted_at desc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// RestoreKnowledge POST /workspace/knowledge/:id/restore 从回收站恢复
func RestoreKnowledge(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var entry models.KnowledgeEntry
	if err := db.DB.Unscoped().First(&entry, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return
	}
	if entry.OwnerID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅创建者可恢复"})
		return
	}
	db.DB.Unscoped().Model(&entry).Update("deleted_at", nil)
	ftsUpsert(entry)
	refreshKnowledgeLinks(entry)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// purgeKnowledgeEntry 彻底删除一条知识条目及其全部附属数据：
// 附件文件、附件记录、评论、历史版本、双向链接、FTS 索引，最后删条目本身。
// 权限判断由调用方负责。
func purgeKnowledgeEntry(entry models.KnowledgeEntry) {
	// 物理删除附件文件
	var atts []models.KnowledgeAttachment
	db.DB.Unscoped().Where("entry_id = ?", entry.ID).Find(&atts)
	for _, a := range atts {
		if a.StoredName != "" {
			_ = removeFileSafe(filepathJoin(kAttachmentDir(), a.StoredName))
		}
	}
	db.DB.Unscoped().Where("entry_id = ?", entry.ID).Delete(&models.KnowledgeAttachment{})
	db.DB.Unscoped().Where("entry_id = ?", entry.ID).Delete(&models.KnowledgeComment{})
	db.DB.Unscoped().Where("entry_id = ?", entry.ID).Delete(&models.KnowledgeVersion{})
	db.DB.Unscoped().Where("source_id = ? OR target_id = ?", entry.ID, entry.ID).Delete(&models.KnowledgeLink{})
	ftsDelete(entry.ID)
	db.DB.Unscoped().Delete(&entry)
}

// PurgeKnowledge DELETE /workspace/knowledge/:id/purge 从回收站彻底删除（含附件/评论/版本/链接）
func PurgeKnowledge(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var entry models.KnowledgeEntry
	if err := db.DB.Unscoped().First(&entry, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return
	}
	if entry.OwnerID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅创建者可彻底删除"})
		return
	}
	purgeKnowledgeEntry(entry)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// EmptyKnowledgeTrash POST /workspace/knowledge/trash/empty 清空回收站（仅本人创建的/超管全部）
func EmptyKnowledgeTrash(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Unscoped().Model(&models.KnowledgeEntry{}).Where("deleted_at IS NOT NULL")
	if cl.Role != models.RoleSuperAdmin {
		q = q.Where("owner_id = ?", cl.UserID)
	}
	var list []models.KnowledgeEntry
	q.Find(&list)
	for _, entry := range list {
		purgeKnowledgeEntry(entry)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "purged": len(list)})
}

// ---------- 5. 置顶 / 收藏切换 ----------

// ToggleKnowledgePin POST /workspace/knowledge/:id/pin 切换置顶（仅创建者可置顶）
func ToggleKnowledgePin(c *gin.Context) {
	e, ok := loadOwnedKnowledge(c)
	if !ok {
		return
	}
	next := 1 - e.Pinned
	db.DB.Model(&e).Update("pinned", next)
	c.JSON(http.StatusOK, gin.H{"pinned": next})
}

// ToggleKnowledgeStar POST /workspace/knowledge/:id/star 切换收藏（任何人可见即可收藏，作为个人书签）
func ToggleKnowledgeStar(c *gin.Context) {
	e, ok := loadVisibleKnowledge(c)
	if !ok {
		return
	}
	next := 1 - e.Starred
	db.DB.Model(&e).Update("starred", next)
	c.JSON(http.StatusOK, gin.H{"starred": next})
}

// ---------- 6. 统计看板 ----------

type kv struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// topN 取 map 中计数最高的 n 项
func topN(m map[string]int, n int) []kv {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	out := make([]kv, 0, len(keys))
	for _, k := range keys {
		out = append(out, kv{Name: k, Count: m[k]})
	}
	return out
}

// KnowledgeStats GET /workspace/knowledge/stats 知识库统计看板（可见范围内）
func KnowledgeStats(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	var entries []models.KnowledgeEntry
	q.Find(&entries)

	total := len(entries)
	catCount := map[string]int{}
	tagCount := map[string]int{}
	authorCount := map[string]int{}
	recent := 0
	myStarred := 0
	cutoff := time.Now().AddDate(0, 0, -30)
	for _, e := range entries {
		cat := e.Category
		if cat == "" {
			cat = "未分类"
		}
		catCount[cat]++
		for _, t := range parseTags(e.Tags) {
			tagCount[t]++
		}
		authorCount[e.OwnerName]++
		if e.CreatedAt.After(cutoff) {
			recent++
		}
		if cl != nil && e.Starred == 1 && (e.OwnerID == cl.UserID || cl.Role == models.RoleSuperAdmin) {
			myStarred++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"total":      total,
		"recent_30d": recent,
		"my_starred": myStarred,
		"categories": topN(catCount, 8),
		"tags":       topN(tagCount, 12),
		"authors":    topN(authorCount, 5),
	})
}

// ---------- 7. 协作评论 + @通知 ----------

// ListKnowledgeComments GET /workspace/knowledge/:id/comments 评论列表（含楼中楼，按时间正序）
func ListKnowledgeComments(c *gin.Context) {
	if _, ok := loadVisibleKnowledge(c); !ok {
		return
	}
	var list []models.KnowledgeComment
	db.DB.Where("entry_id = ?", c.Param("id")).Order("id asc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// CreateKnowledgeComment POST /workspace/knowledge/:id/comments 发表评论（解析 @成员触发通知）
func CreateKnowledgeComment(c *gin.Context) {
	e, ok := loadVisibleKnowledge(c)
	if !ok {
		return
	}
	cl := middleware.GetClaims(c)
	var req struct {
		Content  string `json:"content"`
		ParentID uint   `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评论内容不能为空"})
		return
	}
	cm := models.KnowledgeComment{
		EntryID:  e.ID,
		ParentID: req.ParentID,
		UserID:   cl.UserID,
		UserName: cl.Username,
		Content:  req.Content,
	}
	if err := db.DB.Create(&cm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	mentionComment(c, e, cm, cl)
	addLog(c, cl.UserID, cl.Username, "知识评论: "+e.Title)
	c.JSON(http.StatusOK, cm)
}

// DeleteKnowledgeComment DELETE /workspace/knowledge/comments/:cid 删除评论（本人或超管）
func DeleteKnowledgeComment(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var cm models.KnowledgeComment
	if err := db.DB.First(&cm, c.Param("cid")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "评论不存在"})
		return
	}
	if cm.UserID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅本人或超管可删除"})
		return
	}
	db.DB.Delete(&cm)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// mentionComment 解析评论中的 @姓名 并给对应成员发站内通知
func mentionComment(c *gin.Context, e *models.KnowledgeEntry, cm models.KnowledgeComment, cl *models.Claims) {
	re := regexp.MustCompile(`@([^\s@，。、：:!！?？]+)`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(cm.Content, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		var us []models.User
		db.DB.Where("name = ?", name).Find(&us)
		for _, u := range us {
			if u.ID == cl.UserID {
				continue
			}
			db.DB.Create(&models.Notification{
				UserID:     u.ID,
				Kind:       "user",
				Title:      "有人在知识库「" + e.Title + "」提到了你",
				Content:    cl.Username + " 评论：" + clipRunes(cm.Content, 120),
				ActorID:    cl.UserID,
				ActorName:  cl.Username,
			})
		}
	}
}

// ---------- 8. 版本快照 + 回滚 ----------

// snapshotVersion 在每次保存时写一份版本快照（序号自增）
func snapshotVersion(e models.KnowledgeEntry, cl *models.Claims) {
	var last models.KnowledgeVersion
	db.DB.Where("entry_id = ?", e.ID).Order("version desc").First(&last)
	ver := last.Version + 1
	opID := uint(0)
	opName := ""
	if cl != nil {
		opID = cl.UserID
		opName = cl.Username
	}
	db.DB.Create(&models.KnowledgeVersion{
		EntryID:      e.ID,
		Version:      ver,
		Title:        e.Title,
		Content:      e.Content,
		Tags:         e.Tags,
		Category:     e.Category,
		OperatorID:   opID,
		OperatorName: opName,
	})
}

// ListKnowledgeVersions GET /workspace/knowledge/:id/versions 版本历史列表
func ListKnowledgeVersions(c *gin.Context) {
	if _, ok := loadVisibleKnowledge(c); !ok {
		return
	}
	var list []models.KnowledgeVersion
	db.DB.Where("entry_id = ?", c.Param("id")).Order("version desc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// RestoreKnowledgeVersion POST /workspace/knowledge/:id/version/:vid/restore 回滚到指定版本
func RestoreKnowledgeVersion(c *gin.Context) {
	e, ok := loadOwnedKnowledge(c)
	if !ok {
		return
	}
	cl := middleware.GetClaims(c)
	var v models.KnowledgeVersion
	if err := db.DB.First(&v, c.Param("vid")).Error; err != nil || v.EntryID != e.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "版本不存在"})
		return
	}
	e.Title = v.Title
	e.Content = v.Content
	e.Tags = v.Tags
	e.Category = v.Category
	db.DB.Save(&e)
	ftsUpsert(*e)
	refreshKnowledgeLinks(*e)
	recordKnowledgeLog(e.ID, cl, "version_restore", fmt.Sprintf("回滚到版本 v%d（%s）", v.Version, v.CreatedAt.Format("2006-01-02 15:04")))
	c.JSON(http.StatusOK, e)
}

// ---------- 9. 模板库 ----------

// seedKnowledgeTemplates 启动时预置系统模板（已存在则跳过）
func seedKnowledgeTemplates() {
	builtins := []models.KnowledgeTemplate{
		{Title: "排班规则 SOP", Category: "SOP", Tags: `["SOP","排班"]`, Content: "<h3>适用范围</h3><p>本规则适用于本班次排班与替班流程。</p><h3>排班原则</h3><ul><li>提前一周发布排班表</li><li>临时调班需双方确认并知会组长</li></ul><h3>异常处理</h3><p>缺勤/迟到按制度处理，并在交接中说明。</p>"},
		{Title: "故障应急处理 SOP", Category: "SOP", Tags: `["故障","应急"]`, Content: "<h3>发现故障</h3><p>第一时间记录现象与时间，截图留存。</p><h3>处理步骤</h3><ol><li>判断影响范围</li><li>尝试重启/回滚</li><li>升级上报</li></ol><h3>复盘</h3><p>事后补全故障单与改进项。</p>"},
		{Title: "交接 SOP", Category: "交接", Tags: `["交接"]`, Content: "<h3>交接内容</h3><p>写清「做到哪 + 接下来做什么」。</p><h3>必填项</h3><ul><li>当前进展</li><li>待继续事项</li><li>注意事项</li></ul>"},
		{Title: "活动执行 Checklist", Category: "活动", Tags: `["活动"]`, Content: "<h3>活动前</h3><ul><li>物料到位</li><li>分工明确</li></ul><h3>活动中</h3><ul><li>实时盯盘</li><li>异常上报</li></ul><h3>活动后</h3><ul><li>数据回收</li><li>复盘归档</li></ul>"},
		{Title: "新人入职指引", Category: "指引", Tags: `["新人","指引"]`, Content: "<h3>第一天</h3><p>熟悉工作台与知识库。</p><h3>第一周</h3><p>跟岗学习核心流程并完成一次独立操作。</p>"},
	}
	for _, t := range builtins {
		var cnt int64
		db.DB.Model(&models.KnowledgeTemplate{}).Where("title = ? AND builtin = 1", t.Title).Count(&cnt)
		if cnt == 0 {
			t.Builtin = 1
			db.DB.Create(&t)
		}
	}
}

// ListKnowledgeTemplates GET /workspace/knowledge/templates 模板列表（系统预置 + 本人自建）
func ListKnowledgeTemplates(c *gin.Context) {
	cl := middleware.GetClaims(c)
	q := db.DB.Model(&models.KnowledgeTemplate{})
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	if cl.Role != models.RoleSuperAdmin {
		q = q.Where("builtin = 1 OR owner_id = ?", cl.UserID)
	}
	var list []models.KnowledgeTemplate
	q.Order("builtin desc, id asc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// CreateKnowledgeTemplate POST /workspace/knowledge/templates 新建自定义模板
func CreateKnowledgeTemplate(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var req struct {
		Title    string   `json:"title"`
		Category string   `json:"category"`
		Content  string   `json:"content"`
		Tags     []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模板标题不能为空"})
		return
	}
	t := models.KnowledgeTemplate{
		Title:     req.Title,
		Category:  strings.TrimSpace(req.Category),
		Content:   sanitizeRichContent(req.Content),
		Tags:      normTags(req.Tags),
		OwnerID:   cl.UserID,
		OwnerName: cl.Username,
	}
	db.DB.Create(&t)
	c.JSON(http.StatusOK, t)
}

// ApplyKnowledgeTemplate GET /workspace/knowledge/templates/:id 获取模板内容（套用到新建）
func ApplyKnowledgeTemplate(c *gin.Context) {
	var t models.KnowledgeTemplate
	if err := db.DB.First(&t, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模板不存在"})
		return
	}
	c.JSON(http.StatusOK, t)
}

// DeleteKnowledgeTemplate DELETE /workspace/knowledge/templates/:id 删除自定义模板（系统预置不可删）
func DeleteKnowledgeTemplate(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var t models.KnowledgeTemplate
	if err := db.DB.First(&t, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模板不存在"})
		return
	}
	if t.Builtin == 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "系统预置模板不可删除"})
		return
	}
	if t.OwnerID != cl.UserID && cl.Role != models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅创建者可删除"})
		return
	}
	db.DB.Delete(&t)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------- 10. Markdown 导入 ----------

// ImportKnowledge POST /workspace/knowledge/import 解析 Markdown 为知识条目（按 H1 切分为多条）
func ImportKnowledge(c *gin.Context) {
	cl := middleware.GetClaims(c)
	var req struct {
		Markdown string `json:"markdown"`
		Scope    string `json:"scope"`
		Category string `json:"category"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Markdown = strings.TrimSpace(req.Markdown)
	if req.Markdown == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请粘贴或上传 Markdown 内容"})
		return
	}
	if req.Scope != string(models.ScopePrivate) {
		req.Scope = string(models.ScopeDepartment)
	}
	docs := splitMarkdownEntries(req.Markdown)
	if len(docs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未能解析出有效内容"})
		return
	}
	created := make([]models.KnowledgeEntry, 0, len(docs))
	for _, d := range docs {
		entry := models.KnowledgeEntry{
			Title:     d.title,
			Category:  strings.TrimSpace(req.Category),
			Content:   sanitizeRichContent(markdownToHTML(d.body)),
			Scope:     models.WorkspaceScope(req.Scope),
			OwnerID:   cl.UserID,
			OwnerName: cl.Username,
			DeptID:    cl.DeptID,
			Status:    "published",
			Tags:      "[]",
		}
		if entry.Title == "" {
			entry.Title = "未命名文档"
		}
		db.DB.Create(&entry)
		ftsUpsert(entry)
		snapshotVersion(entry, cl)
		recordKnowledgeLog(entry.ID, cl, "import", "通过 Markdown 导入知识库")
		created = append(created, entry)
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("Markdown 导入知识库: %d 条", len(created)))
	c.JSON(http.StatusOK, gin.H{"created": len(created), "items": created})
}

type mdEntry struct {
	title string
	body  string
}

// splitMarkdownEntries 按一级标题（# ）切分 Markdown 为多条；无 H1 时整体作为一条
func splitMarkdownEntries(md string) []mdEntry {
	lines := strings.Split(md, "\n")
	var out []mdEntry
	var cur *mdEntry
	flush := func() {
		if cur != nil {
			cur.body = strings.TrimSpace(cur.body)
			if cur.title != "" || cur.body != "" {
				out = append(out, *cur)
			}
			cur = nil
		}
	}
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "# ") {
			flush()
			title := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "# "))
			cur = &mdEntry{title: title}
		} else {
			if cur == nil {
				cur = &mdEntry{}
			}
			cur.body += ln + "\n"
		}
	}
	flush()
	return out
}

// markdownToHTML 轻量 Markdown → HTML（标题/列表/代码/引用/链接/粗斜体/行内代码）
func markdownToHTML(md string) string {
	lines := strings.Split(md, "\n")
	var b strings.Builder
	var inUL, inOL, inCode bool
	closeLists := func() {
		if inUL {
			b.WriteString("</ul>")
			inUL = false
		}
		if inOL {
			b.WriteString("</ol>")
			inOL = false
		}
	}
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			if inCode {
				b.WriteString("</code></pre>")
				inCode = false
			} else {
				closeLists()
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(htmlEscape(ln) + "\n")
			continue
		}
		switch {
		case trim == "":
			closeLists()
			continue
		case strings.HasPrefix(trim, "# "):
			closeLists()
			b.WriteString("<h1>" + inlineMD(trim[2:]) + "</h1>")
		case strings.HasPrefix(trim, "## "):
			closeLists()
			b.WriteString("<h2>" + inlineMD(trim[3:]) + "</h2>")
		case strings.HasPrefix(trim, "### "):
			closeLists()
			b.WriteString("<h3>" + inlineMD(trim[4:]) + "</h3>")
		case strings.HasPrefix(trim, "> "):
			closeLists()
			b.WriteString("<blockquote>" + inlineMD(trim[2:]) + "</blockquote>")
		case regexp.MustCompile(`^\d+\.\s`).MatchString(trim):
			if !inOL {
				closeLists()
				b.WriteString("<ol>")
				inOL = true
			}
			text := regexp.MustCompile(`^\d+\.\s`).ReplaceAllString(trim, "")
			b.WriteString("<li>" + inlineMD(text) + "</li>")
		case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
			if !inUL {
				closeLists()
				b.WriteString("<ul>")
				inUL = true
			}
			b.WriteString("<li>" + inlineMD(trim[2:]) + "</li>")
		default:
			closeLists()
			b.WriteString("<p>" + inlineMD(trim) + "</p>")
		}
	}
	closeLists()
	if inCode {
		b.WriteString("</code></pre>")
	}
	return b.String()
}

// inlineMD 行内格式（链接/粗体/斜体/行内代码）
func inlineMD(s string) string {
	s = htmlEscape(s)
	// 行内代码 `x`
	s = regexp.MustCompile("`([^`]+)`").ReplaceAllString(s, "<code>$1</code>")
	// 链接 [text](url)
	s = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`).ReplaceAllString(s, `<a href="$2" target="_blank" rel="noopener">$1</a>`)
	// 粗体 **x**
	s = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(s, "<strong>$1</strong>")
	// 斜体 *x*
	s = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(s, "<em>$1</em>")
	return s
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// ---------- 11. 导出增强：Markdown / Word ----------

// ExportKnowledgeMarkdown GET /workspace/knowledge/export/markdown 导出当前筛选可见条目为单个 .md 文件
func ExportKnowledgeMarkdown(c *gin.Context) {
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	q = applyKbFilters(c, q)
	var list []models.KnowledgeEntry
	q.Order("category asc, updated_at desc").Find(&list)

	fname := fmt.Sprintf("knowledge_%s.md", time.Now().Format("20060102_1504"))
	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fname, urlQueryEscape(fname)))

	var b strings.Builder
	b.WriteString("# GZT 知识库导出\n\n> 导出时间: " + time.Now().Format("2006-01-02 15:04") + " · 共 " + fmt.Sprintf("%d", len(list)) + " 条\n\n")
	for _, k := range list {
		scope := "部门共享"
		if k.Scope == models.ScopePrivate {
			scope = "仅自己"
		}
		b.WriteString("## " + k.Title + "\n\n")
		b.WriteString("> 分类: " + k.Category + " · 标签: " + strings.Join(parseTags(k.Tags), "、") + " · " + scope + " · 更新于 " + k.UpdatedAt.Format("2006-01-02") + "\n\n")
		b.WriteString(k.Content + "\n\n")
		b.WriteString("---\n\n")
	}
	c.String(http.StatusOK, b.String())
}

// ExportKnowledgeDoc GET /workspace/knowledge/export/doc 导出为 Word(.doc，HTML 封装，Word 可直开)
func ExportKnowledgeDoc(c *gin.Context) {
	q := db.DB.Model(&models.KnowledgeEntry{})
	q = scopeVisibleQ(c, q)
	q = applyKbFilters(c, q)
	var list []models.KnowledgeEntry
	q.Order("category asc, updated_at desc").Find(&list)

	fname := fmt.Sprintf("knowledge_%s.doc", time.Now().Format("20060102_1504"))
	c.Header("Content-Type", "application/msword")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fname, urlQueryEscape(fname)))

	var b strings.Builder
	b.WriteString("<html xmlns:o='urn:schemas-microsoft-com:office:office' xmlns:w='urn:schemas-microsoft-com:office:word'><head><meta charset='utf-8'><title>GZT 知识库</title></head><body>")
	b.WriteString("<h1>GZT 知识库导出</h1><p>导出时间: " + time.Now().Format("2006-01-02 15:04") + " · 共 " + fmt.Sprintf("%d", len(list)) + " 条</p>")
	for _, k := range list {
		scope := "部门共享"
		if k.Scope == models.ScopePrivate {
			scope = "仅自己"
		}
		b.WriteString("<h2>" + htmlEscape(k.Title) + "</h2>")
		b.WriteString("<p><i>分类: " + htmlEscape(k.Category) + " · 标签: " + htmlEscape(strings.Join(parseTags(k.Tags), "、")) + " · " + scope + "</i></p>")
		b.WriteString(k.Content)
		b.WriteString("<hr/>")
	}
	b.WriteString("</body></html>")
	c.String(http.StatusOK, b.String())
}

// applyKbFilters 把列表查询的通用筛选（kw/category/tag/parent/status/mine/starred）应用到查询（供导出复用）
func applyKbFilters(c *gin.Context, q *gorm.DB) *gorm.DB {
	if kw := strings.TrimSpace(c.Query("kw")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("title LIKE ? OR content LIKE ? OR tags LIKE ?", like, like, like)
	}
	if cat := strings.TrimSpace(c.Query("category")); cat != "" {
		q = q.Where("category = ?", cat)
	}
	if tag := strings.TrimSpace(c.Query("tag")); tag != "" {
		q = q.Where("tags LIKE ?", "%"+tag+"%")
	}
	if parent := strings.TrimSpace(c.Query("parent")); parent != "" {
		if parent == "root" {
			q = q.Where("parent_id = 0")
		} else if pid, err := strconvParseUint(parent); err == nil {
			q = q.Where("parent_id = ?", pid)
		}
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		q = q.Where("status = ?", status)
	}
	if c.Query("mine") == "1" {
		if cl := middleware.GetClaims(c); cl != nil {
			q = q.Where("owner_id = ?", cl.UserID)
		}
	}
	if c.Query("starred") == "1" {
		q = q.Where("starred = 1")
	}
	return q
}

// ---------- 12. 双向链接（引用关系） ----------

var reLink = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// refreshKnowledgeLinks 根据正文 [[标题]] 重建该条目的出链
func refreshKnowledgeLinks(e models.KnowledgeEntry) {
	db.DB.Where("source_id = ?", e.ID).Delete(&models.KnowledgeLink{})
	titles := map[string]bool{}
	for _, m := range reLink.FindAllStringSubmatch(e.Content, -1) {
		titles[strings.TrimSpace(m[1])] = true
	}
	for title := range titles {
		var target models.KnowledgeEntry
		if err := db.DB.Where("title = ?", title).First(&target).Error; err == nil && target.ID != e.ID {
			db.DB.Create(&models.KnowledgeLink{SourceID: e.ID, TargetID: target.ID})
		}
	}
}

// rebuildAllLinks 全量重建引用关系（初始化时调用一次）
func rebuildAllLinks() {
	db.DB.Where("1 = 1").Delete(&models.KnowledgeLink{})
	var entries []models.KnowledgeEntry
	db.DB.Find(&entries)
	for _, e := range entries {
		refreshKnowledgeLinks(e)
	}
}

// ListKnowledgeBacklinks GET /workspace/knowledge/:id/backlinks 反向链接（谁引用了我）
func ListKnowledgeBacklinks(c *gin.Context) {
	id, err := strconvParseUint(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var links []models.KnowledgeLink
	db.DB.Where("target_id = ?", id).Find(&links)
	ids := make([]uint, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.SourceID)
	}
	var srcs []models.KnowledgeEntry
	if len(ids) > 0 {
		db.DB.Where("id IN ?", ids).Find(&srcs)
	}
	c.JSON(http.StatusOK, srcs)
}

// ListKnowledgeOutlinks GET /workspace/knowledge/:id/outlinks 正向链接（我引用了谁）
func ListKnowledgeOutlinks(c *gin.Context) {
	id, err := strconvParseUint(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 id"})
		return
	}
	var links []models.KnowledgeLink
	db.DB.Where("source_id = ?", id).Find(&links)
	ids := make([]uint, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.TargetID)
	}
	var tgts []models.KnowledgeEntry
	if len(ids) > 0 {
		db.DB.Where("id IN ?", ids).Find(&tgts)
	}
	c.JSON(http.StatusOK, tgts)
}

// ---------- 小工具 ----------

func strconvParseUint(s string) (uint, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	return uint(v), err
}

func filepathJoin(dir, name string) string {
	return filepath.Join(dir, name)
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

func removeFileSafe(path string) error {
	return os.Remove(path)
}
