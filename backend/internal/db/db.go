package db

import (
	"shiftworkbench/internal/config"
	"shiftworkbench/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init() error {
	var err error
	DB, err = gorm.Open(sqlite.Open(config.C.DBPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return err
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
