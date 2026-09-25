package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// systemLogQuery 列表与导出共用同一套筛选条件（v0.41.1）
// 此前两处各写一遍，新增筛选条件很容易只改一处，导致"页面上看到的"和"导出的"不一致。
func systemLogQuery(c *gin.Context) *gorm.DB {
	q := db.DB
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
	return q
}

// ListSystemLogs GET /api/system-logs 系统运行日志列表（按级别/来源/关键词/时间筛选 + 分页）
// 用于「系统崩溃/异常」排查；仅管理员可访问（路由层已要求登录）。
func ListSystemLogs(c *gin.Context) {
	q := systemLogQuery(c).Order("created_at desc")
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

// ExportSystemLogsXLSX GET /api/system-logs/export 导出系统运行日志
//   format=xlsx|csv（默认 xlsx；CSV 带 UTF-8 BOM，Excel 直接打开不乱码）
//   split=level 时按级别拆表，一个级别一个工作表
func ExportSystemLogsXLSX(c *gin.Context) {
	q := systemLogQuery(c).Order("created_at desc")
	var list []models.SystemLog
	if err := q.Limit(20000).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	heads := []string{"ID", "时间", "级别", "来源", "信息", "详情(堆栈)"}
	rows := make([][]interface{}, 0, len(list))
	for _, l := range list {
		rows = append(rows, []interface{}{
			l.ID,
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			l.Level,
			l.Source,
			l.Message,
			l.Detail,
		})
	}
	stamp := time.Now().Format("20060102_1504")

	if strings.EqualFold(c.Query("format"), "csv") {
		writeCSV(c, heads, rowsToStrings(rows), "系统运行日志_"+stamp+".csv")
		return
	}

	sheets := []SheetData{{Name: "系统运行日志", Headers: heads, Rows: rows}}
	if strings.EqualFold(c.Query("split"), "level") {
		sheets = groupRows(heads, rows, "级别", "系统运行日志")
	}
	writeMultiSheetXLSX(c, sheets, "系统运行日志_"+stamp+".xlsx")
}
