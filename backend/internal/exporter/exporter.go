// Package exporter 导出能力共享层（v0.41.1）
//
// 为什么单独成包：handlers 与 wecompush 都需要 CSV / 多 sheet 导出，而 handlers 已经
// import 了 wecompush（Dashboard 要读 wp_logs），反向 import 会构成循环依赖。
// 把与业务无关的导出细节沉到这里，两边都只依赖本包，依赖方向保持单向。
package exporter

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// utf8BOM Excel 识别 UTF-8 中文所必需；缺少它时 Excel 按本地 ANSI 解码，中文必然乱码。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// SheetData 一个工作表的完整内容。
type SheetData struct {
	Name    string
	Headers []string
	Rows    [][]interface{}
}

// WriteCSV 输出 CSV（带 UTF-8 BOM），headers + rows 逐行写入。
func WriteCSV(c *gin.Context, headers []string, rows [][]string, filename string) {
	var buf strings.Builder
	buf.Write(utf8BOM) // 单独写 BOM：交给 csv.Writer 会被当成首个字段的普通字符
	w := csv.NewWriter(&buf)
	_ = w.Write(headers)
	for _, r := range rows {
		_ = w.Write(r)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "CSV 生成失败: " + err.Error()})
		return
	}
	writeDownload(c, "text/csv; charset=utf-8", filename, buf.String())
}

// WriteMultiSheetXLSX 生成含多个工作表的工作簿并输出。
// 空 sheet 会被跳过；若全部为空则给出一张提示表（excelize 不允许无工作表的文件）。
func WriteMultiSheetXLSX(c *gin.Context, sheets []SheetData, filename string) {
	usable := make([]SheetData, 0, len(sheets))
	for _, s := range sheets {
		if len(s.Rows) > 0 {
			usable = append(usable, s)
		}
	}
	if len(usable) == 0 {
		usable = []SheetData{{Name: "数据", Headers: []string{"提示"}, Rows: [][]interface{}{{"当前筛选条件下没有数据"}}}}
	}

	f := excelize.NewFile()
	first := true
	for _, s := range usable {
		name := SanitizeSheetName(s.Name)
		if first {
			f.SetSheetName("Sheet1", name)
			first = false
		} else if _, err := f.NewSheet(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "工作表创建失败: " + err.Error()})
			return
		}
		for j, h := range s.Headers {
			col, _ := excelize.CoordinatesToCellName(1+j, 1)
			f.SetCellValue(name, col, h)
		}
		for i, row := range s.Rows {
			for j, v := range row {
				col, _ := excelize.CoordinatesToCellName(1+j, 2+i)
				f.SetCellValue(name, col, v)
			}
		}
	}
	WriteXLSX(c, f, filename)
}

// WriteXLSX 输出 excelize 工作簿（与 handlers 既有 writeXLSX 行为一致：双 filename 头）
func WriteXLSX(c *gin.Context, f *excelize.File, filename string) {
	buf, err := f.WriteToBuffer()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件生成失败"})
		return
	}
	safeName := strings.NewReplacer("\"", "", ";", "", "\r", "", "\n", "").Replace(filename)
	ct := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeName, url.QueryEscape(filename)))
	c.Data(http.StatusOK, ct, buf.Bytes())
}

// GroupRows 按 groupBy 列的值把行分组，保持首次出现顺序，便于"一个条件一个 sheet"。
func GroupRows(headers []string, rows [][]interface{}, groupBy, sheetPrefix string) []SheetData {
	idx := -1
	for i, h := range headers {
		if h == groupBy {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []SheetData{{Name: sheetPrefix, Headers: headers, Rows: rows}}
	}
	var order []string
	buckets := map[string][][]interface{}{}
	for _, r := range rows {
		key := "其他"
		if idx < len(r) {
			if s := strings.TrimSpace(fmt.Sprint(r[idx])); s != "" {
				key = s
			}
		}
		if _, ok := buckets[key]; !ok {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], r)
	}
	out := make([]SheetData, 0, len(order))
	for _, k := range order {
		out = append(out, SheetData{Name: sheetPrefix + "-" + k, Headers: headers, Rows: buckets[k]})
	}
	return out
}

// ToStringRows 把 [][]interface{} 摊平成 [][]string，供 CSV 使用
func ToStringRows(rows [][]interface{}) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		line := make([]string, 0, len(r))
		for _, v := range r {
			line = append(line, fmt.Sprint(v))
		}
		out = append(out, line)
	}
	return out
}

// SanitizeSheetName excelize 表名的硬性限制：≤31 字符且不含 []:*?/\
func SanitizeSheetName(name string) string {
	rep := strings.NewReplacer("[", "", "]", "", ":", "", "*", "", "?", "", "/", "", "\\", "")
	s := rep.Replace(strings.TrimSpace(name))
	if s == "" {
		s = "数据"
	}
	runes := []rune(s) // 按 rune 截断，避免中文被按字节切成半个字
	if len(runes) > 31 {
		runes = runes[:31]
	}
	return string(runes)
}

func writeDownload(c *gin.Context, contentType, filename, body string) {
	safeName := strings.NewReplacer("\"", "", ";", "", "\r", "", "\n", "").Replace(filename)
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeName, url.QueryEscape(filename)))
	c.String(http.StatusOK, body)
}
