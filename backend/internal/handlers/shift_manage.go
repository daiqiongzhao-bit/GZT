package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// ============ 排班管理接口（v0.18.0）============
//
// 权限口径：
//   规则配置 / 员工偏好 / 特殊工作日 / 生成 / 应用  → 部门管理员 + 超管（本部门及子孙）
//   员工提交需求                                   → 任何登录用户（仅本人）
//   解锁已锁定需求                                 → 部门管理员 + 超管
//   查看规则 / 特殊工作日 / 需求列表                → 本部门可见范围

// planDeptID 解析并校验请求中的 dept_id：非超管只能操作本部门及子孙。
func planDeptID(c *gin.Context, raw uint) (uint, bool) {
	cl := currentClaims(c)
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return 0, false
	}
	deptID := raw
	if deptID == 0 {
		deptID = cl.DeptID
	}
	if !canManageDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作该部门"})
		return 0, false
	}
	return deptID, true
}

// ---------- 规则配置 ----------

// GetShiftRule GET /shift-rules?dept_id=
func GetShiftRule(c *gin.Context) {
	cl := currentClaims(c)
	deptID, _ := strconv.Atoi(c.Query("dept_id"))
	did := uint(deptID)
	if did == 0 {
		did = cl.DeptID
	}
	if !canManageDept(c, did) && !canViewDept(c, did) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门规则"})
		return
	}
	// 返回带默认值的规则（未配置时用默认值，方便前端直接渲染）
	r := ruleForDept(did)
	c.JSON(http.StatusOK, r)
}

// canViewDept 是否可查看该部门数据（超管任意；其余限本部门及子孙）
func canViewDept(c *gin.Context, deptID uint) bool {
	ids := deptScopeIDs(c)
	if ids == nil {
		return true
	}
	for _, id := range ids {
		if id == deptID {
			return true
		}
	}
	return false
}

type shiftRuleReq struct {
	DeptID                   uint   `json:"dept_id"`
	MaxRestStreak            int    `json:"max_rest_streak"`
	MaxWorkStreak            int    `json:"max_work_streak"`
	MonthWorkDays            int    `json:"month_work_days"`
	RequireMorningBeforeRest bool   `json:"require_morning_before_rest"`
	RequireEveningAfterRest  bool   `json:"require_evening_after_rest"`
	MorningShiftName         string `json:"morning_shift_name"`
	EveningShiftName         string `json:"evening_shift_name"`
	MinPerShift              int    `json:"min_per_shift"`
	AllowExceedMonthDays     bool   `json:"allow_exceed_month_days"`
}

// UpdateShiftRule PUT /shift-rules 保存规则（部门管/超管）
func UpdateShiftRule(c *gin.Context) {
	var req shiftRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	deptID, ok := planDeptID(c, req.DeptID)
	if !ok {
		return
	}
	// 参数校验：给出明确边界，避免存入无意义的值导致生成器行为异常
	if req.MaxRestStreak < 1 || req.MaxRestStreak > 31 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "最高连续休息天数需在 1–31 之间"})
		return
	}
	if req.MaxWorkStreak < 1 || req.MaxWorkStreak > 31 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "最高连续上班天数需在 1–31 之间"})
		return
	}
	if req.MonthWorkDays < 1 || req.MonthWorkDays > 31 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "月度出勤天数需在 1–31 之间"})
		return
	}
	if req.MinPerShift < 0 || req.MinPerShift > 99 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "每班最少人数需在 0–99 之间"})
		return
	}
	// 班次名（若填了）必须在本部门班次中存在
	shifts := displayShifts(deptID)
	if n := strings.TrimSpace(req.MorningShiftName); n != "" && !containsStr(shifts, n) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("「%s」不是本部门已配置的班次", n)})
		return
	}
	if n := strings.TrimSpace(req.EveningShiftName); n != "" && !containsStr(shifts, n) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("「%s」不是本部门已配置的班次", n)})
		return
	}

	cl := currentClaims(c)
	var r models.ShiftRule
	err := db.DB.Where("dept_id = ?", deptID).First(&r).Error
	if err != nil {
		r = models.ShiftRule{DeptID: deptID}
	}
	r.MaxRestStreak = req.MaxRestStreak
	r.MaxWorkStreak = req.MaxWorkStreak
	r.MonthWorkDays = req.MonthWorkDays
	r.RequireMorningBeforeRest = req.RequireMorningBeforeRest
	r.RequireEveningAfterRest = req.RequireEveningAfterRest
	r.MorningShiftName = strings.TrimSpace(req.MorningShiftName)
	r.EveningShiftName = strings.TrimSpace(req.EveningShiftName)
	r.MinPerShift = req.MinPerShift
	r.AllowExceedMonthDays = req.AllowExceedMonthDays
	r.UpdatedBy = claimsName(cl)

	if err := db.DB.Save(&r).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf(
		"保存排班规则（%s）：连续休≤%d 连续上≤%d 月出勤%d 每班≥%d",
		deptName(deptID), r.MaxRestStreak, r.MaxWorkStreak, r.MonthWorkDays, r.MinPerShift))
	c.JSON(http.StatusOK, r)
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ---------- 员工固定班次偏好 ----------

