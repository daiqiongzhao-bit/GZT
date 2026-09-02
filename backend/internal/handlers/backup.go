package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// backupLock 保护备份创建过程，避免定时器与手动备份并发写同一目录
var backupLock sync.Mutex

// BackupInfo 备份文件元数据
type BackupInfo struct {
	ID        string `json:"id"`        // 文件名（含 .db）
	Name      string `json:"name"`      // 文件名（含 .db）
	CreatedAt string `json:"created_at"` // 文件修改时间
	Size      int64  `json:"size"`      // 本地文件字节数
	Type      string `json:"type"`      // manual | auto
	Remote    bool   `json:"remote"`    // 是否额外复制到异地
}

// backupConfig 自动备份配置，持久化于 <BackupDir>/config.json
type backupConfig struct {
	Frequency  string `json:"frequency"`   // none | daily | weekly
	Retention  int    `json:"retention"`   // 保留份数（含 local）
	RemoteDir  string `json:"remote_dir"`  // 异地备份目录（可选）
}

const backupPrefix = "swb-backup-"
const backupExt = ".db"

// backupDir 返回本地备份目录并确保存在
func backupDir() (string, error) {
	dir := config.C.BackupDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// configPath 返回 config.json 路径
func configPath() string {
	return filepath.Join(config.C.BackupDir, "config.json")
}

// readBackupConfig 读取自动备份配置（不存在返回默认 none）
func readBackupConfig() backupConfig {
	cfg := backupConfig{Frequency: "none", Retention: 7, RemoteDir: ""}
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 7
	}
	// WebDAV 地址加密存储，读取时解密
	if strings.HasPrefix(cfg.RemoteDir, "dav:") {
		if dec, e := db.Decrypt(strings.TrimPrefix(cfg.RemoteDir, "dav:")); e == nil {
			cfg.RemoteDir = dec
		} else {
			cfg.RemoteDir = ""
		}
	}
	return cfg
}

// writeBackupConfig 写入自动备份配置
func writeBackupConfig(cfg backupConfig) error {
	if _, err := backupDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0o644)
}

// checkpoint 尝试将 WAL 落盘，确保文件复制时数据完整（best-effort）
func checkpoint() {
	_ = db.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
}

// copyFile 整文件复制（直接文件复制，最可靠）
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// CreateBackup 生成一份当前数据库的备份。bType 为 manual/auto。
func CreateBackup(bType string) (*BackupInfo, error) {
	backupLock.Lock()
	defer backupLock.Unlock()

	dir, err := backupDir()
	if err != nil {
		return nil, err
	}
	checkpoint()

	src := config.C.DBPath
	if _, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("源数据库不存在: %w", err)
	}

	ts := time.Now().Format("2006-01-02-150405")
	now := time.Now()
	typeTag := ""
	if bType == "auto" {
		typeTag = "auto-"
	}
	name := backupPrefix + typeTag + ts + backupExt
	localPath := filepath.Join(dir, name)
	if err := copyFile(src, localPath); err != nil {
		return nil, err
	}

	info := &BackupInfo{
		ID:        name,
		Name:      name,
		CreatedAt: now.Format("2006-01-02 15:04:05"),
		Size:      0, // 由 ListBackups 从文件信息填充
		Type:      bType,
		Remote:    false,
	}

	// 异地备份：WebDAV 上传 或 本地另一目录（兼容旧配置）
	cfg := readBackupConfig()
	if cfg.RemoteDir != "" {
		if isWebDAVURL(cfg.RemoteDir) {
			if err := uploadToWebDAV(cfg.RemoteDir, localPath, name); err == nil {
				info.Remote = true
				cleanupRemoteRetention(cfg.RemoteDir, cfg.Retention)
			} else {
				// 上传失败记录日志，不中断本地备份
				_ = db.DB.Create(&models.Log{Action: "异地备份失败: " + err.Error()}).Error
			}
		} else {
			if err := os.MkdirAll(cfg.RemoteDir, 0o755); err == nil {
				if rerr := copyFile(localPath, filepath.Join(cfg.RemoteDir, name)); rerr == nil {
					info.Remote = true
				}
			}
		}
	}

	// 自动清理：保留份数超出则删最旧的（按文件名时间排序）
	enforceRetentionLocked(cfg.Retention)
	return info, nil
}

