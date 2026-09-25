package handlers

// ============================================================================
// v0.41.1 第一波后端改动回归保护
//
// 重点守三件事，都是"改错了不会报错、只会悄悄不准"的类型：
//   1. 登录失败的动作串带 IP 后缀，筛选必须用 LIKE 而不是等值——等值会让"筛登录失败"
//      永远返回空，而接口依然返回 200，极其难发现。
//   2. CSV 必须带 UTF-8 BOM，否则 Excel 打开中文全是乱码，且文件本身完全合法。
//   3. wp_logs 不可用时 Dashboard 统计要降级为 available=false，绝不能让整页 500。
// ============================================================================

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
	"shiftworkbench/internal/wecompush"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupWaveDB 建临时库并跑完整迁移（与线上 schema 一致）
func setupWaveDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wave.db")
	g, err := gorm.Open(sqlite.Open(db.DSN(path)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.MigrateAll(g); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.DB = g
	config.C = &config.Config{DBPath: path}
	return path
}

// newQueryCtx 构造带查询串的测试上下文
func newQueryCtx(t *testing.T, query string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test"+query, nil)
	return c, w
}

// seedAuthLogs 写入覆盖四种动作的审计数据（两个用户，便于验证跨用户）
func seedAuthLogs(t *testing.T) {
	t.Helper()
	rows := []models.Log{
		{UserID: 1, UserName: "张三", Action: "登录系统", IP: "10.0.0.1", Client: "web"},
		{UserID: 1, UserName: "张三", Action: "退出登录", IP: "10.0.0.1", Client: "web"},
		{UserID: 2, UserName: "李四", Action: "登录失败（密码错误）来自 10.0.0.9", IP: "10.0.0.9", Client: "pwa"},
		{UserID: 2, UserName: "李四", Action: "修改个人密码", IP: "10.0.0.9", Client: "web"},
	}
	for _, r := range rows {
		if err := db.DB.Create(&r).Error; err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}
}

// TestListAuthLogsFilters 全局登录审计的筛选口径
func TestListAuthLogsFilters(t *testing.T) {
	setupWaveDB(t)
	seedAuthLogs(t)

	// 1) 不筛选：应拿到全部 4 条，且只含登录审计动作（不会被其它业务日志污染）
	c, w := newQueryCtx(t, "")
	ListAuthLogs(c)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total":4`) {
		t.Fatalf("期望 total=4，实际响应：%s", w.Body.String())
	}

	// 2) 按动作筛「登录失败」：动作串带 IP 后缀，必须靠 LIKE 命中（本测试的核心回归点）
	c, w = newQueryCtx(t, "?action="+urlQuery("登录失败"))
	ListAuthLogs(c)
	if !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("action=登录失败 应命中 1 条（LIKE 匹配），实际：%s", w.Body.String())
	}

	// 3) 按用户筛：只有李四(2) 的 2 条
	c, w = newQueryCtx(t, "?user_id=2")
	ListAuthLogs(c)
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("user_id=2 应命中 2 条，实际：%s", w.Body.String())
	}

	// 4) 关键词按 IP 命中
	c, w = newQueryCtx(t, "?q=10.0.0.9")
	ListAuthLogs(c)
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("q=10.0.0.9 应命中 2 条，实际：%s", w.Body.String())
	}

	// 5) 分页：limit=1 只返回 1 条，但 total 仍是全量
	c, w = newQueryCtx(t, "?limit=1")
	ListAuthLogs(c)
	body := w.Body.String()
	if !strings.Contains(body, `"total":4`) {
		t.Fatalf("分页时 total 应仍为 4，实际：%s", body)
	}
}