// ListUserShiftPrefs GET /shift-prefs?dept_id=
func ListUserShiftPrefs(c *gin.Context) {
	deptQ, _ := strconv.Atoi(c.Query("dept_id"))
	deptID := uint(deptQ)
	if deptID == 0 {
		cl := currentClaims(c)
		deptID = cl.DeptID
	}
	if !canViewDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门"})
		return
	}
	var list []models.UserShiftPref
	q := db.DB.Order("user_id asc")
	if deptID > 0 {
		q = q.Where("dept_id IN ?", descendantDeptIDs(deptID))
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

type shiftPrefReq struct {
	UserID        uint   `json:"user_id"`
	DeptID        uint   `json:"dept_id"`
	Mode          string `json:"mode"`            // rotate / fixed
	FixedShift    string `json:"fixed_shift"`     // 固定班次名
	FixedWeekDays string `json:"fixed_week_days"` // 生效星期，空=每天
	Note          string `json:"note"`
}

// UpsertUserShiftPref PUT /shift-prefs 设置员工固定班次（部门管/超管）
func UpsertUserShiftPref(c *gin.Context) {
	var req shiftPrefReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	var u models.User
	if err := db.DB.First(&u, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "人员不存在"})
		return
	}
	if !canManageDept(c, u.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能设置本部门人员"})
		return
	}

	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = models.ShiftModeRotate
	}
	if mode != models.ShiftModeRotate && mode != models.ShiftModeFixed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模式只能是 rotate（参与倒班）或 fixed（固定班次）"})
		return
	}
	fixed := strings.TrimSpace(req.FixedShift)
	if mode == models.ShiftModeFixed {
		if fixed == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "固定班次模式必须指定班次"})
			return
		}
		if !containsStr(displayShifts(u.DeptID), fixed) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("「%s」不是该部门已配置的班次", fixed)})
			return
		}
	}
	// 星期合法性
	if wd := strings.TrimSpace(req.FixedWeekDays); wd != "" {
		days := parseWeekDays(wd)
		if len(days) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "生效星期格式应为 1-7 的逗号分隔（1=周一）"})
			return
		}
	}

	cl := currentClaims(c)
	var p models.UserShiftPref
	err := db.DB.Where("user_id = ?", u.ID).First(&p).Error
	if err != nil {
		p = models.UserShiftPref{UserID: u.ID}
	}
	p.DeptID = u.DeptID
	p.Mode = mode
	p.FixedShift = fixed
	p.FixedWeekDays = strings.TrimSpace(req.FixedWeekDays)
	p.Note = strings.TrimSpace(req.Note)
	if err := db.DB.Save(&p).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if mode == models.ShiftModeFixed {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("设置固定班次：%s → %s", u.Name, fixed))
	} else {
		addLog(c, cl.UserID, cl.Username, "取消固定班次："+u.Name+" 恢复参与倒班")
	}
	// 回读核验
	db.DB.First(&p, p.ID)
	c.JSON(http.StatusOK, p)
}

