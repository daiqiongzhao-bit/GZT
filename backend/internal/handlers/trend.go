package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// DashboardTrends GET /api/dashboard/trends?days=30 近 N 天业务量趋势序列。
//
// 返回每天：新建任务数 / 完成任务数 / 排班人数（按 People JSON 数组长度汇总）/ 知识库新增数。
// 设计取舍：
//   - 时区用 config.TZ()（P1-3 成果），按本地时区"天"分桶，避免 SQLite DATE() 在 UTC 存值下的错位；
//   - 只用 3 次查询拉近 N 天内的窄数据，再在内存分桶，避免逐天全表扫描（created_at 无索引）。
func DashboardTrends(c *gin.Context) {
	days := 30
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 180 {
			days = n
		}
	}
	loc := config.TZ()
	now := config.Now()
	// 以本地时区"今天 00:00"为右边界对齐，end 为明天 00:00
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Add(24 * time.Hour)
	cutoff := end.Add(-time.Duration(days) * 24 * time.Hour)

	// —— 一次拉取近 N 天内的任务（创建或完成），内存分桶 ——
	type taskRow struct {
		CreatedAt   time.Time
		CompletedAt time.Time
		Status      string
	}
	var tasks []taskRow
	db.DB.Model(&models.Task{}).
		Select("created_at, completed_at, status").
		Where("created_at >= ? OR (status = ? AND completed_at >= ?)", cutoff, "done", cutoff).
		Find(&tasks)

	// —— 一次拉取近 N 天内的排班（date 落在 [cutoff, end)） ——
	var scheds []models.Schedule
	db.DB.Where("date >= ? AND date < ?", cutoff.Format("2006-01-02"), end.Format("2006-01-02")).Find(&scheds)

	// —— 一次拉取近 N 天内的知识库新增 ——
	type kbRow struct {
		CreatedAt time.Time
	}
	var kbs []kbRow
	db.DB.Model(&models.KnowledgeEntry{}).Select("created_at").Where("created_at >= ?", cutoff).Find(&kbs)

	// 初始化每天桶（从最早一天排到今天）
	buckets := make([]gin.H, 0, days)
	idx := map[string]int{}
	for i := days - 1; i >= 0; i-- {
		d0 := end.Add(-time.Duration(i+1) * 24 * time.Hour)
		ds := d0.Format("2006-01-02")
		buckets = append(buckets, gin.H{
			"date":            ds,
			"tasks_created":   0,
			"tasks_done":      0,
			"on_duty_people":  0,
			"knowledge_added": 0,
		})
		idx[ds] = days - 1 - i
	}

	for _, t := range tasks {
		if !t.CreatedAt.IsZero() && t.CreatedAt.After(cutoff) {
			if i, ok := idx[t.CreatedAt.In(loc).Format("2006-01-02")]; ok {
				buckets[i]["tasks_created"] = buckets[i]["tasks_created"].(int) + 1
			}
		}
		if t.Status == "done" && !t.CompletedAt.IsZero() && t.CompletedAt.After(cutoff) {
			if i, ok := idx[t.CompletedAt.In(loc).Format("2006-01-02")]; ok {
				buckets[i]["tasks_done"] = buckets[i]["tasks_done"].(int) + 1
			}
		}
	}
	for _, s := range scheds {
		if i, ok := idx[s.Date]; ok {
			var people []string
			_ = json.Unmarshal([]byte(s.People), &people)
			buckets[i]["on_duty_people"] = buckets[i]["on_duty_people"].(int) + len(people)
		}
	}
	for _, k := range kbs {
		if !k.CreatedAt.IsZero() && k.CreatedAt.After(cutoff) {
			if i, ok := idx[k.CreatedAt.In(loc).Format("2006-01-02")]; ok {
				buckets[i]["knowledge_added"] = buckets[i]["knowledge_added"].(int) + 1
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"days":   days,
		"series": buckets,
	})
}
