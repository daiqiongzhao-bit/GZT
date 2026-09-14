package handlers

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"shiftworkbench/internal/data"
)

// ListHolidays GET /holidays?year=2026
// 返回当年中国法定节假日安排（rest 休息日 / work 调休补班日），供前端班表标注与展示。
// 数据为内置只读源（internal/data），不强制排班——仅作软偏好与展示。
func ListHolidays(c *gin.Context) {
	year := c.Query("year")
	y, err := strconv.Atoi(year)
	if err != nil || y < 2000 || y > 2100 {
		y = time.Now().Year()
	}
	from := fmt.Sprintf("%d-01-01", y)
	to := fmt.Sprintf("%d-12-31", y)
	rest, work := data.HolidaysInRange(from, to)

	type item struct {
		Date string `json:"date"`
		Name string `json:"name"`
	}
	restList := make([]item, 0, len(rest))
	workList := make([]item, 0, len(work))
	for d, n := range rest {
		restList = append(restList, item{Date: d, Name: n})
	}
	for d, n := range work {
		workList = append(workList, item{Date: d, Name: n})
	}
	sort.Slice(restList, func(i, j int) bool { return restList[i].Date < restList[j].Date })
	sort.Slice(workList, func(i, j int) bool { return workList[i].Date < workList[j].Date })

	c.JSON(200, gin.H{
		"year": y,
		"rest": restList,
		"work": workList,
	})
}
