package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"

	"github.com/gin-gonic/gin"
)

// GetSetting 获取企业设置
func GetSetting(c *gin.Context) {
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	s.Version = config.C.AppVersion
	c.JSON(http.StatusOK, s)
}

type settingReq struct {
	CompanyName string `json:"company_name"`
	Slogan      string `json:"slogan"`
	Copyright   string `json:"copyright"`
	Logo        string `json:"logo"` // 企业 Logo 文件名，空表示使用默认图标
}

// UpdateSetting 更新企业设置（仅超管）
func UpdateSetting(c *gin.Context) {
	var req settingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	s.CompanyName = req.CompanyName
	s.Slogan = req.Slogan
	s.Copyright = req.Copyright
	s.Version = config.C.AppVersion
	db.DB.Save(&s)
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "更新企业设置")
	c.JSON(http.StatusOK, s)
}

// ===================== 企业 Logo =====================

const logoMaxSize = 2 << 20 // 2MB

// logoDir 返回 Logo 存储目录（与数据库同盘，便于随数据目录一起备份）
func logoDir() string {
	dir := filepath.Dir(config.C.DBPath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	if dir != "/" {
		dir = strings.TrimRight(dir, "/")
	}
	return filepath.Join(dir, "uploads")
}

// UploadLogo POST /api/settings/logo 上传企业 Logo（仅超管）
// 只接受 png/jpg/jpeg/webp/svg，限制 2MB；新图保存成功后删除旧图，避免垃圾堆积
func UploadLogo(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要上传的图片"})
		return
	}
	if file.Size > logoMaxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "图片不能超过 2MB"})
		return
	}
	raw := strings.ToLower(filepath.Ext(file.Filename))
	var ext string
	switch raw {
	case ".png":
		ext = ".png"
	case ".jpg", ".jpeg":
		ext = ".jpg"
	case ".webp":
		ext = ".webp"
	case ".svg":
		ext = ".svg"
	case ".ico":
		ext = ".ico"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 PNG / JPG / WEBP / SVG / ICO 格式"})
		return
	}

	dir := logoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "目录创建失败: " + err.Error()})
		return
	}
	name := fmt.Sprintf("logo_%d%s", time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, name)

	// 位图格式（PNG/JPG/ICO）：统一解码 → 抠掉纯色背景变透明 → 限幅 → 重新编码为 PNG
	// 这样 Logo 在任何主题底色上都不会顶着一块白方块
	if ext == ".png" || ext == ".jpg" || ext == ".ico" {
		fh, err := file.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取失败: " + err.Error()})
			return
		}
		data, err := io.ReadAll(fh)
		fh.Close()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取失败: " + err.Error()})
			return
		}
		var img image.Image
		if ext == ".ico" {
			img, err = decodeICO(data)
		} else {
			img, _, err = image.Decode(bytes.NewReader(data))
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无法识别该图片，请换一张或改用 PNG 上传（" + err.Error() + "）"})
			return
		}
		img = removeBackground(fitSize(img, 512))
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "图片处理失败: " + err.Error()})
			return
		}
		// 统一以 PNG 落地（保留 alpha 通道）
		name = fmt.Sprintf("logo_%d.png", time.Now().UnixNano())
		dst = filepath.Join(dir, name)
		if err := os.WriteFile(dst, buf.Bytes(), 0o644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败: " + err.Error()})
			return
		}
	} else if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败: " + err.Error()})
		return
	}

	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	if s.Logo != "" && s.Logo != name {
		_ = os.Remove(filepath.Join(dir, filepath.Base(s.Logo)))
	}
	s.Logo = name
	s.Version = config.C.AppVersion
	db.DB.Save(&s)
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "上传企业 Logo")
	c.JSON(http.StatusOK, s)
}

// DeleteLogo DELETE /api/settings/logo 恢复为默认图标（仅超管）
func DeleteLogo(c *gin.Context) {
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	if s.Logo != "" {
		_ = os.Remove(filepath.Join(logoDir(), filepath.Base(s.Logo)))
		s.Logo = ""
		db.DB.Save(&s)
	}
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "移除企业 Logo")
	c.JSON(http.StatusOK, s)
}

// GetLogo GET /api/settings/logo 公开读取（登录页与侧边栏展示用）
// 带 ?v=文件名 可主动刷新浏览器缓存
func GetLogo(c *gin.Context) {
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	if s.Logo == "" {
		c.Status(http.StatusNotFound)
		return
	}
	p := filepath.Join(logoDir(), filepath.Base(s.Logo))
	f, err := os.Open(p)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png":
		c.Header("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		c.Header("Content-Type", "image/jpeg")
	case ".webp":
		c.Header("Content-Type", "image/webp")
	case ".svg":
		c.Header("Content-Type", "image/svg+xml")
	case ".ico":
		c.Header("Content-Type", "image/x-icon")
	default:
		c.Header("Content-Type", "application/octet-stream")
	}
	c.Header("Cache-Control", "public, max-age=3600")
	if _, err := io.Copy(c.Writer, f); err != nil {
		c.Status(http.StatusInternalServerError)
	}
}

// UpdateTimezone POST /api/settings/timezone 设置系统时区（仅超管），立即生效
func UpdateTimezone(c *gin.Context) {
	var req struct {
		Timezone string `json:"timezone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Timezone) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供时区"})
		return
	}
	loc, err := time.LoadLocation(req.Timezone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的时区标识"})
		return
	}
	var s models.Setting
	db.DB.FirstOrCreate(&s, models.Setting{ID: 1})
	s.Timezone = req.Timezone
	if err := db.DB.Save(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	time.Local = loc // 立即生效：逾期/今日/本月判定与到点推送均按新时区计算
	addLog(c, currentClaims(c).UserID, currentClaims(c).Username, "设置时区: "+req.Timezone)
	c.JSON(http.StatusOK, gin.H{"ok": true, "timezone": req.Timezone, "now": time.Now().Format("2006-01-02 15:04:05")})
}

// ListLogs 系统日志（支持筛选：user_name / action 关键词 / 时间范围 / limit / offset 分页）
func ListLogs(c *gin.Context) {
	q := db.DB.Order("created_at desc")
	if v := c.Query("user_name"); v != "" {
		q = q.Where("user_name LIKE ?", "%"+v+"%")
	}
	if v := c.Query("action"); v != "" {
		q = q.Where("action LIKE ?", "%"+v+"%")
	}
	if v := c.Query("from"); v != "" {
		q = q.Where("created_at >= ?", v)
	}
	if v := c.Query("to"); v != "" {
		q = q.Where("created_at <= ?", v)
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	var list []models.Log
	q.Offset(offset).Limit(limit).Find(&list)
	c.JSON(http.StatusOK, list)
}
