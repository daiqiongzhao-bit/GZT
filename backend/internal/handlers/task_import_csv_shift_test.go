package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/middleware"
	"shiftworkbench/internal/models"
)

// TestCSVImportShiftNames 守住「CSV/企业微信导入」这条路径的班次归一化。
// regression: enterprise.go 原来自带一张只有「早班/晚班/早晚/全员」的映射表，
// 「中班」「夜班」查不到 → 静默变成「全员」→ 任务提醒会 @ 到所有人。
func TestCSVImportShiftNames(t *testing.T) {
	setupFreezeDB(t) // 同 package 内的内存库脚手架（Task/Log/TaskCompletion/Setting）

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/enterprise/tasks/import",
		strings.NewReader(""))
	c.Request.Header.Set("Content-Type", "application/json")
	cl := &models.Claims{UserID: 1, Username: "admin", Role: models.RoleSuperAdmin, DeptID: 0}
	c.Set(middleware.CtxUserKey, cl)

	text := "标题,班次,类型,时间,优先级,内容,负责人,按周执行\n" +
		"早班任务,早班,每日,08:00,中,,,\n" +
		"中班任务,中班,每日,16:00,中,,,\n" +
		"夜班任务,夜班,每日,23:00,中,,,\n" +
		"未知班次任务,看不懂的班,每日,10:00,中,,,\n"

	created, failed, errs := importTasksFromCSVText(c, text, cl, 0)
	if created != 4 || failed != 0 {
		t.Fatalf("导入应 4 成功 0 失败，实际 created=%d failed=%d errs=%v", created, failed, errs)
	}

	var got []models.Task
	if err := db.DB.Order("id asc").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{"早班", "中班", "夜班", "全员"}
	if len(got) != len(want) {
		t.Fatalf("应导入 %d 条，实际 %d 条", len(want), len(got))
	}
	for i, exp := range want {
		if got[i].Shift != exp {
			t.Errorf("第 %d 条「%s」班次应为 %q，实际 %q", i+1, got[i].Title, exp, got[i].Shift)
		}
	}
}
