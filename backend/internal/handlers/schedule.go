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
	q := db.DB.Where("date = ? AND dept_id = ?", date, deptID)
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
	// 一人一天只在一个班次：编辑时把该批人员当天其他班次的旧记录替换掉（排除本条自身）
	replaced := replacePersonShifts(s.Date, s.DeptID, req.People, s.ID)
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