// DeleteUserShiftPref DELETE /shift-prefs/:userId 取消固定班次（恢复倒班）
func DeleteUserShiftPref(c *gin.Context) {
	uid, _ := strconv.Atoi(c.Param("userId"))
	var u models.User
	if err := db.DB.First(&u, uid).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "人员不存在"})
		return
	}
	if !canManageDept(c, u.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能操作本部门人员"})
		return
	}
	cl := currentClaims(c)
	if err := db.DB.Where("user_id = ?", uid).Delete(&models.UserShiftPref{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "取消固定班次："+u.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------- 员工休假 / 上班需求（规则4）----------

// ListShiftRequests GET /shift-requests?dept_id=&user_id=
// 员工只能看自己的；管理员可看本部门及子孙全部。
func ListShiftRequests(c *gin.Context) {
	cl := currentClaims(c)
	var list []models.ShiftRequest
	q := db.DB.Order("user_id asc, id desc")

	if cl.Role == models.RoleExecutor {
		q = q.Where("user_id = ?", cl.UserID)
	} else {
		deptQ, _ := strconv.Atoi(c.Query("dept_id"))
		did := uint(deptQ)
		if did == 0 {
			did = cl.DeptID
		}
		if !canViewDept(c, did) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门"})
			return
		}
		if did > 0 {
			q = q.Where("dept_id IN ?", descendantDeptIDs(did))
		}
		if uid, _ := strconv.Atoi(c.Query("user_id")); uid > 0 {
			q = q.Where("user_id = ?", uid)
		}
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

type shiftRequestReq struct {
	ID        uint   `json:"id"`
	UserID    uint   `json:"user_id"` // 仅管理员可为他人代录
	Type      string `json:"type"`    // rest / work
	Repeat    string `json:"repeat"`  // once / weekly
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	WeekDays  string `json:"week_days"`
	Reason    string `json:"reason"`
}

// CreateShiftRequest POST /shift-requests 提交需求并锁定（员工本人或管理员代录）
func CreateShiftRequest(c *gin.Context) {
	var req shiftRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	cl := currentClaims(c)

	// 目标人员：员工只能给自己提，管理员可代录本部门人员
	targetID := cl.UserID
	if req.UserID > 0 && req.UserID != cl.UserID {
		if cl.Role == models.RoleExecutor {
			c.JSON(http.StatusForbidden, gin.H{"error": "只能提交本人的需求"})
			return
		}
		targetID = req.UserID
	}
	var u models.User
	if err := db.DB.First(&u, targetID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "人员不存在"})
		return
	}
	if cl.Role != models.RoleExecutor && req.UserID > 0 && !canManageDept(c, u.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能为本人及本部门人员代录需求"})
		return
	}

	typ := strings.TrimSpace(req.Type)
	if typ == "" {
		typ = models.PrefTypeRest
	}
	if typ != models.PrefTypeRest && typ != models.PrefTypeWork {
		c.JSON(http.StatusBadRequest, gin.H{"error": "类型只能是 rest（休假）或 work（指定上班）"})
		return
	}
	repeat := strings.TrimSpace(req.Repeat)
	if repeat == "" {
		repeat = models.PrefRepeatOnce
	}

	// 按重复方式校验必填项
	if repeat == models.PrefRepeatWeek {
		if len(parseWeekDays(req.WeekDays)) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "每周重复需指定星期（1=周一 … 7=周日）"})
			return
		}
	} else {
		repeat = models.PrefRepeatOnce
		st, ok := parseDateKey(req.StartDate)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写开始日期（YYYY-MM-DD）"})
			return
		}
		en := st
		if e, ok2 := parseDateKey(req.EndDate); ok2 {
			en = e
		}
		if en.Before(st) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "结束日期不能早于开始日期"})
			return
		}
		if en.Sub(st).Hours()/24 > 180 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "单次需求跨度不宜超过 180 天"})
			return
		}
		req.StartDate = dateKey(st)
		req.EndDate = dateKey(en)
	}

	now := time.Now()
	r := models.ShiftRequest{
		UserID:    u.ID,
		UserName:  u.Name,
		DeptID:    u.DeptID,
		Type:      typ,
		Repeat:    repeat,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		WeekDays:  strings.TrimSpace(req.WeekDays),
		Reason:    strings.TrimSpace(req.Reason),
		Status:    models.PrefStatusLocked, // 提交即锁定，不可自行修改
		LockedAt:  &now,
	}
	if err := db.DB.Create(&r).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	desc := describeRequest(r)
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("提交排班需求并锁定：%s %s", u.Name, desc))
	c.JSON(http.StatusOK, r)
}

// describeRequest 生成需求的自然语言描述（日志与界面复用）。
func describeRequest(r models.ShiftRequest) string {
	typ := "休假"
	if r.Type == models.PrefTypeWork {
		typ = "指定上班"
	}
	if r.Repeat == models.PrefRepeatWeek {
		names := map[int]string{1: "一", 2: "二", 3: "三", 4: "四", 5: "五", 6: "六", 7: "日"}
		var parts []string
		for _, d := range parseWeekDays(r.WeekDays) {
			parts = append(parts, "周"+names[d])
		}
		return fmt.Sprintf("每周%s%s", strings.Join(parts, "、"), typ)
	}
	if r.StartDate == r.EndDate {
		return fmt.Sprintf("%s%s", r.StartDate, typ)
	}
	return fmt.Sprintf("%s ~ %s %s", r.StartDate, r.EndDate, typ)
}

