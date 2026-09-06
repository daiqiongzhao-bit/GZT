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

func ListSchedules(c *gin.Context) {
	scope := deptScopeIDs(c)
	var list []models.Schedule
	q := db.DB.Order("date asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// replacePersonShifts 实施「一人一天只在一个班次」规则：
// 对 people 中的每个人，清除其当天（同部门）在其他班次的旧记录——
// 单人的旧记录整条删除，多人记录则只移除该人。管理员改班次即「替换」，
// 而不是追加出同人同天多班次。excludeID 用于编辑时排除自身记录。
// 返回被替换（移除）的人员名单，便于日志与提示。
func replacePersonShifts(date string, deptID uint, people []string, excludeID uint) []string {
	var existing []models.Schedule
	// v0.0.5：跨部门清理（原实现带 dept_id 条件，跨部门就失效）。
	// 同一人同一天只允许存在一个班次，否则超管视角会看到「一人两个班次」。
	// deptID 仍保留用于日志与未来扩展。
	_ = deptID
	q := db.DB.Where("date = ?", date)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	q.Find(&existing)
	var replaced []string
	for _, p := range people {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for _, s := range existing {
			var names []string
			if err := json.Unmarshal([]byte(s.People), &names); err != nil {
				continue
			}
			idx := -1
			for i, n := range names {
				if n == p {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			// 同一人可能有多条旧记录（异常数据），名单里只记一次
			dup := false
			for _, r := range replaced {
				if r == p {
					dup = true
					break
				}
			}
			if !dup {
				replaced = append(replaced, p)
			}
			if len(names) == 1 {
				// 旧记录只包含此人 → 整条删除
				db.DB.Delete(&models.Schedule{}, s.ID)
			} else {
				// 多人记录 → 只移除此人
				names = append(names[:idx], names[idx+1:]...)
				nj, _ := json.Marshal(names)
				db.DB.Model(&models.Schedule{}).Where("id = ?", s.ID).Update("people", string(nj))
			}
		}
	}
	return replaced
}

// validatePeopleDept 校验排班人员是否归属该部门（或其上级部门）。
// v0.0.5：修复「A 部门的人被排进 B 部门班表」——刘海龙属三亚预订仓，却能被排进信息部。
// 查不到对应账号的历史姓名不做强校验，避免阻断既有数据。
func validatePeopleDept(deptID uint, people []string) error {
	if deptID == 0 || len(people) == 0 {
		return nil
	}
	var bad []string
	seen := map[string]bool{}
	for _, raw := range people {
		p := strings.TrimSpace(raw)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		var u models.User
		if err := db.DB.Where("name = ? OR username = ?", p, p).First(&u).Error; err != nil {
			continue // 历史遗留姓名，无对应账号 → 不强校验
		}
		if u.DeptID == deptID || isAncestorDept(u.DeptID, deptID) {
			continue // 本部门人员，或上级部门人员下到子部门排班
		}
		bad = append(bad, fmt.Sprintf("%s（属%s）", u.Name, deptName(u.DeptID)))
	}
	if len(bad) > 0 {
		return fmt.Errorf("以下人员不属于该部门，无法排入：%s", strings.Join(bad, "、"))
	}
	return nil
}

// deptName 返回部门名称，查不到时降级为「部门N」
func deptName(id uint) string {
	var d models.Department
	if err := db.DB.First(&d, id).Error; err != nil {
		return fmt.Sprintf("部门%d", id)
	}
	return d.Name
}

// isAncestorDept 判断 ancestorID 是否为 deptID 的上级部门（含自身）
func isAncestorDept(ancestorID, deptID uint) bool {
	for _, id := range descendantDeptIDs(ancestorID) {
		if id == deptID {
			return true
		}
	}
	return false
}

type scheduleReq struct {
	Date   string   `json:"date"`
	Shift  string   `json:"shift"`
	People []string `json:"people"`
	DeptID uint     `json:"dept_id"`
}

// CreateSchedule 新建班表
func CreateSchedule(c *gin.Context) {
	var req scheduleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Date == "" || req.Shift == "" || len(req.People) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期、班次、当班人员均必填"})
		return
	}
	cl := currentClaims(c)
	deptID := req.DeptID
	if deptID == 0 {
		deptID = cl.DeptID // 未指定部门时兜底为本部门
	}
	if !canManageDept(c, deptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权在该部门下操作"})
		return
	}
	// v0.0.5：人员必须归属该部门，防止跨部门排班
	if err := validatePeopleDept(deptID, req.People); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 一人一天只在一个班次：先把该批人员当天其他班次的旧记录替换掉
	replaced := replacePersonShifts(req.Date, deptID, req.People, 0)
	if len(replaced) > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("班次替换：%s 从其他班次改为 %s %s",
			strings.Join(replaced, "、"), req.Date, req.Shift))
	}
	peopleJSON, _ := json.Marshal(req.People)
	s := models.Schedule{
		Date:   req.Date,
		Shift:  req.Shift,
		People: string(peopleJSON),
		DeptID: deptID,
	}
	if err := db.DB.Create(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "新建班表: "+req.Date+" "+req.Shift)
	notifyPeopleByName(req.People, "schedule", "班表更新",
		fmt.Sprintf("%s 于 %s 为你安排了 %s %s", claimsName(cl), time.Now().Format("2006-01-02 15:04"), req.Date, req.Shift),
		cl.UserID, claimsName(cl))
	c.JSON(http.StatusOK, s)
}

// DeleteSchedule 删除班表
func DeleteSchedule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var s models.Schedule
	if err := db.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "班表不存在"})
		return
	}
	cl := currentClaims(c)
	if !canManageDept(c, s.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除其他部门班表"})
		return
	}
	if err := db.DB.Delete(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "删除班表: "+c.Param("id"))
	var oldPeople []string
	if err := json.Unmarshal([]byte(s.People), &oldPeople); err == nil {
		notifyPeopleByName(oldPeople, "schedule", "班表更新",
			fmt.Sprintf("%s 于 %s 取消了你的 %s %s", claimsName(cl), time.Now().Format("2006-01-02 15:04"), s.Date, s.Shift),
			cl.UserID, claimsName(cl))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UpdateSchedule 修改班表：支持跨日移动 / 改班次 / 改人员
func UpdateSchedule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var s models.Schedule
	if err := db.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "班表不存在"})
		return
	}
	cl := currentClaims(c)
	if !canManageDept(c, s.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改其他部门班表"})
		return
	}
	var req scheduleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Date != "" {
		s.Date = req.Date
	}
	if req.Shift != "" {
		s.Shift = req.Shift
	}
	if req.People != nil {
		peopleJSON, _ := json.Marshal(req.People)
		s.People = string(peopleJSON)
	}
	// v0.0.5：支持跨部门迁移班表（原实现忽略 req.DeptID，部门建错后无法修改，只能删了重建）
	if req.DeptID > 0 && req.DeptID != s.DeptID {
		if !canManageDept(c, req.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权把班表迁移到该部门"})
			return
		}
		s.DeptID = req.DeptID
	}
	// 以最终人员名单校验归属（req.People 为空时沿用原有名单）
	var finalPeople []string
	_ = json.Unmarshal([]byte(s.People), &finalPeople)
	if err := validatePeopleDept(s.DeptID, finalPeople); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 一人一天只在一个班次：编辑时把该批人员当天其他班次的旧记录替换掉（排除本条自身）
	replaced := replacePersonShifts(s.Date, s.DeptID, finalPeople, s.ID)
	if len(replaced) > 0 {
		addLog(c, cl.UserID, cl.Username, fmt.Sprintf("班次替换：%s 从其他班次改为 %s %s",
			strings.Join(replaced, "、"), s.Date, s.Shift))
	}
	if err := db.DB.Save(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "修改班表: "+s.Date+" "+s.Shift)
	var curPeople []string
	if err := json.Unmarshal([]byte(s.People), &curPeople); err == nil {
		notifyPeopleByName(curPeople, "schedule", "班表更新",
			fmt.Sprintf("%s 于 %s 更新了你的 %s %s 班表", claimsName(cl), time.Now().Format("2006-01-02 15:04"), s.Date, s.Shift),
			cl.UserID, claimsName(cl))
	}
	c.JSON(http.StatusOK, s)
}
