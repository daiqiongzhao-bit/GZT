package wecompush

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// BuildXLSX 生成带样式的明细表：表头加粗浅蓝底、边框、冻结首行
func BuildXLSX(path, sheetName string, headers []string, rows [][]any) error {
	f := excelize.NewFile()
	defer f.Close()

	if sheetName == "" {
		sheetName = "明细"
	}
	if idx, err := f.NewSheet(sheetName); err == nil {
		f.SetActiveSheet(idx)
		if def := f.GetSheetName(0); def != sheetName {
			f.DeleteSheet(def)
		}
	} else {
		sheetName = f.GetSheetName(0)
	}

	headStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DDEBF7"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    thinBorder(),
	})
	if err != nil {
		return err
	}
	bodyStyle, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    thinBorder(),
	})
	if err != nil {
		return err
	}

	// 表头
	head := make([]any, len(headers))
	for i, h := range headers {
		head[i] = h
	}
	if err := f.SetSheetRow(sheetName, "A1", &head); err != nil {
		return err
	}
	lastCol, err := excelize.ColumnNumberToName(len(headers))
	if err != nil {
		return err
	}

	// 数据行
	for i, row := range rows {
		cell := fmt.Sprintf("A%d", i+2)
		vals := make([]any, len(headers))
		for j := range headers {
			if j < len(row) {
				vals[j] = row[j]
			} else {
				vals[j] = ""
			}
		}
		if err := f.SetSheetRow(sheetName, cell, &vals); err != nil {
			return err
		}
	}

	// 样式
	if err := f.SetCellStyle(sheetName, "A1", lastCol+"1", headStyle); err != nil {
		return err
	}
	if len(rows) > 0 {
		if err := f.SetCellStyle(sheetName, "A2", fmt.Sprintf("%s%d", lastCol, len(rows)+1), bodyStyle); err != nil {
			return err
		}
	}

	// 列宽：按表头与内容的粗略长度取合适值
	for i, h := range headers {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			continue
		}
		width := float64(len([]rune(h))*2 + 4)
		if width < 10 {
			width = 10
		}
		if width > 40 {
			width = 40
		}
		_ = f.SetColWidth(sheetName, col, col, width)
	}

	// 冻结首行 + 加筛选
	_ = f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
	_ = f.AutoFilter(sheetName, fmt.Sprintf("A1:%s1", lastCol), nil)

	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("输出路径为空")
	}
	return f.SaveAs(path)
}

func thinBorder() []excelize.Border {
	c := "D9D9D9"
	return []excelize.Border{
		{Type: "left", Color: c, Style: 1},
		{Type: "right", Color: c, Style: 1},
		{Type: "top", Color: c, Style: 1},
		{Type: "bottom", Color: c, Style: 1},
	}
}
