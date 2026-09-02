package config

import (
	"os"
	"time"
)

type Config struct {
	Port       string
	JWTSecret  []byte
	AESKey     []byte
	DBPath     string
	AppVersion string
	BackupDir  string
}

var C *Config

func Init() {
	// 强制使用中国时区（北京时间），避免容器默认 UTC 导致任务逾期判断与到点推送错 8 小时
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		time.Local = loc
	} else {
		time.Local = time.FixedZone("CST", 8*3600)
	}
	C = &Config{
		Port:       getEnv("APP_PORT", "8080"),
		JWTSecret:  []byte(getEnv("JWT_SECRET", "shift-workbench-secret-change-me")),
		AESKey:     []byte(padKey(getEnv("AES_KEY", "shift-workbench-aes-key-2024"))),
		DBPath:     getEnv("DB_PATH", "shift_workbench.db"),
		AppVersion: "v0.0.1",
		BackupDir:  getEnv("BACKUP_DIR", "backups"),
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// padKey 将 AES key 补齐/截断到 32 字节（AES-256）
func padKey(s string) string {
	b := []byte(s)
	if len(b) >= 32 {
		return string(b[:32])
	}
	for len(b) < 32 {
		b = append(b, 0)
	}
	return string(b)
}
