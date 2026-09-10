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

// ============ 排班草稿（多版本暂存）============
//
// 场景：排班生成后往往需要反复微调，定稿前不该直接覆盖正式班表。
// 流程：生成预览 → 暂存为草稿（可存多版）→ 挑一版「推送到正式班表」。

type draftSaveReq struct {
	DeptID uint                         `json:"dept_id"`
	Year   int                          `json:"year"`
	Month  int                          `json:"month"`
	Name   string                       `json:"name"`
	Note   string                       `json:"note"`
	Plan   map[string]map[string]string `json:"plan"`
	Stats  map[string]interface{}       `json:"stats"`
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
	if year == 0 || month == 0 {
		now := time.Now()
		year, month = now.Year(), int(now.Month())
	}

	var list []models.ShiftPlanDraft
	q := db.DB.Where("dept_id = ? AND year = ? AND month = ?", deptID, year, month)
	if err := q.Order("created_at desc").Find(&list).Error; err != nil {
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
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
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
			CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "未命名版本"
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
	d := models.ShiftPlanDraft{
		DeptID:    deptID,
		Year:      req.Year,
		Month:     req.Month,
		Name:      name,
		Note:      strings.TrimSpace(req.Note),
		Content:   string(content),
		Stats:     statsJSON,
		CreatorID: uid,
		Creator:   uname,
	}
	if err := db.DB.Create(&d).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": d.ID, "violations": len(violations)})
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