// TestExportAuthLogsCSV 导出 CSV：必须带 UTF-8 BOM，且行数 = 表头 + 数据
func TestExportAuthLogsCSV(t *testing.T) {
	setupWaveDB(t)
	seedAuthLogs(t)

	c, w := newQueryCtx(t, "?format=csv")
	ExportAuthLogs(c)
	if w.Code != http.StatusOK {
		t.Fatalf("导出应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if len(body) < 3 || !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("CSV 缺少 UTF-8 BOM，Excel 打开会中文乱码")
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("Content-Type 应为 text/csv，实际 %q", ct)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 5 { // 1 表头 + 4 条数据
		t.Fatalf("期望 5 行（表头+4条），实际 %d 行", len(lines))
	}
	if !strings.Contains(lines[0], "用户名") {
		t.Fatalf("表头异常：%s", lines[0])
	}
}

// TestExportAuthLogsMultiSheet split=action 时按动作拆表，且不产生非法表名
func TestExportAuthLogsMultiSheet(t *testing.T) {
	setupWaveDB(t)
	seedAuthLogs(t)

	c, w := newQueryCtx(t, "?split=action")
	ExportAuthLogs(c)
	if w.Code != http.StatusOK {
		t.Fatalf("多 sheet 导出应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("应输出 xlsx，实际 Content-Type=%q", ct)
	}
	// 输出必须是合法 xlsx（PK zip 头），否则说明工作簿构造失败
	if !bytes.HasPrefix(w.Body.Bytes(), []byte{'P', 'K'}) {
		t.Fatalf("输出不是合法 xlsx 文件")
	}
}

// TestPushRunStatsAggregate 推送运行看板聚合：只统计真实运行，试跑不计入
func TestPushRunStatsAggregate(t *testing.T) {
	setupWaveDB(t)

	// wp_logs 由 MigrateAll 建表，此处应可用
	st := pushRunStats()
	if st["available"] != true {
		t.Fatalf("wp_logs 已建表，期望 available=true，实际 %v", st)
	}

	now := config.Now()
	rows := []wecompush.WpLog{
		{TaskID: 1, TaskName: "打卡提醒", RunAt: now, Trigger: "schedule", Status: "success", Count: 10},
		{TaskID: 1, TaskName: "打卡提醒", RunAt: now, Trigger: "manual", Status: "failed", Count: 0, Message: "boom"},
		{TaskID: 2, TaskName: "日报", RunAt: now, Trigger: "dry-run", Status: "success", Count: 99},
	}
	for _, r := range rows {
		if err := db.DB.Create(&r).Error; err != nil {
			t.Fatalf("seed wp_log: %v", err)
		}
	}

	st = pushRunStats()
	// 试跑(dry-run)被排除，因此今日运行 = 2 条（1 成功 + 1 失败），推送条数 = 10（不含试跑的 99）
	if st["today_runs"] != int64(2) {
		t.Fatalf("期望 today_runs=2（排除试跑），实际 %v", st["today_runs"])
	}
	if st["today_ok"] != int64(1) || st["today_failed"] != int64(1) {
		t.Fatalf("期望 1 成功 1 失败，实际 ok=%v failed=%v", st["today_ok"], st["today_failed"])
	}
	if st["today_pushed"] != int64(10) {
		t.Fatalf("期望 today_pushed=10（不含试跑的 99），实际 %v", st["today_pushed"])
	}
	if st["last_status"] != "failed" {
		t.Fatalf("最后一次运行是 failed，实际 %v", st["last_status"])
	}
	// 1 成功 / 2 运行 = 50%
	if st["week_success_rate"] != 50.0 {
		t.Fatalf("期望成功率 50.0，实际 %v", st["week_success_rate"])
	}
}

// TestPushRunStatsDegrades 表不存在时必须降级而不是让 Dashboard 500
func TestPushRunStatsDegrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nolog.db")
	g, err := gorm.Open(sqlite.Open(db.DSN(path)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 只建日志表，刻意不建 wp_logs
	if err := g.AutoMigrate(&models.Log{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.DB = g
	config.C = &config.Config{DBPath: path}

	st := pushRunStats()
	if st["available"] != false {
		t.Fatalf("wp_logs 缺失时应降级为 available=false，实际 %v", st)
	}
}

// urlQuery 简单转义，避免中文查询串在测试中出问题
func urlQuery(s string) string {
	return strings.NewReplacer(" ", "%20", "（", "%EF%BC%88", "）", "%EF%BC%89").Replace(s)
}
