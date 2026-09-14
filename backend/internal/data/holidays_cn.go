package data

import (
	"time"
)

// Holiday 表示一天的国家法定节假日安排。
//   - Kind: "rest" 法定休息日；"work" 调休补班日（周末上班）。
type Holiday struct {
	Date string // YYYY-MM-DD
	Kind string // "rest" | "work"
	Name string // 事由：元旦 / 春节 / 劳动节调休补班 ...
}

// cnHolidays 内置中国法定节假日（2024-2026）。
// 数据依据国务院办公厅每年发布的《关于当年部分节假日安排的通知》：
//   - 2024：国办发明电〔2023〕号
//   - 2025：国办发明电〔2024〕号
//   - 2026：国办发明电〔2025〕7号
//
// 注意：本表为【只读内置数据源】，跨年（2027 及以后）需在此补充，
// 运行时不依赖外部网络，避免第三方接口不稳定导致数据缺失。
// 企业自定义店休日请使用 SpecialRestDay（强制）机制，不在此表维护。
var cnHolidays = []Holiday{
	// ===================== 2024 =====================
	{Date: "2024-01-01", Kind: "rest", Name: "元旦"},
	{Date: "2024-02-04", Kind: "work", Name: "春节调休补班"},
	{Date: "2024-02-10", Kind: "rest", Name: "春节"},
	{Date: "2024-02-11", Kind: "rest", Name: "春节"},
	{Date: "2024-02-12", Kind: "rest", Name: "春节"},
	{Date: "2024-02-13", Kind: "rest", Name: "春节"},
	{Date: "2024-02-14", Kind: "rest", Name: "春节"},
	{Date: "2024-02-15", Kind: "rest", Name: "春节"},
	{Date: "2024-02-16", Kind: "rest", Name: "春节"},
	{Date: "2024-02-17", Kind: "rest", Name: "春节"},
	{Date: "2024-02-18", Kind: "work", Name: "春节调休补班"},
	{Date: "2024-04-04", Kind: "rest", Name: "清明"},
	{Date: "2024-04-05", Kind: "rest", Name: "清明"},
	{Date: "2024-04-06", Kind: "rest", Name: "清明"},
	{Date: "2024-04-28", Kind: "work", Name: "劳动节调休补班"},
	{Date: "2024-05-01", Kind: "rest", Name: "劳动节"},
	{Date: "2024-05-02", Kind: "rest", Name: "劳动节"},
	{Date: "2024-05-03", Kind: "rest", Name: "劳动节"},
	{Date: "2024-05-04", Kind: "rest", Name: "劳动节"},
	{Date: "2024-05-05", Kind: "rest", Name: "劳动节"},
	{Date: "2024-06-10", Kind: "rest", Name: "端午"},
	{Date: "2024-09-14", Kind: "work", Name: "中秋调休补班"},
	{Date: "2024-09-15", Kind: "rest", Name: "中秋"},
	{Date: "2024-09-16", Kind: "rest", Name: "中秋"},
	{Date: "2024-09-17", Kind: "rest", Name: "中秋"},
	{Date: "2024-09-29", Kind: "work", Name: "国庆调休补班"},
	{Date: "2024-10-01", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-02", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-03", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-04", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-05", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-06", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-07", Kind: "rest", Name: "国庆"},
	{Date: "2024-10-12", Kind: "work", Name: "国庆调休补班"},
	// ===================== 2025 =====================
	{Date: "2025-01-01", Kind: "rest", Name: "元旦"},
	{Date: "2025-01-26", Kind: "work", Name: "春节调休补班"},
	{Date: "2025-01-28", Kind: "rest", Name: "春节"},
	{Date: "2025-01-29", Kind: "rest", Name: "春节"},
	{Date: "2025-01-30", Kind: "rest", Name: "春节"},
	{Date: "2025-01-31", Kind: "rest", Name: "春节"},
	{Date: "2025-02-01", Kind: "rest", Name: "春节"},
	{Date: "2025-02-02", Kind: "rest", Name: "春节"},
	{Date: "2025-02-03", Kind: "rest", Name: "春节"},
	{Date: "2025-02-04", Kind: "rest", Name: "春节"},
	{Date: "2025-02-08", Kind: "work", Name: "春节调休补班"},
	{Date: "2025-04-04", Kind: "rest", Name: "清明"},
	{Date: "2025-04-05", Kind: "rest", Name: "清明"},
	{Date: "2025-04-06", Kind: "rest", Name: "清明"},
	{Date: "2025-04-27", Kind: "work", Name: "劳动节调休补班"},
	{Date: "2025-05-01", Kind: "rest", Name: "劳动节"},
	{Date: "2025-05-02", Kind: "rest", Name: "劳动节"},
	{Date: "2025-05-03", Kind: "rest", Name: "劳动节"},
	{Date: "2025-05-04", Kind: "rest", Name: "劳动节"},
	{Date: "2025-05-05", Kind: "rest", Name: "劳动节"},
	{Date: "2025-05-31", Kind: "rest", Name: "端午"},
	{Date: "2025-06-01", Kind: "rest", Name: "端午"},
	{Date: "2025-06-02", Kind: "rest", Name: "端午"},
	{Date: "2025-09-28", Kind: "work", Name: "国庆中秋调休补班"},
	{Date: "2025-10-01", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-02", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-03", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-04", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-05", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-06", Kind: "rest", Name: "中秋"},
	{Date: "2025-10-07", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-08", Kind: "rest", Name: "国庆"},
	{Date: "2025-10-11", Kind: "work", Name: "国庆中秋调休补班"},
	// ===================== 2026 =====================
	{Date: "2026-01-01", Kind: "rest", Name: "元旦"},
	{Date: "2026-01-02", Kind: "rest", Name: "元旦"},
	{Date: "2026-01-03", Kind: "rest", Name: "元旦"},
	{Date: "2026-01-04", Kind: "work", Name: "元旦调休补班"},
	{Date: "2026-02-14", Kind: "work", Name: "春节调休补班"},
	{Date: "2026-02-15", Kind: "rest", Name: "春节"},
	{Date: "2026-02-16", Kind: "rest", Name: "春节"},
	{Date: "2026-02-17", Kind: "rest", Name: "春节"},
	{Date: "2026-02-18", Kind: "rest", Name: "春节"},
	{Date: "2026-02-19", Kind: "rest", Name: "春节"},
	{Date: "2026-02-20", Kind: "rest", Name: "春节"},
	{Date: "2026-02-21", Kind: "rest", Name: "春节"},
	{Date: "2026-02-22", Kind: "rest", Name: "春节"},
	{Date: "2026-02-23", Kind: "rest", Name: "春节"},
	{Date: "2026-02-28", Kind: "work", Name: "春节调休补班"},
	{Date: "2026-04-04", Kind: "rest", Name: "清明"},
	{Date: "2026-04-05", Kind: "rest", Name: "清明"},
	{Date: "2026-04-06", Kind: "rest", Name: "清明"},
	{Date: "2026-05-09", Kind: "work", Name: "劳动节调休补班"},
	{Date: "2026-05-01", Kind: "rest", Name: "劳动节"},
	{Date: "2026-05-02", Kind: "rest", Name: "劳动节"},
	{Date: "2026-05-03", Kind: "rest", Name: "劳动节"},
	{Date: "2026-05-04", Kind: "rest", Name: "劳动节"},
	{Date: "2026-05-05", Kind: "rest", Name: "劳动节"},
	{Date: "2026-06-19", Kind: "rest", Name: "端午"},
	{Date: "2026-06-20", Kind: "rest", Name: "端午"},
	{Date: "2026-06-21", Kind: "rest", Name: "端午"},
	{Date: "2026-09-20", Kind: "work", Name: "国庆调休补班"},
	{Date: "2026-09-25", Kind: "rest", Name: "中秋"},
	{Date: "2026-09-26", Kind: "rest", Name: "中秋"},
	{Date: "2026-09-27", Kind: "rest", Name: "中秋"},
	{Date: "2026-10-01", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-02", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-03", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-04", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-05", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-06", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-07", Kind: "rest", Name: "国庆"},
	{Date: "2026-10-10", Kind: "work", Name: "国庆调休补班"},
}

// HolidaysInRange 返回 [from,to] 区间内（含端点）的法定节假日，
// 分别按 rest（休息）/ work（调休补班）返回 date→name 映射。
func HolidaysInRange(from, to string) (rest, work map[string]string) {
	rest = map[string]string{}
	work = map[string]string{}
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return
	}
	for _, h := range cnHolidays {
		hd, err := time.Parse("2006-01-02", h.Date)
		if err != nil {
			continue
		}
		if (hd.Equal(f) || hd.After(f)) && (hd.Before(t) || hd.Equal(t)) {
			if h.Kind == "rest" {
				rest[h.Date] = h.Name
			} else {
				work[h.Date] = h.Name
			}
		}
	}
	return
}