// enforceRetentionLocked 在已持有 backupLock 时清理最旧备份
func enforceRetentionLocked(retention int) {
	dir, err := backupDir()
	if err != nil || retention <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if len(e.Name()) > len(backupPrefix) && e.Name()[:len(backupPrefix)] == backupPrefix && filepath.Ext(e.Name()) == backupExt {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files) // 文件名含时间戳，字典序即时间序
	if len(files) > retention {
		for _, old := range files[:len(files)-retention] {
			_ = os.Remove(filepath.Join(dir, old))
		}
	}
}

// ListBackups 列出本地已有备份
func ListBackups() ([]BackupInfo, error) {
	dir, err := backupDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	cfg := readBackupConfig()
	var list []BackupInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		nm := e.Name()
		if len(nm) <= len(backupPrefix) || nm[:len(backupPrefix)] != backupPrefix || filepath.Ext(nm) != backupExt {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		// 类型：文件名不含 auto 标记为 manual
		bType := "manual"
		if len(nm) > len(backupPrefix)+len("auto-") && nm[len(backupPrefix):len(backupPrefix)+5] == "auto-" {
			bType = "auto"
		}
		list = append(list, BackupInfo{
			ID:        nm,
			Name:      nm,
			CreatedAt: fi.ModTime().Format("2006-01-02 15:04:05"),
			Size:      fi.Size(),
			Type:      bType,
			Remote:    cfg.RemoteDir != "",
		})
	}
	// 按名称倒序（最新在前）
	sort.Slice(list, func(i, j int) bool { return list[i].Name > list[j].Name })
	return list, nil
}

// DownloadBackup 返回备份文件供前端下载
func DownloadBackup(c *gin.Context, id string) {
	dir, err := backupDir()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "备份目录不可用"})
		return
	}
	path := filepath.Join(dir, filepath.Base(id))
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "备份不存在"})
		return
	}
	c.FileAttachment(path, filepath.Base(id))
}

// RestoreBackup 从备份还原：先把当前库备份为 .bak-before-restore，再覆盖并重建连接
func RestoreBackup(id string) error {
	backupLock.Lock()
	defer backupLock.Unlock()

	dir, err := backupDir()
	if err != nil {
		return err
	}
	src := filepath.Join(dir, filepath.Base(id))
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("备份不存在")
	}

	// 先备份当前 db，以便回滚
	checkpoint()
	currentBackup := config.C.DBPath + ".bak-before-restore"
	if err := copyFile(config.C.DBPath, currentBackup); err != nil {
		return fmt.Errorf("备份当前数据库失败: %w", err)
	}

	// 覆盖当前 db 文件
	if err := copyFile(src, config.C.DBPath); err != nil {
		// 还原备份副本
		_ = copyFile(currentBackup, config.C.DBPath)
		return fmt.Errorf("还原失败: %w", err)
	}

	// 关键：重建 GORM 连接，否则 DB 仍指向旧文件句柄
	if err := db.Reopen(); err != nil {
		_ = copyFile(currentBackup, config.C.DBPath)
		_ = db.Reopen()
		return fmt.Errorf("重建数据库连接失败: %w", err)
	}
	return nil
}

// DeleteBackup 删除某个备份
func DeleteBackup(id string) error {
	backupLock.Lock()
	defer backupLock.Unlock()
	dir, err := backupDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, filepath.Base(id))
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("备份不存在")
	}
	return os.Remove(path)
}

// ---- HTTP handlers ----

// ListBackupsHandler GET /backups
func ListBackupsHandler(c *gin.Context) {
	list, err := ListBackups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// CreateBackupHandler POST /backups 立即手动备份
func CreateBackupHandler(c *gin.Context) {
	info, err := CreateBackup("manual")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "创建手动备份 "+info.Name)
	c.JSON(http.StatusOK, info)
}

// DownloadBackupHandler GET /backups/:id/download
func DownloadBackupHandler(c *gin.Context) {
	DownloadBackup(c, c.Param("id"))
}