// UpdateShiftRequest PUT /shift-requests/:id
// 规则4：已锁定需求不可修改；管理员可先解锁再改（或直接重置为待定）。
func UpdateShiftRequest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var r models.ShiftRequest
	if err := db.DB.First(&r, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "需求不存在"})
		return
	}
	cl := currentClaims(c)
	isOwner := r.UserID == cl.UserID
	isAdmin := cl.Role != models.RoleExecutor && canManageDept(c, r.DeptID)

	if !isOwner && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改该需求"})
		return
	}
	// 锁定态：员工本人不可改（正是规则4 的核心）；管理员可改
	if r.Status == models.PrefStatusLocked && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "该需求已锁定不可修改，如需变更请联系管理员解锁"})
		return
	}

	var req shiftRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if t := strings.TrimSpace(req.Type); t == models.PrefTypeRest || t == models.PrefTypeWork {
		r.Type = t
	}
	if rep := strings.TrimSpace(req.Repeat); rep == models.PrefRepeatOnce || rep == models.PrefRepeatWeek {
		r.Repeat = rep
	}
	if req.StartDate != "" {
		if st, ok := parseDateKey(req.StartDate); ok {
			r.StartDate = dateKey(st)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "开始日期格式应为 YYYY-MM-DD"})
			return
		}
	}
	if req.EndDate != "" {
		if en, ok := parseDateKey(req.EndDate); ok {
			r.EndDate = dateKey(en)
		}
	}
	if req.WeekDays != "" {
		r.WeekDays = strings.TrimSpace(req.WeekDays)
	}
	if req.Reason != "" {
		r.Reason = strings.TrimSpace(req.Reason)
	}
	// 校验完整性
	if r.Repeat == models.PrefRepeatWeek {
		if len(parseWeekDays(r.WeekDays)) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "每周重复需指定星期"})
			return
		}
	} else {
		st, ok := parseDateKey(r.StartDate)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写开始日期"})
			return
		}
		en := st
		if e, ok2 := parseDateKey(r.EndDate); ok2 {
			en = e
		}
		if en.Before(st) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "结束日期不能早于开始日期"})
			return
		}
		r.StartDate, r.EndDate = dateKey(st), dateKey(en)
	}
	now := time.Now()
	r.LockedAt = &now
	r.Status = models.PrefStatusLocked
	if err := db.DB.Save(&r).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("修改排班需求：%s %s", r.UserName, describeRequest(r)))
	c.JSON(http.StatusOK, r)
}

// DeleteShiftRequest DELETE /shift-requests/:id
// 员工可删除自己的「未锁定」需求；管理员可删除本部门任意需求。
func DeleteShiftRequest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var r models.ShiftRequest
	if err := db.DB.First(&r, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "需求不存在"})
		return
	}
	cl := currentClaims(c)
	isOwner := r.UserID == cl.UserID
	isAdmin := cl.Role != models.RoleExecutor && canManageDept(c, r.DeptID)

	if !isAdmin {
		if !isOwner {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该需求"})
			return
		}
		if r.Status == models.PrefStatusLocked {
			c.JSON(http.StatusForbidden, gin.H{"error": "该需求已锁定不可删除，如需变更请联系管理员解锁"})
			return
		}
	}
	if err := db.DB.Delete(&models.ShiftRequest{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("删除排班需求：%s %s", r.UserName, describeRequest(r)))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UnlockShiftRequest POST /shift-requests/:id/unlock 管理员解锁（改为待定）
func UnlockShiftRequest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var r models.ShiftRequest
	if err := db.DB.First(&r, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "需求不存在"})
		return
	}
	cl := currentClaims(c)
	if cl.Role == models.RoleExecutor || !canManageDept(c, r.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有管理员可以解锁需求"})
		return
	}
	if r.Status != models.PrefStatusLocked {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该需求未处于锁定状态"})
		return
	}
	if err := db.DB.Model(&models.ShiftRequest{}).Where("id = ?", r.ID).
		Updates(map[string]interface{}{"status": models.PrefStatusPending, "locked_at": nil}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("解锁排班需求：%s %s", r.UserName, describeRequest(r)))
	db.DB.First(&r, id) // 回读核验
	c.JSON(http.StatusOK, r)
}

