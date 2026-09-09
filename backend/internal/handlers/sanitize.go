package handlers

import (
	"regexp"
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
