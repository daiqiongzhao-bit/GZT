package handlers

import (
	"regexp"
	"strconv"
	"strings"
)

// sanitizeRichContent 服务端白名单净化富文本 HTML，防存储型 XSS。
// 与前端 RichTextEditor.sanitize 保持一致的净化口径，并作为「纵深防御」——
// 任何已登录用户即使绕过前端直接 POST 原始 HTML，落库前也会在此被净化。
// 规则：删危险标签及其内容、删 on* 事件、拦截 javascript: 协议、收敛 style 为安全白名单。
func sanitizeRichContent(html string) string {
	if html == "" {
		return ""
	}
	s := html

	// 0) 删 HTML 注释：从浏览器 / Word 复制粘贴时会带入 <!--StartFragment--> / <!--EndFragment-->
	// 这类剪贴板标记。它们在编辑器里不显示，却会被当文本存进正文、并在变更日志里露出来，提前清掉。
	s = htmlCommentRe.ReplaceAllString(s, "")

	// 1) 删危险容器及其全部内容
	for _, tag := range []string{"script", "style", "iframe", "object", "embed", "link", "meta", "form", "svg", "math"} {
		// <tag ...>...</tag>
		re := regexp.MustCompile(`(?is)<\s*` + tag + `\b[^>]*>[\s\S]*?<\s*/\s*` + tag + `\s*>`)
		s = re.ReplaceAllString(s, "")
		// 自闭合 <tag .../>
		re2 := regexp.MustCompile(`(?is)<\s*` + tag + `\b[^>]*/?>`)
		s = re2.ReplaceAllString(s, "")
	}

	// 2) 删 on* 事件属性（含无引号值）
	reEvent := regexp.MustCompile(`(?i)\son\w+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
	s = reEvent.ReplaceAllString(s, "")

	// 3) 拦截 javascript:/vbscript:/data: 协议（href/src 等，兼容单双引号与无引号）
	reProto := regexp.MustCompile(`(?i)(href|src|xlink:href|action|formaction)\s*=\s*(?:"|')?\s*(?:javascript|vbscript|data)\s*:`)
	s = reProto.ReplaceAllString(s, "")

	// 4) 去掉 style 里非结构性样式（防 url()/expression 等注入），只保留对齐与留白类
	s = stripUnsafeStyle(s)

	return s
}

var safeStyleKeys = []string{
	"text-align", "text-indent", "margin", "padding",
	"margin-left", "margin-right", "margin-top", "margin-bottom",
	"padding-left", "padding-right", "padding-top", "padding-bottom",
	"list-style", "line-height", "font-weight", "white-space",
}

func stripUnsafeStyle(s string) string {
	// 形如 style="..." 或 style='...'，逐个收敛
	re := regexp.MustCompile(`(?i)\sstyle\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		body := ""
		if len(sub) >= 2 && sub[1] != "" {
			body = sub[1]
		} else if len(sub) >= 3 && sub[2] != "" {
			body = sub[2]
		}
		if body == "" {
			return m
		}
		// 去掉被编码的引号变体与危险函数调用
		if strings.Contains(strings.ToLower(body), "javascript") ||
			strings.Contains(strings.ToLower(body), "expression") ||
			strings.Contains(strings.ToLower(body), "@import") ||
			strings.Contains(strings.ToLower(body), "url(") ||
			strings.Contains(strings.ToLower(body), "behavior") {
			return m
		}
		var keep []string
		for _, decl := range strings.Split(body, ";") {
			d := strings.TrimSpace(decl)
			if d == "" {
				continue
			}
			kv := strings.SplitN(d, ":", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(kv[0]))
			val := strings.TrimSpace(kv[1])
			// 值里不允许再出现属性注入残留
			if strings.Contains(strings.ToLower(val), "expression") ||
				strings.Contains(strings.ToLower(val), "javascript:") {
				continue
			}
			ok := false
			for _, sk := range safeStyleKeys {
				if key == sk {
					ok = true
					break
				}
			}
			if ok {
				keep = append(keep, key+":"+val)
			}
		}
		if len(keep) == 0 {
			return ""
		}
		// 保留原有引号风格
		if len(sub) >= 2 && sub[1] != "" {
			return ` style="` + strings.Join(keep, ";") + `"`
		}
		return " style='" + strings.Join(keep, ";") + "'"
	})
}

// sanitizeLink 仅放行 http/https 绝对链接（用于站内通知附带的超链接）。
// 其它协议（javascript:/data:/vbscript: 等）或空值一律返回空串，杜绝伪协议注入。
func sanitizeLink(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 去控制字符与空白，避免注入换行/引号
	s = regexp.MustCompile(`[\x00-\x20\x7f]`).ReplaceAllString(s, "")
	if len(s) > 2048 {
		s = s[:2048]
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return s
	}
	return ""
}

// ==================== 富文本 → 纯文本（变更日志可读化，v0.27.0） ====================

// htmlCommentRe 匹配 HTML 注释。除常规注释外，重点清理从浏览器 / Word 复制时残留的
// <!--StartFragment--> / <!--EndFragment--> 剪贴板标记——它们是历史变更记录「太乱」的元凶之一。
var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// htmlSpaceRe 行内连续空白（含全角空格）压缩用；包级预编译，避免逐行重复编译
var htmlSpaceRe = regexp.MustCompile("[\t\u00a0 ]+")

// htmlToPlainText 把富文本 HTML 转成人类可读的纯文本，用于变更日志里的「修改前 / 修改后」摘录。
// 不追求排版还原，只为让审计记录看得懂：列表成行、段落换行、解码常见实体、清掉剪贴板残留。
// 同时兼容历史脏数据——正文里已被当作文本存下来的 <ol><li> / &nbsp; 也会被还原成正常文字。
func htmlToPlainText(html string) string {
	if html == "" {
		return ""
	}
	s := html
	s = htmlCommentRe.ReplaceAllString(s, "")
	for _, tag := range []string{"script", "style"} {
		re := regexp.MustCompile(`(?is)<\s*` + tag + `\b[^>]*>[\s\S]*?<\s*/\s*` + tag + `\s*>`)
		s = re.ReplaceAllString(s, "")
	}
	// 列表项先转成带圆点的行，避免信息塌成一行
	s = regexp.MustCompile(`(?i)<\s*li\b[^>]*>`).ReplaceAllString(s, "\n• ")
	// 块级标签（含闭合与自闭合）统一换成换行
	s = regexp.MustCompile(`(?i)<\s*/?\s*(br|p|div|li|tr|h[1-6]|blockquote|pre|ul|ol|table|section|article|header|footer)\b[^>]*/?>`).ReplaceAllString(s, "\n")
	// 残余的行内标签整体去掉
	s = regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(s, "")
	s = decodeHTMLEntities(s)
	// 归一空白：行内连续空白压成一个空格（保留换行），连续空行只留一个
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(htmlSpaceRe.ReplaceAllString(ln, " "))
		if ln == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// decodeHTMLEntities 解码常见 HTML 实体（命名实体 + 十进制 / 十六进制数字实体）
func decodeHTMLEntities(s string) string {
	named := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&apos;", "'",
		"&mdash;", "—", "&ndash;", "–", "&hellip;", "…", "&middot;", "·",
		"&ldquo;", "“", "&rdquo;", "”", "&lsquo;", "‘", "&rsquo;", "’",
		"&times;", "×",
	)
	s = named.Replace(s)
	s = regexp.MustCompile(`&#x([0-9a-fA-F]+);`).ReplaceAllStringFunc(s, func(m string) string {
		v, err := strconv.ParseInt(m[3:len(m)-1], 16, 32)
		if err != nil {
			return m
		}
		return string(rune(v))
	})
	s = regexp.MustCompile(`&#(\d+);`).ReplaceAllStringFunc(s, func(m string) string {
		v, err := strconv.ParseInt(m[2:len(m)-1], 10, 32)
		if err != nil {
			return m
		}
		return string(rune(v))
	})
	return s
}