// ---------- 特殊工作日（规则6）----------

// ListSpecialWorkDays GET /special-workdays?dept_id=&from=&to=
func ListSpecialWorkDays(c *gin.Context) {
	deptQ, _ := strconv.Atoi(c.Query("dept_id"))
	deptID := uint(deptQ)
	if deptID == 0 {
		cl := currentClaims(c)
		deptID = cl.DeptID
	}
	if !canViewDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门"})
		return
	}
	var list []models.SpecialWorkDay
	q := db.DB.Order("date asc")
	if deptID > 0 {
		q = q.Where("dept_id = ? OR dept_id = 0", deptID)
	}
	if f := strings.TrimSpace(c.Query("from")); f != "" {
		q = q.Where("date >= ?", f)
	}
	if t := strings.TrimSpace(c.Query("to")); t != "" {
		q = q.Where("date <= ?", t)
	}
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

type specialDayReq struct {
	ID       uint   `json:"id"`
	Date     string `json:"date"`
	DeptID   uint   `json:"dept_id"` // 0 = 全部部门
	Name     string `json:"name"`
	AllStaff bool   `json:"all_staff"`
	Note     string `json:"note"`
}

// UpsertSpecialWorkDay POST /special-workdays 新增或更新
func UpsertSpecialWorkDay(c *gin.Context) {
	var req specialDayReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	cl := currentClaims(c)
	if req.ID > 0 {
		var old models.SpecialWorkDay
		if err := db.DB.First(&old, req.ID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "特殊工作日不存在"})
			return
		}
		// dept_id=0 表示全局，仅超管可改
		if old.DeptID == 0 {
			if cl.Role != models.RoleSuperAdmin {
				c.JSON(http.StatusForbidden, gin.H{"error": "全局特殊工作日仅超级管理员可修改"})
				return
			}
		} else if !canManageDept(c, old.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权修改该部门特殊工作日"})
			return
		}
		if d, ok := parseDateKey(req.Date); ok {
			old.Date = dateKey(d)
		}
		if n := strings.TrimSpace(req.Name); n != "" {
			old.Name = n
		}
		old.AllStaff = req.AllStaff
		old.Note = strings.TrimSpace(req.Note)
		if err := db.DB.Save(&old).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		addLog(c, cl.UserID, cl.Username, "修改特殊工作日："+old.Date+" "+old.Name)
		c.JSON(http.StatusOK, old)
		return
	}

	deptID, ok := planDeptID(c, req.DeptID)
	if !ok {
		return
	}
	d, ok2 := parseDateKey(req.Date)
	if !ok2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 YYYY-MM-DD"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写事由（如：店庆、节假日）"})
		return
	}
	var cnt int64
	db.DB.Model(&models.SpecialWorkDay{}).Where("date = ? AND dept_id = ?", dateKey(d), deptID).Count(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该日期已存在特殊工作日设置"})
		return
	}
	s := models.SpecialWorkDay{
		Date: dateKey(d), DeptID: deptID, Name: name,
		AllStaff: req.AllStaff, Note: strings.TrimSpace(req.Note),
	}
	if err := db.DB.Create(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "新增特殊工作日："+s.Date+" "+s.Name)
	c.JSON(http.StatusOK, s)
}

