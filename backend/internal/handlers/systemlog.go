package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// ListSystemLogs GET /api/system-logs 系统运行日志列表（按级别/来源/关键词/时间筛选 + 分页）
// 用于「系统崩溃/异常」排查；仅管理员可访问（路由层已要求登录）。
func ListSystemLogs(c *gin.Context) {
	q := db.DB.Order("created_at desc")
	if v := c.Query("level"); v != "" {
		q = q.Where("level = ?", strings.ToUpper(v))
	}
	if v := c.Query("source"); v != "" {
		q = q.Where("source LIKE ?", "%"+v+"%")
	}
	if v := c.Query("q"); v != "" {
		q = q.Where("message LIKE ? OR detail LIKE ?", "%"+v+"%", "%"+v+"%")
	}
	if v := c.Query("from"); v != "" {
		q = q.Where("created_at >= ?", v)
	}
	if v := c.Query("to"); v != "" {
		q = q.Where("created_at <= ?", v)
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	var list []models.SystemLog
	if err := q.Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// ExportSystemLogsXLSX GET /api/system-logs/export 导出系统运行日志为 Excel(.xlsx)
func ExportSystemLogsXLSX(c *gin.Context) {
	q := db.DB.Order("created_at desc")
	if v := c.Query("level"); v != "" {
		q = q.Where("level = ?", strings.ToUpper(v))
	}
	if v := c.Query("source"); v != "" {
		q = q.Where("source LIKE ?", "%"+v+"%")
	}
	if v := c.Query("q"); v != "" {
		q = q.Where("message LIKE ? OR detail LIKE ?", "%"+v+"%", "%"+v+"%")
	}
	if v := c.Query("from"); v != "" {
		q = q.Where("created_at >= ?", v)
	}
	if v := c.Query("to"); v != "" {
		q = q.Where("created_at <= ?", v)
	}
	var list []models.SystemLog
	if err := q.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	f := excelize.NewFile()
	sheet := "系统运行日志"
	f.SetSheetName("Sheet1", sheet)
	heads := []string{"ID", "时间", "级别", "来源", "信息", "详情(堆栈)"}
	for j, h := range heads {
		col, _ := excelize.CoordinatesToCellName(1+j, 1)
		f.SetCellValue(sheet, col, h)
	}
	for i, l := range list {
		row := []interface{}{
			l.ID,
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			l.Level,
			l.Source,
			l.Message,
			l.Detail,
		}
		for j, v := range row {
			col, _ := excelize.CoordinatesToCellName(1+j, 2+i)
			f.SetCellValue(sheet, col, v)
		}
	}
	writeXLSX(c, f, "系统运行日志_"+time.Now().Format("20060102_1504")+".xlsx")
}
