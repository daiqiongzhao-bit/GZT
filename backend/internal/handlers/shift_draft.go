package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ============ 排班版本管理（v0.19.3 草稿箱 → v0.37.0 版本管理）============
//
// 场景：排班生成后往往需要反复微调，定稿前不该直接覆盖正式班表。
// 流程：生成预览 → 自动/手动留档为版本 → 可对比、可「只补差异」回滚 → 挑一版推送到正式班表。
//
// v0.37.0 新增：
//   · 自动留档（auto=true）：生成/微调/发布后自动存一版，同人 + 5 分钟内的连续改动合并成一条，
//     避免一次微调刷出几十条版本；自动版每部门每月只保留最近 maxAutoVersions 条。
//   · 版本序号 rev：同部门同月从 1 递增，界面显示 v1 / v2 …。
//   · 改名 / 改备注（PUT /shift-drafts/:id）。

const (
	// autoMergeWindow 自动留档的合并窗口：同一人在该时间窗内的连续改动只留一条版本
	autoMergeWindow = 5 * time.Minute
	// maxAutoVersions 每部门每月保留的自动版本上限（手动版不受限）
	maxAutoVersions = 30
)

type draftSaveReq struct {
	DeptID uint                         `json:"dept_id"`
	Year   int                          `json:"year"`
	Month  int                          `json:"month"`
	Name   string                       `json:"name"`
	Note   string                       `json:"note"`
	Plan   map[string]map[string]string `json:"plan"`
	Stats  map[string]interface{}       `json:"stats"`
	Source string                       `json:"source"` // v0.37.0：版本来源
	Auto   bool                         `json:"auto"`   // v0.37.0：是否自动留档
}

// draftUpdateReq PUT /shift-drafts/:id 改名 / 改备注 / 标记「当前发布版」
type draftUpdateReq struct {
	Name string `json:"name"`
	Note string `json:"note"`
	// Applied 非 nil 时设置「当前发布版」标记：置 true 会把同部门同月其它版本取消标记
	Applied *bool `json:"applied"`
}

// draftSourceLabel 把来源枚举翻成人类可读的默认版本名
func draftSourceLabel(source string) string {
	switch source {
	case "generate":
		return "生成班表"
	case "adjust":
		return "手动微调"
	case "apply":
		return "发布前存档"
	case "rollback":
		return "回滚存档"
	case "import":
		return "导入存档"
	default:
		return "手动存版"
	}
}

// nextDraftRev 计算同部门同月的下一个版本序号
func nextDraftRev(deptID uint, year, month int) int {
	var last models.ShiftPlanDraft
	if err := db.DB.Where("dept_id = ? AND year = ? AND month = ?", deptID, year, month).
		Order("rev desc").First(&last).Error; err == nil {
		return last.Rev + 1
	}
	return 1
}

// pruneAutoVersions 自动版本超过上限时，删除最旧的若干条（手动版本不动）
func pruneAutoVersions(deptID uint, year, month int) {
	var old []models.ShiftPlanDraft
	db.DB.Where("dept_id = ? AND year = ? AND month = ? AND is_auto = ?", deptID, year, month, true).
		Order("created_at desc").Offset(maxAutoVersions).Find(&old)
	for _, d := range old {
		db.DB.Delete(&models.ShiftPlanDraft{}, d.ID)
	}
}