// DeleteSpecialWorkDay DELETE /special-workdays/:id
func DeleteSpecialWorkDay(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var s models.SpecialWorkDay
	if err := db.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "特殊工作日不存在"})
		return
	}
	cl := currentClaims(c)
	if s.DeptID == 0 {
		if cl.Role != models.RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "全局特殊工作日仅超级管理员可删除"})
			return
		}
	} else if !canManageDept(c, s.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该部门特殊工作日"})
		return
	}
	if err := db.DB.Delete(&models.SpecialWorkDay{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "删除特殊工作日："+s.Date+" "+s.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------- 生成 / 校验 / 应用 ----------

type generateReq struct {
	DeptID  uint   `json:"dept_id"`
	Year    int    `json:"year"`
	Month   int    `json:"month"`
	UserIDs []uint `json:"user_ids"` // 可选：仅排这些人
}

// GenerateSchedule POST /schedules/generate 生成整月排班（不落库，返回预览）
func GenerateSchedule(c *gin.Context) {
	var req generateReq
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
	pi := buildPlanInfo(deptID, req.Year, req.Month, req.UserIDs)
	res, err := pi.GeneratePlan()
	if err != nil {
		// 预检失败（如人手不足）属于用户输入问题，返回 400 并给出可操作建议
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"plan":       res.Plan,
		"violations": res.Violations,
		"notes":      res.Notes,
		"warnings":   res.Warnings,
		"shifts":     pi.Shifts,
		"morning":    pi.Morning,
		"evening":    pi.Evening,
		"people":     pi.People,
		"special":    pi.Special,
		"rule":       pi.Rule,
	})
}

type planApplyReq struct {
	DeptID uint                         `json:"dept_id"`
	Year   int                          `json:"year"`
	Month  int                          `json:"month"`
	Plan   map[string]map[string]string `json:"plan"`
}

// ApplyPlan POST /schedules/apply 把（微调后的）整月计划落库为正式班表
func ApplyPlan(c *gin.Context) {
	var req planApplyReq
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "计划为空，无可应用的排班"})
		return
	}

	pi := buildPlanInfo(deptID, req.Year, req.Month, nil)

	// 1) 强制还原锁定需求（规则4）：即使前端传回的计划覆盖了锁定休假，也必须还原
	notes := pi.enforceLocks(req.Plan)

	// 2) 校验：把违规一并返回，由管理员决定是否仍要应用
	violations := pi.validatePlan(req.Plan)

	// 3) 落库：先清空该部门该月班表，再按计划写入。
	//    只处理本部门人员，避免误删其他部门数据。
	validNames := map[string]bool{}
	for _, p := range pi.People {
		validNames[p.Name] = true
	}
	first, last := monthRange(req.Year, req.Month)
	from, to := dateKey(first), dateKey(last)

	tx := db.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": tx.Error.Error()})
		return
	}
	if err := tx.Where("dept_id = ? AND date >= ? AND date <= ?", deptID, from, to).
		Delete(&models.Schedule{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 按「日期 + 班次」聚合成多条 Schedule（People 为 JSON 数组）
	type key struct{ date, shift string }
	buckets := map[key][]string{}
	for date, byName := range req.Plan {
		d, ok := parseDateKey(date)
		if !ok || d.Before(first) || d.After(last) {
			continue // 忽略区间外的脏数据
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		// 稳定顺序，保证落库结果可复现
		sortStrings(names)
		for _, name := range names {
			shift := strings.TrimSpace(byName[name])
			if isRestShift(shift) || isPendingShift(shift) {
				continue // 休息与占位不落库
			}
			if !validNames[name] {
				continue // 非本部门人员，跳过
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
		s := models.Schedule{Date: k.date, Shift: k.shift, People: string(pj), DeptID: deptID}
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

	// 4) 通知被排班人员
	allNames := make([]string, 0, len(pi.People))
	for _, p := range pi.People {
		allNames = append(allNames, p.Name)
	}
	cl := currentClaims(c)
	notifyPeopleByName(allNames, "schedule", "班表更新",
		fmt.Sprintf("%s 于 %s 发布了 %d 年 %d 月排班（%s）",
			claimsName(cl), time.Now().Format("2006-01-02 15:04"), req.Year, req.Month, deptName(deptID)),
		cl.UserID, claimsName(cl))

	addLog(c, cl.UserID, cl.Username, fmt.Sprintf("应用排班：%s %d年%d月，共 %d 条班次记录",
		deptName(deptID), req.Year, req.Month, created))

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"created":    created,
		"violations": violations,
		"notes":      notes,
	})
}

// sortStrings 简单字符串排序（避免为小切片引入 sort 包依赖差异）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		cur := s[i]
		j := i - 1
		for j >= 0 && s[j] > cur {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = cur
	}
}

// ValidatePlan POST /schedules/validate 单独校验一份计划（手工微调后复查）
func ValidatePlan(c *gin.Context) {
	var req planApplyReq
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
	pi := buildPlanInfo(deptID, req.Year, req.Month, nil)
	vs := pi.validatePlan(req.Plan)
	// 按人员/规则汇总，便于界面直接展示
	byRule := map[string]int{}
	byPerson := map[string]int{}
	for _, v := range vs {
		byRule[v.Rule]++
		byPerson[v.Person]++
	}
	c.JSON(http.StatusOK, gin.H{
		"violations": vs,
		"by_rule":    byRule,
		"by_person":  byPerson,
		"total":      len(vs),
	})
}
