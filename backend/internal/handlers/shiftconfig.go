package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

var hmRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// ListShiftConfigs GET /shift-configs 部门班次列表（按部门隔离）
func ListShiftConfigs(c *gin.Context) {
	scope := deptScopeIDs(c)
	q := db.DB.Order("dept_id asc, id asc")
	if len(scope) > 0 {
		q = q.Where("dept_id IN ?", scope)
	}
	var list []models.ShiftConfig
	q.Find(&list)
	c.JSON(http.StatusOK, list)
}

type shiftConfigReq struct {
	ID        uint   `json:"id"`
	DeptID    uint   `json:"dept_id"`
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// UpsertShiftConfig POST /shift-configs 新建或更新班次定义（部门管/超管）
func UpsertShiftConfig(c *gin.Context) {
	var req shiftConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "班次名称不能为空"})
		return
	}
	if !hmRe.MatchString(req.StartTime) || !hmRe.MatchString(req.EndTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "时间格式需为 HH:MM（如 09:00）"})
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
	if req.ID > 0 {
		var old models.ShiftConfig
		if err := db.DB.First(&old, req.ID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "班次不存在"})
			return
		}
		if !canManageDept(c, old.DeptID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "只能修改本部门班次"})
			return
		}
		old.Name = req.Name
		old.StartTime = req.StartTime
		old.EndTime = req.EndTime
		if err := db.DB.Save(&old).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		addLog(c, cl.UserID, cl.Username, "修改班次: "+old.Name+" "+old.StartTime+"-"+old.EndTime)
		c.JSON(http.StatusOK, old)
		return
	}
	var cnt int64
	db.DB.Model(&models.ShiftConfig{}).Where("dept_id = ? AND name = ?", deptID, req.Name).Count(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该部门已存在同名班次"})
		return
	}
	sc := models.ShiftConfig{DeptID: deptID, Name: req.Name, StartTime: req.StartTime, EndTime: req.EndTime}
	if err := db.DB.Create(&sc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "新增班次: "+sc.Name+" "+sc.StartTime+"-"+sc.EndTime)
	c.JSON(http.StatusOK, sc)
}

// DeleteShiftConfig DELETE /shift-configs/:id
func DeleteShiftConfig(c *gin.Context) {
	cl := currentClaims(c)
	var sc models.ShiftConfig
	if err := db.DB.First(&sc, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "班次不存在"})
		return
	}
	if !canManageDept(c, sc.DeptID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能删除本部门班次"})
		return
	}
	if err := db.DB.Delete(&sc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, cl.UserID, cl.Username, "删除班次: "+sc.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
