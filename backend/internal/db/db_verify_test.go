package db

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestBuildDSNEnablesWAL 验证 buildDSN 构造的连接串确实让 SQLite 进入 WAL 模式，
// 避免并发写出现 "database is locked"。
func TestBuildDSNEnablesWAL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "verify.db")
	dsn := buildDSN(p)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var mode string
	if err := gdb.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("期望 journal_mode=wal, 实际 %q (dsn=%s)", mode, dsn)
	}
	// 验证 busy_timeout 也被设置
	var bt int
	if err := gdb.Raw("PRAGMA busy_timeout").Scan(&bt).Error; err != nil {
		t.Fatalf("pragma busy_timeout: %v", err)
	}
	if bt <= 0 {
		t.Fatalf("期望 busy_timeout>0, 实际 %d", bt)
	}
}
