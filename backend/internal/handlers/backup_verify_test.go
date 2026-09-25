package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestVerifyBackupOK 使用临时库模拟「线上库」，快照为备份后校验：
// 预期完整性 ok、演练还原成功、表集合一致。
func TestVerifyBackupOK(t *testing.T) {
	dir := t.TempDir()
	livePath := filepath.Join(dir, "live.db")
	backupPath := filepath.Join(dir, "swb-backup-all-2026-01-01-000000.db")

	// 1) 构造一个与线上 schema 一致的「线上库」并写入数据
	live, err := gorm.Open(sqlite.Open(db.DSN(livePath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if err := db.MigrateAll(live); err != nil {
		t.Fatalf("migrate live: %v", err)
	}
	// 写入一条可计数数据，确保表非空
	if err := live.Exec("INSERT INTO logs(action) VALUES('seed')").Error; err != nil {
		t.Fatalf("seed log: %v", err)
	}
	sqlLive, _ := live.DB()
	_ = sqlLive.Close()

	// 2) 把 config 指向该库，snapshotDB 需要 db.DB 全局与 config.C.DBPath
	config.C = &config.Config{DBPath: livePath}
	db.DB, _ = gorm.Open(sqlite.Open(db.DSN(livePath)), &gorm.Config{})

	// 3) 生成备份（VACUUM INTO 一致性快照）
	if err := snapshotDB(backupPath); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// 4) 校验备份
	res, err := verifyBackupFile(backupPath, db.DB)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Verdict != "ok" {
		t.Fatalf("期望 verdict=ok, 实际 %q (msg=%s)", res.Verdict, res.Message)
	}
	if res.Integrity != "ok" {
		t.Fatalf("期望 integrity=ok, 实际 %q", res.Integrity)
	}
	if !res.DrillOK {
		t.Fatalf("期望 drill_ok=true, 实际 false (msg=%s)", res.Message)
	}
	if len(res.MissingTables) != 0 {
		t.Fatalf("期望无缺表, 实际 %v", res.MissingTables)
	}
	// 校验表清单中确实包含 logs 且行数 >=1
	found := false
	for _, tb := range res.Tables {
		if tb.Name == "logs" && tb.Rows >= 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("期望备份含 logs 且行数>=1, 实际 tables=%v", res.Tables)
	}
}

// TestVerifyBackupCorrupt 校验一个被截断的损坏备份应判为 fail。
func TestVerifyBackupCorrupt(t *testing.T) {
	dir := t.TempDir()
	livePath := filepath.Join(dir, "live.db")
	corruptPath := filepath.Join(dir, "swb-backup-all-2026-01-01-000000.db")

	live, err := gorm.Open(sqlite.Open(db.DSN(livePath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if err := db.MigrateAll(live); err != nil {
		t.Fatalf("migrate live: %v", err)
	}
	sqlLive, _ := live.DB()
	_ = sqlLive.Close()

	config.C = &config.Config{DBPath: livePath}
	db.DB, _ = gorm.Open(sqlite.Open(db.DSN(livePath)), &gorm.Config{})

	// 写入明显损坏的内容（非 SQLite 文件头）
	if err := os.WriteFile(corruptPath, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	res, err := verifyBackupFile(corruptPath, db.DB)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Verdict != "fail" {
		t.Fatalf("期望损坏备份 verdict=fail, 实际 %q (msg=%s)", res.Verdict, res.Message)
	}
}