// ListPlanDrafts GET /shift-drafts 列出某部门某月的草稿版本
func ListPlanDrafts(c *gin.Context) {
	var raw uint
	if v, err := strconv.Atoi(c.Query("dept_id")); err == nil && v > 0 {
		raw = uint(v)
	}
	deptID, ok := planDeptID(c, raw)
	if !ok {
		return
	}
	year, _ := strconv.Atoi(c.Query("year"))
	month, _ := strconv.Atoi(c.Query("month"))

	// 草稿箱视图：不传 year+month 时返回该部门全部草稿（跨月份），
	// 避免「保存的是 11 月、当前看的是 9 月」导致草稿凭空消失。
	var list []models.ShiftPlanDraft
	q := db.DB.Where("dept_id = ?", deptID)
	if year > 0 && month > 0 {
		q = q.Where("year = ? AND month = ?", year, month)
	}
	if err := q.Order("year desc, month desc, created_at desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 列表不返回完整班表内容（可能很大），只给概要
	type item struct {
		ID        uint      `json:"id"`
		Name      string    `json:"name"`
		Note      string    `json:"note"`
		Applied   bool      `json:"applied"`
		Creator   string    `json:"creator"`
		Stats     string    `json:"stats"`
		Days      int       `json:"days"`
		Year      int       `json:"year"`
		Month     int       `json:"month"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		// v0.37.0 版本管理
		Source string `json:"source"`
		Auto   bool   `json:"auto"`
		Rev    int    `json:"rev"`
	}
	out := make([]item, 0, len(list))
	for _, d := range list {
		days := 0
		var plan map[string]map[string]string
		if json.Unmarshal([]byte(d.Content), &plan) == nil {
			days = len(plan)
		}
		out = append(out, item{
			ID: d.ID, Name: d.Name, Note: d.Note, Applied: d.Applied,
			Creator: d.Creator, Stats: d.Stats, Days: days,
			Year: d.Year, Month: d.Month,
			CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
			Source: d.Source, Auto: d.Auto, Rev: d.Rev,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// SavePlanDraft POST /shift-drafts 暂存一个排班版本
func SavePlanDraft(c *gin.Context) {
	var req draftSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	deptID, ok := planDeptID(c, req.DeptID)
	if !ok {
		return
	}
	if req.Year < 2000 || req.Year > 2100 || req.Month < 1 || req.Month > 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供有效的年份与月份"})
		return
	}
	if len(req.Plan) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "班表为空，无可暂存的内容"})
		return
	}
	content, err := json.Marshal(req.Plan)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "班表序列化失败"})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		if req.Auto {
			source = "adjust"
		} else {
			source = "manual"
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		// 自动留档不打断操作，用「来源 + 时间」自动命名；手动存版仍由界面提示取名
		name = draftSourceLabel(source)
		if req.Auto {
			name += " · " + time.Now().Format("01-02 15:04")
		}
	}

	statsJSON := ""
	if req.Stats != nil {
		if b, err := json.Marshal(req.Stats); err == nil {
			statsJSON = string(b)
		}
	}

	// 违规数快照：让列表页能直观显示每个版本的质量
	pi := buildPlanInfo(deptID, req.Year, req.Month, nil)
	violations := pi.validatePlan(req.Plan)
	if statsJSON == "" {
		sb, _ := json.Marshal(map[string]interface{}{"violations": len(violations)})
		statsJSON = string(sb)
	}

	uid, uname := currentUser(c)
	note := strings.TrimSpace(req.Note)

	// v0.37.0：自动留档的「合并窗口」。同部门 + 同月 + 同一人 + 5 分钟内的连续自动改动，
	// 视为同一次编辑，直接覆盖上一条自动版本（rev 不变），避免微调时刷出几十条版本。
	if req.Auto {
		var last models.ShiftPlanDraft
		if err := db.DB.Where("dept_id = ? AND year = ? AND month = ? AND is_auto = ? AND creator_id = ?",
			deptID, req.Year, req.Month, true, uid).
			Order("created_at desc").First(&last).Error; err == nil && time.Since(last.CreatedAt) < autoMergeWindow {
			db.DB.Model(&models.ShiftPlanDraft{}).Where("id = ?", last.ID).Updates(map[string]interface{}{
				"content": string(content),
				"stats":   statsJSON,
				"source":  source,
				"name":    name,
				"note":    note,
			})
			pruneAutoVersions(deptID, req.Year, req.Month)
			c.JSON(http.StatusOK, gin.H{"id": last.ID, "rev": last.Rev, "merged": true, "violations": len(violations)})
			return
		}
	}

	rev := nextDraftRev(deptID, req.Year, req.Month)
	d := models.ShiftPlanDraft{
		DeptID:    deptID,
		Year:      req.Year,
		Month:     req.Month,
		Name:      name,
		Note:      note,
		Content:   string(content),
		Stats:     statsJSON,
		CreatorID: uid,
		Creator:   uname,
		Source:    source,
		Auto:      req.Auto,
		Rev:       rev,
	}
	if err := db.DB.Create(&d).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Auto {
		pruneAutoVersions(deptID, req.Year, req.Month)
	}
	c.JSON(http.StatusOK, gin.H{"id": d.ID, "rev": rev, "merged": false, "violations": len(violations)})
}

// UpdatePlanDraft PUT /shift-drafts/:id 版本改名 / 改备注
func UpdatePlanDraft(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的版本 ID"})
		return
	}
	var d models.ShiftPlanDraft
	if err := db.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "版本不存在"})
		return
	}
	if !canManageDept(c, d.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改该部门的排班版本"})
		return
	}
	var req draftUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	up := map[string]interface{}{}
	if n := strings.TrimSpace(req.Name); n != "" {
		if len([]rune(n)) > 64 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "版本名不超过 64 个字"})
			return
		}
		up["name"] = n
	}
	if len([]rune(strings.TrimSpace(req.Note))) > 512 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "备注不超过 512 个字"})
		return
	}
	up["note"] = strings.TrimSpace(req.Note)
	if req.Applied != nil {
		if *req.Applied {
			// 一份班表只有一个生效版本：标记它之前先把同月其它版本取消标记
			db.DB.Model(&models.ShiftPlanDraft{}).
				Where("dept_id = ? AND year = ? AND month = ? AND id != ?", d.DeptID, d.Year, d.Month, d.ID).
				Update("applied", false)
		}
		up["applied"] = *req.Applied
	}
	if len(up) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有要修改的内容"})
		return
	}
	if err := db.DB.Model(&models.ShiftPlanDraft{}).Where("id = ?", id).Updates(up).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetPlanDraft GET /shift-drafts/:id 读取某个草稿的完整班表
func GetPlanDraft(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的草稿 ID"})
		return
	}
	var d models.ShiftPlanDraft
	if err := db.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "草稿不存在"})
		return
	}
	var plan map[string]map[string]string
	if err := json.Unmarshal([]byte(d.Content), &plan); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "班表内容解析失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":      d.ID,
		"dept_id": d.DeptID,
		"year":    d.Year,
		"month":   d.Month,
		"name":    d.Name,
		"note":    d.Note,
		"applied": d.Applied,
		"creator": d.Creator,
		"plan":    plan,
	})
}

// DeletePlanDraft DELETE /shift-drafts/:id 删除草稿
func DeletePlanDraft(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的草稿 ID"})
		return
	}
	if err := db.DB.Delete(&models.ShiftPlanDraft{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ApplyPlanDraft POST /shift-drafts/:id/apply 把草稿推送到正式班表
//
// 复用 ApplyPlan 的落库逻辑：强制还原锁定需求 → 校验 → 覆盖该部门该月。
func ApplyPlanDraft(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的草稿 ID"})
		return
	}
	var d models.ShiftPlanDraft
	if err := db.DB.First(&d, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "草稿不存在"})
		return
	}
	var plan map[string]map[string]string
	if err := json.Unmarshal([]byte(d.Content), &plan); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "班表内容解析失败"})
		return
	}

	req := planApplyReq{DeptID: d.DeptID, Year: d.Year, Month: d.Month, Plan: plan}
	pi := buildPlanInfo(d.DeptID, d.Year, d.Month, nil)

	// 1) 还原锁定需求（规则4）
	notes := pi.enforceLocks(req.Plan)
	// 2) 校验
	violations := pi.validatePlan(req.Plan)

	// 3) 落库（与 ApplyPlan 一致：清空该部门该月后重写）
	validNames := map[string]bool{}
	for _, p := range pi.People {
		validNames[p.Name] = true
	}
	first, last := monthRange(d.Year, d.Month)
	from, to := dateKey(first), dateKey(last)

	tx := db.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
		return
	}
	if err := tx.Where("dept_id = ? AND date >= ? AND date <= ?", d.DeptID, from, to).
		Delete(&models.Schedule{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type key struct{ date, shift string }
	buckets := map[key][]string{}
	for date, byName := range req.Plan {
		dt, ok := parseDateKey(date)
		if !ok || dt.Before(first) || dt.After(last) {
			continue
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			shift := strings.TrimSpace(byName[name])
			if isRestShift(shift) || isPendingShift(shift) {
				continue
			}
			if !validNames[name] {
				continue
			}
			buckets[key{date, shift}] = append(buckets[key{date, shift}], name)
		}
	}

	created := 0
	for k, names := range buckets {
		if len(names) == 0 {
			continue
		}
		pj, _ := json.Marshal(names)
		s := models.Schedule{Date: k.date, Shift: k.shift, People: string(pj), DeptID: d.DeptID}
		if err := tx.Create(&s).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		created++
	}
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 标记该草稿已应用，并把同月其他草稿置为未应用（一份班表只有一个生效版本）
	db.DB.Model(&models.ShiftPlanDraft{}).
		Where("dept_id = ? AND year = ? AND month = ? AND id != ?", d.DeptID, d.Year, d.Month, d.ID).
		Update("applied", false)
	db.DB.Model(&models.ShiftPlanDraft{}).Where("id = ?", d.ID).Update("applied", true)

	c.JSON(http.StatusOK, gin.H{
		"created":    created,
		"violations": violations,
		"notes":      notes,
	})
}

// currentUserName 取当前登录用户名（取不到就留空，不影响草稿功能）。
func currentUser(c *gin.Context) (uint, string) {
	var u models.User
	if cl := middleware.GetClaims(c); cl != nil {
		if err := db.DB.Select("id", "name").First(&u, cl.UserID).Error; err == nil {
			return u.ID, u.Name
		}
		return cl.UserID, cl.Username
	}
	return 0, ""
}
