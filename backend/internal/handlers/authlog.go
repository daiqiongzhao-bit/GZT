package handlers

// ============================================================================
// 全局登录审计（v0.41.1）
//
// 背景：v0.40.8 已经有「单用户登录/登出时间线」（GET /api/users/:id/auth-logs，
// 员工管理里点某人才能看到）。但排查"这个 IP 昨晚是谁在试密码""今天有多少人登出异常"
// 这类问题必须**跨用户**看，单用户视角帮不上忙。本文件补的就是这个缺口。
//
// 数据完全复用既有操作审计表 Log（addLog 已记录 IP/UA/客户端），不新增表、不做迁移，
// 因此属于纯增量改动——这也是把它放进第一波（低风险）的原因。
//
// 动作口径（与 addLog 写入保持一字不差）：
//   登录系统 / 退出登录 / 修改个人密码 / 登录失败（密码错误）来自 <IP>
// ============================================================================

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

// authActionList 登录审计相关的动作前缀（用于下拉筛选与默认值）
var authActionList = []string{"登录系统", "退出登录", "修改个人密码", "登录失败"}

// authLogQuery 构建登录审计查询（列表与导出共用同一套筛选条件，避免两处口径漂移）
func authLogQuery(c *gin.Context) *gorm.DB {
	q := db.DB.Model(&models.Log{}).Where("action IN ? OR action LIKE ?",
		[]string{"登录系统", "退出登录", "修改个人密码"}, "登录失败%")
	if v := c.Query("user_id"); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			q = q.Where("user_id = ?", id)
		}
	}
	if v := strings.TrimSpace(c.Query("action")); v != "" {
		// 用 LIKE 而非等值：登录失败的动作串带 IP 后缀（"登录失败（密码错误）来自 x.x.x.x"），
		// 等值匹配会让"筛登录失败"永远查不到东西。
		q = q.Where("action LIKE ?", "%"+v+"%")
	}
	if v := strings.TrimSpace(c.Query("ip")); v != "" {
		q = q.Where("ip LIKE ?", "%"+v+"%")
	}
	if v := strings.TrimSpace(c.Query("q")); v != "" {
		// 关键词：用户名 / IP / UA 三个维度任一命中
		q = q.Where("user_name LIKE ? OR ip LIKE ? OR ua LIKE ?", "%"+v+"%", "%"+v+"%", "%"+v+"%")
	}
	if v := c.Query("from"); v != "" {
		q = q.Where("created_at >= ?", v)
	}
	if v := c.Query("to"); v != "" {
		q = q.Where("created_at <= ?", v)
	}
	return q
}

// ListAuthLogs GET /api/auth-logs 全局登录审计（跨用户，可筛选 + 分页）
func ListAuthLogs(c *gin.Context) {
	q := authLogQuery(c)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit := 50
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
	var list []models.Log
	if err := q.Order("created_at desc").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "list": list, "actions": authActionList})
}

// ExportAuthLogs GET /api/auth-logs/export 导出全局登录审计
//   format=xlsx|csv（默认 xlsx）
//   split=action 时按动作拆分，一个动作一个工作表（多 sheet 导出）
func ExportAuthLogs(c *gin.Context) {
	q := authLogQuery(c)
	var list []models.Log
	if err := q.Order("created_at desc").Limit(20000).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	headers := []string{"ID", "时间", "用户ID", "用户名", "动作", "来源IP", "客户端", "User-Agent"}
	rows := make([][]interface{}, 0, len(list))
	for _, l := range list {
		rows = append(rows, []interface{}{
			l.ID,
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			l.UserID,
			l.UserName,
			l.Action,
			l.IP,
			l.Client,
			l.UA,
		})
	}
	stamp := time.Now().Format("20060102_1504")

	if strings.EqualFold(c.Query("format"), "csv") {
		writeCSV(c, headers, rowsToStrings(rows), "登录审计_"+stamp+".csv")
		return
	}

	sheets := []SheetData{{Name: "登录审计", Headers: headers, Rows: rows}}
	if strings.EqualFold(c.Query("split"), "action") {
		sheets = groupRows(headers, rows, "动作", "登录审计")
	}
	writeMultiSheetXLSX(c, sheets, "登录审计_"+stamp+".xlsx")
}
