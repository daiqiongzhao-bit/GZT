package db

import (
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// buildDSN 构造 SQLite 连接串：开启 WAL + 忙等待，避免并发写出现 "database is locked"，
// 并提升读并发；同时限制连接池，避免句柄耗尽。
func buildDSN(path string) string {
	dsn := path
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}
	if !strings.Contains(dsn, "_pragma") {
		dsn += "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	}
	return dsn
}

var DB *gorm.DB

func Init() error {
	var err error
	DB, err = gorm.Open(sqlite.Open(buildDSN(config.C.DBPath)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return err
	}
	// 连接池：WAL 下允许多读 + 单写，限制连接数避免句柄耗尽
	if sqlDB, e := DB.DB(); e == nil {
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(10 * time.Minute)
	}
	return DB.AutoMigrate(
		&models.Department{},
		&models.ShiftConfig{},
		&models.User{},
		&models.Schedule{},
		&models.Task{},
		&models.TaskCompletion{},
		&models.Webhook{},
		&models.Setting{},
		&models.Log{},
		&models.Template{},
		&models.Notification{},
		&models.ScheduledBroadcast{},
		&models.PushSubscription{},
		&models.KnowledgeEntry{},
		&models.KnowledgeAttachment{},
		&models.KnowledgeTempAttachment{},
		&models.KnowledgeChangeLog{},
		&models.KnowledgeComment{},
		&models.KnowledgeVersion{},
		&models.KnowledgeTemplate{},
		&models.KnowledgeLink{},
		&models.WorkLog{},
		&models.WorkHandover{},
		&models.WorkHandoverEvent{},
		&models.WSFileAttachment{},
		&models.SystemLog{},
	)
}

// Reopen 在数据库文件被替换（还原）后，关闭旧连接并基于已读取的配置重新打开，
// 使 DB 指向新的 SQLite 文件。保留 config.C 中已解析的配置。
func Reopen() error {
	sqlDB, err := DB.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
	return Init()
}