// RestoreBackupHandler POST /backups/:id/restore
func RestoreBackupHandler(c *gin.Context) {
	if err := RestoreBackup(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "还原备份 "+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteBackupHandler DELETE /backups/:id
func DeleteBackupHandler(c *gin.Context) {
	if err := DeleteBackup(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "删除备份 "+c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetBackupConfigHandler GET /backup-config
func GetBackupConfigHandler(c *gin.Context) {
	cfg := readBackupConfig()
	c.JSON(http.StatusOK, cfg)
}

// SaveBackupConfigHandler POST /backup-config
func SaveBackupConfigHandler(c *gin.Context) {
	var req backupConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	// 前端按钮文案为「关闭」时提交 off，归一为后端约定值 none
	if req.Frequency == "off" {
		req.Frequency = "none"
	}
	if req.Frequency != "none" && req.Frequency != "daily" && req.Frequency != "weekly" {
		req.Frequency = "none"
	}
	if req.Retention <= 0 {
		req.Retention = 7
	}
	// 异地备份：WebDAV 地址保存前先校验连通性，失败则不保存；凭据加密存储（本地路径保持明文兼容）
	if req.RemoteDir != "" && isWebDAVURL(req.RemoteDir) {
		if err := webdavTestConnect(req.RemoteDir); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "异地备份校验失败（未保存）: " + err.Error()})
			return
		}
		if enc, e := db.Encrypt(req.RemoteDir); e == nil {
			req.RemoteDir = "dav:" + enc
		}
	}
	if err := writeBackupConfig(req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "更新备份配置")
	// 返回解密后的配置，避免前端拿到加密串
	c.JSON(http.StatusOK, readBackupConfig())
}

// StartBackupScheduler 在 main 中 service.Seed() 之后启动自动备份定时器。
// 不依赖系统 cron，纯 Go 后台 goroutine，每分钟检查是否到达计划时间。
// daily：每日 02:30 执行；weekly：每周日 02:30 执行。
func StartBackupScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		var nextRun time.Time
		recompute := func() {
			cfg := readBackupConfig()
			now := time.Now()
			switch cfg.Frequency {
			case "daily":
				next := time.Date(now.Year(), now.Month(), now.Day(), 2, 30, 0, 0, now.Location())
				if !next.After(now) {
					next = next.Add(24 * time.Hour)
				}
				nextRun = next
			case "weekly":
				next := time.Date(now.Year(), now.Month(), now.Day(), 2, 30, 0, 0, now.Location())
				// 找到下一个周日
				for next.Weekday() != time.Sunday || !next.After(now) {
					next = next.Add(24 * time.Hour)
				}
				nextRun = next
			default:
				nextRun = time.Time{} // 关闭
			}
		}
		recompute()
		for range ticker.C {
			if nextRun.IsZero() {
				recompute()
				continue
			}
			if time.Now().Before(nextRun) {
				continue
			}
			// 到达计划时间：执行自动备份
			if _, err := CreateBackup("auto"); err != nil {
				// 仅记录，不中断调度
				_ = db.DB.Create(&models.Log{Action: "自动备份失败: " + err.Error()}).Error
			}
			recompute()
		}
	}()
}

// StartLogRetentionScheduler 审计日志保留策略：每日清理超过保留天数的日志（0=永久保留）。
// 读取 Setting.LogRetentionDays，启动时先清理一次，之后每 24 小时检查。
func StartLogRetentionScheduler() {
	go func() {
		cleanup := func() {
			var s models.Setting
			db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
			if s.LogRetentionDays <= 0 {
				return
			}
			cutoff := time.Now().AddDate(0, 0, -s.LogRetentionDays)
			res := db.DB.Where("created_at < ?", cutoff).Delete(&models.Log{})
			if res.Error == nil && res.RowsAffected > 0 {
				_ = db.DB.Create(&models.Log{Action: fmt.Sprintf("自动清理审计日志 %d 条（保留 %d 天）", res.RowsAffected, s.LogRetentionDays)}).Error
			}
		}
		cleanup()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanup()
		}
	}()
}
