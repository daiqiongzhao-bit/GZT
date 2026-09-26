package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// TestDashboardTrends 守住趋势序列的分桶逻辑：今天创建/完成/排班/知识库新增要落进"今天"桶。
// 这能抓住"时区分桶错位"和"People JSON 没按人数汇总"两类静默错误。
func TestDashboardTrends(t *testing.T) {
	setupWaveDB(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	db.DB.Create(&models.Task{Title: "今天创建", CreatedAt: now, Status: "todo"})
	db.DB.Create(&models.Task{Title: "今天完成", CreatedAt: now.Add(-48 * time.Hour), Status: "done", CompletedAt: now})
	db.DB.Create(&models.Task{Title: "昨天完成", CreatedAt: now.Add(-72 * time.Hour), Status: "done", CompletedAt: now.Add(-24 * time.Hour)})
	db.DB.Create(&models.Schedule{Date: today, Shift: "早班", People: `["甲","乙","丙"]`})
	db.DB.Create(&models.KnowledgeEntry{Title: "今天新增", CreatedAt: now, OwnerID: 1})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/dashboard/trends?days=30", nil)
	DashboardTrends(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Days   int `json:"days"`
		Series []struct {
			Date            string `json:"date"`
			TasksCreated    int    `json:"tasks_created"`
			TasksDone       int    `json:"tasks_done"`
			OnDutyPeople    int    `json:"on_duty_people"`
			KnowledgeAdded  int    `json:"knowledge_added"`
		} `json:"series"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Days != 30 {
		t.Fatalf("days want 30 got %d", resp.Days)
	}
	if len(resp.Series) != 30 {
		t.Fatalf("series len want 30 got %d", len(resp.Series))
	}
	last := resp.Series[len(resp.Series)-1] // 最后一天 = 今天
	if last.Date != today {
		t.Fatalf("last date want %s got %s", today, last.Date)
	}
	if last.TasksCreated < 1 {
		t.Errorf("today tasks_created want>=1 got %d", last.TasksCreated)
	}
	if last.TasksDone < 1 {
		t.Errorf("today tasks_done want>=1 got %d", last.TasksDone)
	}
	if last.OnDutyPeople != 3 {
		t.Errorf("today on_duty_people want 3 got %d", last.OnDutyPeople)
	}
	if last.KnowledgeAdded < 1 {
		t.Errorf("today knowledge_added want>=1 got %d", last.KnowledgeAdded)
	}
	// 昨天桶应记到"昨天完成"=1，今天桶 tasks_done 不含昨天的
	if resp.Series[len(resp.Series)-2].TasksDone != 1 {
		t.Errorf("yesterday tasks_done want 1 got %d", resp.Series[len(resp.Series)-2].TasksDone)
	}
}
