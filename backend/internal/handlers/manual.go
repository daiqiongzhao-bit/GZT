package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/db"
	"shiftworkbench/internal/models"
)

// seedKeyManual 系统自动种入手册的持久标记（写入 KnowledgeEntry.SeedKey）。
// 用显式标记而非 title/category/owner 推断来识别自动种入条目：
//   - 误删（false-positive）：标题前缀相同的人工条目不会被牵连（见 B1）；
//   - 漏删（false-negative）：不再依赖「当前最低 id 的超管」，换超管/降权也不会漏掉孤儿（见 B2）。
const seedKeyManual = "manual"

// reLegacyManualTitle 匹配旧版本（v0.16.0/v0.16.1，无 seed_key）自动种入手册的标题：
// 严格锚定为「GZT 操作使用手册 v<主>.<次>.<修订>」，用于一次性回填 seed_key。
// 刻意要求以版本号结尾，从而排除诸如「GZT 操作使用手册 我的补充笔记」这类人工条目。
var reLegacyManualTitle = regexp.MustCompile(`^GZT 操作使用手册 v\d+\.\d+\.\d+$`)

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

	// 0) 先把旧版本遗留（无 seed_key）的自动种入手册打上标记，再统一清理历史版本种入的手册
	//    （含回收站中的），保证全库最多只有一条自动种入的手册。
	//    清理放在新建之前：若清理成功而后续新建失败，则版本标记不会写入，下次启动仍会重试，
	//    永远不会退化为「多份手册并存」的状态；若先建后清，清理一旦失败重复条目就会一直留着。
	backfillLegacySeededManuals()
	for _, old := range findSeededManuals() {
		purgeKnowledgeEntry(old)
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
		SeedKey:   seedKeyManual,
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

// findSeededManuals 返回所有「系统自动种入的手册」条目（含回收站中的），按 id 升序。
// 回收站条目同样占用一份附件文件，必须一并清理，否则会留下 3.45 MB 的孤儿文件。
// 仅以持久标记 SeedKey 判定身份——不依赖 title / category / owner 等可变属性，
// 因此换超管、降权、改标题都不会造成误删或漏删。
func findSeededManuals() []models.KnowledgeEntry {
	var list []models.KnowledgeEntry
	db.DB.Unscoped().
		Where("seed_key = ?", seedKeyManual).
		Order("id asc").
		Find(&list)
	return list
}

// backfillLegacySeededManuals 一次性回填：把由旧版本（v0.16.0/v0.16.1，建表时尚无 seed_key 列）
// 种入、因而没有标记的手册补上 seed_key，否则它们会被当成人工条目而永远留在库里。
// 选取口径（两处刻意选择，防止误伤人工条目）：
//   - 仅取 seed_key 为空或 NULL 且标题以「GZT 操作使用手册 v」开头的候选；
//   - 再在 Go 侧用严格锚定正则 ^GZT 操作使用手册 v\d+\.\d+\.\d+$ 二次筛选，
//     从而排除像「GZT 操作使用手册 我的补充笔记」这类同前缀的人工条目（B1）；
//   - **不加 owner 过滤**：按 owner 过滤正是 B2 的成因（换超管后老行匹配不上）。
// 幂等：打完标记后下次调用不再命中候选。
func backfillLegacySeededManuals() {
	var candidates []models.KnowledgeEntry
	db.DB.Unscoped().
		Where("(seed_key = '' OR seed_key IS NULL) AND title LIKE ?", "GZT 操作使用手册 v%").
		Find(&candidates)
	ids := make([]uint, 0, len(candidates))
	for _, e := range candidates {
		if reLegacyManualTitle.MatchString(strings.TrimSpace(e.Title)) {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	db.DB.Unscoped().Model(&models.KnowledgeEntry{}).
		Where("id IN ?", ids).
		Update("seed_key", seedKeyManual)
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
