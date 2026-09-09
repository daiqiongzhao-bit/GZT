package handlers

import (
	"strings"
	"testing"
	"time"

	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestBackfillBroadcastIDs 验证历史广播（无 broadcast_id）按相同 created_at 聚合并回填聚合键。
func TestBackfillBroadcastIDs(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	_ = gdb.AutoMigrate(&models.Notification{})
	db.DB = gdb

	now := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	rows := []models.Notification{
		{UserID: 11, Kind: "broadcast", Title: "旧公告", Content: "内容A", ActorID: 1, ActorName: "A", CreatedAt: now},
		{UserID: 12, Kind: "broadcast", Title: "旧公告", Content: "内容A", ActorID: 1, ActorName: "A", CreatedAt: now},
		{UserID: 13, Kind: "broadcast", Title: "另一条", Content: "内容B", ActorID: 1, ActorName: "A", CreatedAt: now.Add(1 * time.Second)},
		{UserID: 14, Kind: "schedule", Title: "非广播", Content: "x", ActorID: 1, ActorName: "A", CreatedAt: now}, // 不属于广播
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	BackfillBroadcastIDs()

	var b11, b12, b13 models.Notification
	gdb.First(&b11, rows[0].ID)
	gdb.First(&b12, rows[1].ID)
	gdb.First(&b13, rows[2].ID)
	if b11.BroadcastID == "" {
		t.Fatal("期望同一次广播的行被回填 broadcast_id")
	}
	if b11.BroadcastID != b12.BroadcastID {
		t.Fatalf("同一次广播应共享 broadcast_id: %q vs %q", b11.BroadcastID, b12.BroadcastID)
	}
	if b13.BroadcastID == "" {
		t.Fatal("另一条历史广播也应回填")
	}
	if b11.BroadcastID == b13.BroadcastID {
		t.Fatalf("不同广播不应共用 broadcast_id")
	}
	if !strings.HasPrefix(b11.BroadcastID, "bc_") {
		t.Fatalf("broadcast_id 格式异常: %q", b11.BroadcastID)
	}
}
