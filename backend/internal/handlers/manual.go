package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// 手册种入知识库（v0.16.0）
//
// 设计要点：
//  1. 手册 PDF 随前端产物一起被 //go:embed 编入二进制，由 main.go 读出后交给本函数；
//     这样 NAS / 离线部署也无需额外拷贝文件。
//  2. 每个 App 版本**只在首次启动时种入一次**：Setting.ManualSeededVersion 记录已种版本。
//     于是管理员把该知识条目删除后，同一版本内不会被自动重建；升级到新版本时会重新补齐。
//  3. 条目以「全公司可见」（ScopePublic）种入，保证人人可读；创建者记为超级管理员。
//  4. 手册同时作为条目附件挂上，可在知识库内直接下载 / 在线阅读。

// SeedManualResult 种入结果，便于启动日志输出。
type SeedManualResult struct {
	Seeded  bool   // 本次是否真的种入了
	Reason  string // 未种入的原因（已是最新 / 无超管 / 无 PDF 等）
	EntryID uint   // 种入的条目 ID（Seeded=true 时有效）
	Title   string
	Size    int64
}

// randHex 生成 n 字节的随机十六进制串（用于不可猜测的附件存储名）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下退化为时间戳，仍保证唯一性
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// SeedManualToKnowledge 把内嵌的《GZT 操作使用手册》PDF 种入知识库。
// pdf 为 nil / 空时直接跳过（例如构建产物里没有手册）。
func SeedManualToKnowledge(pdf []byte) SeedManualResult {
	ver := config.C.AppVersion
	if len(pdf) == 0 {
		return SeedManualResult{Reason: "未找到内嵌手册文件"}
	}

	// 已种入过当前版本 → 不再重复（管理员删除后同版本内不重建）
	var st models.Setting
	db.DB.FirstOrCreate(&st, models.Setting{ID: 1})
	if st.ManualSeededVersion == ver {
		return SeedManualResult{Reason: "当前版本 " + ver + " 已种入过"}
	}

	// 条目创建者记为超级管理员（知识库按 owner 记归属）
	var admin models.User
	if err := db.DB.Where("role = ?", models.RoleSuperAdmin).Order("id asc").First(&admin).Error; err != nil {
		return SeedManualResult{Reason: "未找到超级管理员账号，跳过"}
	}

	// 1) 手册落盘到附件目录
	dir := kAttachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SeedManualResult{Reason: "附件目录创建失败: " + err.Error()}
	}
	stored := "manual_" + strings.TrimPrefix(ver, "v") + "_" + randHex(6) + ".pdf"
	dst := filepath.Join(dir, stored)
	if err := os.WriteFile(dst, pdf, 0o644); err != nil {
		return SeedManualResult{Reason: "手册写入失败: " + err.Error()}
	}

	// 2) 建知识条目（全公司可见，已发布状态）
	title := "GZT 操作使用手册 " + ver
	content := buildManualIntro(ver)
	entry := models.KnowledgeEntry{
		Title:     title,
		Category:  "使用手册",
		Content:   content,
		Scope:     models.ScopePublic,
		OwnerID:   admin.ID,
		OwnerName: admin.Username,
		DeptID:    admin.DeptID,
		Status:    "published",
		Tags:      `["手册","操作指南","帮助"]`,
		Summary:   "系统操作与运维手册（随版本自动更新），支持在线阅读与 PDF 下载。",
	}
	if err := db.DB.Create(&entry).Error; err != nil {
		_ = os.Remove(dst)
		return SeedManualResult{Reason: "条目创建失败: " + err.Error()}
	}

	// 3) 挂附件记录
	att := models.KnowledgeAttachment{
		EntryID:    entry.ID,
		FileName:   fmt.Sprintf("GZT操作使用手册_%s.pdf", ver),
		StoredName: stored,
		Mime:       "application/pdf",
		Size:       int64(len(pdf)),
		OwnerID:    admin.ID,
		OwnerName:  admin.Username,
	}
	if err := db.DB.Create(&att).Error; err != nil {
		// 附件记录失败则回滚条目与文件，保持原子性，下次启动可重试
		_ = os.Remove(dst)
		db.DB.Unscoped().Delete(&entry)
		return SeedManualResult{Reason: "附件记录创建失败: " + err.Error()}
	}

	// 4) 维护全文检索索引（与 CreateKnowledge 一致）
	ftsUpsert(entry)

	// 5) 记变更日志（操作人：超级管理员）
	recordKnowledgeLog(entry.ID, &models.Claims{UserID: admin.ID, Username: admin.Username, Role: models.RoleSuperAdmin},
		"create", fmt.Sprintf("系统随 %s 版本发布自动种入手册，并挂载附件「%s」（%d 字节）",
			ver, att.FileName, len(pdf)))

	// 6) 标记已种入版本（放最后：任何一步失败都不会打标，下次启动可重试）
	if err := db.DB.Model(&models.Setting{}).Where("id = ?", 1).
		Update("manual_seeded_version", ver).Error; err != nil {
		// 打标失败不影响本次种入，但下次启动会重复种入一次 —— 记录以便排查
		fmt.Fprintln(os.Stderr, "[manual] 记录手册种入版本失败:", err)
	}

	return SeedManualResult{Seeded: true, EntryID: entry.ID, Title: title, Size: int64(len(pdf))}
}

// buildManualIntro 生成手册条目的正文（在线阅读入口 + 说明）。
// 正文里同时嵌入 PDF 的在线阅读链接，便于在知识库内直接翻阅。
func buildManualIntro(ver string) string {
	return `<h2>《GZT 操作使用手册》` + ver + `</h2>` +
		`<p>本手册随系统版本自动更新，覆盖排班、任务、人员、通知、知识库、工作日志、交接接力、` +
		`审计日志、备份还原与安装升级等全部功能。建议新同事入职时先通读一遍。</p>` +
		`<h3>如何使用</h3>` +
		`<ul>` +
		`<li>点击下方附件即可<b>下载 PDF</b>，用任意 PDF 阅读器打开。</li>` +
		`<li>也可直接在浏览器访问 <code>/manual.pdf</code> 在线阅读。</li>` +
		`<li>手册中的截图与示例数据均为<b>虚构演示数据</b>，不含任何真实信息。</li>` +
		`</ul>` +
		`<h3>本次版本重点（` + ver + `）</h3>` +
		`<ul>` +
		`<li><b>知识库</b>：全文检索、标签、目录树、回收站、置顶收藏、评论 @通知、版本回滚、模板库、Markdown / Word 导入导出、双向链接、统计看板。</li>` +
		`<li><b>工作日志</b>：日期区间 / 关键词 / 按人筛选、分页、统计卡片、附件、一键转交接、编辑留痕、导出。</li>` +
		`<li><b>交接接力</b>：处理时间线、退回并附原因、催办、优先级、截止时间与逾期标记、附件、关联来源、筛选分页。</li>` +
		`<li><b>审计日志</b>：新增表头并改为栅格对齐，缺失值以「—」占位，长列表不再错位。</li>` +
		`</ul>` +
		`<p><i>提示：管理员可自行删除本条目；删除后同一版本内系统不会重建，升级到新版本时会重新补充。</i></p>`
}
