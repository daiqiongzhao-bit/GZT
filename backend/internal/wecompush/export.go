package wecompush

// ============================================================================
// 推送运行日志导出（v0.41.1）
//
// 补齐此前只有列表（GET /logs）没有导出的缺口：排查"某几天推送是不是都失败了"时，
// 需要在表格里拉时间线、做透视，光靠页面上 100 条列表不够用。
//
// 导出细节统一走 internal/exporter（与 handlers 侧同一份实现），
// 因此 CSV 的 BOM、多 sheet 的分组与表名清洗规则两边天然一致。
// ============================================================================

import (
	"strings"
	"time"

	"shiftworkbench/internal/exporter"

	"github.com/gin-gonic/gin"
)

// ExportLogs GET /api/wecom-push/logs/export 导出推送运行日志
//   format=xlsx|csv（默认 xlsx）
//   split=status 时按状态拆表（成功 / 空 / 失败各一张）
func (h *H) ExportLogs(c *gin.Context) {
	q := h.DB.Model(&WpLog{}).Order("id desc")
	if tid := c.Query("task_id"); tid != "" && tid != "0" {
		q = q.Where("task_id = ?", tid)
	}
	if st := strings.TrimSpace(c.Query("status")); st != "" {
		q = q.Where("status = ?", st)
	}
	if from := c.Query("from"); from != "" {
		q = q.Where("run_at >= ?", from)
	}
	if to := c.Query("to"); to != "" {
		q = q.Where("run_at <= ?", to)
	}
	var items []WpLog
	if err := q.Limit(20000).Find(&items).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}

	headers := []string{"ID", "任务ID", "任务名称", "运行时间", "触发方式", "状态", "推送条数", "耗时(ms)", "文件", "说明"}
	rows := make([][]interface{}, 0, len(items))
	for _, l := range items {
		rows = append(rows, []interface{}{
			l.ID, l.TaskID, l.TaskName,
			l.RunAt.Format("2006-01-02 15:04:05"),
			triggerText(l.Trigger),
			statusText(l.Status),
			l.Count, l.DurationMs, l.FileName, l.Message,
		})
	}
	stamp := time.Now().Format("20060102_1504")

	if strings.EqualFold(c.Query("format"), "csv") {
		exporter.WriteCSV(c, headers, exporter.ToStringRows(rows), "推送运行日志_"+stamp+".csv")
		return
	}

	sheets := []exporter.SheetData{{Name: "推送运行日志", Headers: headers, Rows: rows}}
	if strings.EqualFold(c.Query("split"), "status") {
		sheets = exporter.GroupRows(headers, rows, "状态", "推送运行日志")
	}
	exporter.WriteMultiSheetXLSX(c, sheets, "推送运行日志_"+stamp+".xlsx")
}

// statusText 运行状态转中文（导出给业务看，不直接暴露英文枚举）
func statusText(s string) string {
	switch s {
	case "success":
		return "成功"
	case "empty":
		return "空(无数据)"
	case "failed":
		return "失败"
	default:
		return s
	}
}
