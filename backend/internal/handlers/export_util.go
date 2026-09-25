package handlers

// ============================================================================
// 导出工具（v0.41.1）
//
// 实现统一放在 internal/exporter，这里只做薄封装：
//  · 让本包既有调用点（systemlog / authlog 等）保持原来的函数名，改动面最小
//  · 避免 CSV 的 BOM 处理、多 sheet 分组这些细节在两处各写一遍而逐渐漂移
//
// 之所以要抽到独立包：handlers 已经 import 了 wecompush（Dashboard 读 wp_logs），
// 而 wecompush 也要导出日志，反向 import 会成环。共享包让依赖保持单向。
// ============================================================================

import (
	"shiftworkbench/internal/exporter"

	"github.com/gin-gonic/gin"
)

// SheetData 一个工作表的内容（直接复用共享包定义）
type SheetData = exporter.SheetData

// writeCSV 输出 CSV（UTF-8 BOM）
func writeCSV(c *gin.Context, headers []string, rows [][]string, filename string) {
	exporter.WriteCSV(c, headers, rows, filename)
}

// writeMultiSheetXLSX 输出多工作表 xlsx
func writeMultiSheetXLSX(c *gin.Context, sheets []SheetData, filename string) {
	exporter.WriteMultiSheetXLSX(c, sheets, filename)
}

// groupRows 按某列分组，一组一个 sheet
func groupRows(headers []string, rows [][]interface{}, groupBy, sheetPrefix string) []SheetData {
	return exporter.GroupRows(headers, rows, groupBy, sheetPrefix)
}

// rowsToStrings 摊平成字符串二维切片，供 CSV 使用
func rowsToStrings(rows [][]interface{}) [][]string {
	return exporter.ToStringRows(rows)
}
