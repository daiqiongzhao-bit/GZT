package handlers

import (
	"regexp"
	"strconv"
	"strings"
)

// ==================== 富文本净化（v0.28.0 全面强化）====================
//
// sanitizeRichContent 服务端白名单净化富文本 HTML，防存储型 XSS。
// 这是**唯一防线**（落库前），前端 safeHtml 仅为纵深防御。
//
// v0.28.0 相比旧版的变更（配合 Tiptap 编辑器放开 Word 常用排版能力）：
//  1. safeStyleKeys 16 → 41 项（放开字号/字体/颜色/行距等文本外观样式）
//  2. 【安全修复】旧版 stripUnsafeStyle 检测到危险函数时 `return m` 原样放行整条 style，
//     与净化意图完全相反；改为「逐声明过滤」——危险的丢弃，安全的保留。
//  3. 新增 safeStyleValue() 值级正则校验（光有 key 白名单挡不住 `color:expression(...)`）
//  4. 【新增】sanitizeAttrs() 属性级白名单：旧版对 style 以外的属性零限制
//  5. 危险标签黑名单 10 → 19 项（补 base/template/textarea/xmp 等逃逸与嵌入载体）
//  6. 伪协议黑名单补 file/blob/filesystem，并对 <a href> 改用**正向白名单**
//  7. 净化前先 decodeHTMLEntities，防 &#106;avascript: 实体编码绕过
//  8. 超长截断（2MB），防超大 payload 撑爆 DB 与 DOM
//
// 规则顺序不可颠倒：属性白名单必须在 style 收敛**之前**，
// 否则属性遍历会把刚删掉的属性重新引入。
func sanitizeRichContent(html string) string {
	if html == "" {
		return ""
	}
	s := html

	// 0) 删 HTML 注释：从浏览器 / Word 复制粘贴时会带入 <!--StartFragment--> / <!--EndFragment-->
	// 这类剪贴板标记。它们在编辑器里不显示，却会被当文本存进正文、并在变更日志里露出来，提前清掉。
	s = htmlCommentRe.ReplaceAllString(s, "")

	// 1) 删危险容器及其全部内容
	for _, tag := range unsafeTags {
		// <tag ...>...</tag>
		re := regexp.MustCompile(`(?is)<\s*` + tag + `\b[^>]*>[\s\S]*?<\s*/\s*` + tag + `\s*>`)
		s = re.ReplaceAllString(s, "")
		// 自闭合 <tag .../>
		re2 := regexp.MustCompile(`(?is)<\s*` + tag + `\b[^>]*/?>`)
		s = re2.ReplaceAllString(s, "")
	}

	// 2) 删 on* 事件属性（含无引号值）
	s = reEventAttr.ReplaceAllString(s, "")

	// 3) 属性级白名单：只放行白名单内的属性，其余一律删除。
	//    ⚠️ 必须在实体解码**之前**：若先解码，&quot; 会被还原成真实引号，
	//    使 font-family:&quot;Times New Roman&quot; 变成属性值内含引号的歧义形态，
	//    导致属性切分失败、样式被误删。
	s = sanitizeAttrs(s)

	// 4) 实体解码：把 &#106;avascript: 之类还原成明文，让后续协议拦截能命中。
	//    放在属性白名单之后，仅服务于协议检测，不影响已完成的属性裁剪。
	s = decodeHTMLEntities(s)

	// 5) 【关键顺序】<a href> 正向白名单必须**先于**伪协议拦截执行。
	//
	//    原因：伪协议拦截若用 `href="javascript:` 只删前缀，会留下 `alert(1)">` 碎片，
	//    该碎片被下一次属性解析当成新属性名（输出 `<a alert(1)">`），既污染 HTML
	//    又可能绕过后续检测。
	//    改用正向白名单先**整条删除**非法 href，就不存在残留问题。
	s = enforceHrefWhitelist(s)

	// 6) 伪协议兜底拦截：覆盖 href 之外的载体（src/action/formaction/xlink:href），
	//    以及白名单漏网的场景。reProtoAttr 匹配**整个属性**（属性名 + = + 值），
	//    一次删干净，不残留碎片。
	s = reProtoAttr.ReplaceAllString(s, "")

	// 7) 收敛 style 为安全白名单（缺陷修复 + 白名单扩充 + 值级校验）
	s = stripUnsafeStyle(s)

	// 8) 超长截断：Content 是 SQLite TEXT 无硬限，防滥用
	if len(s) > maxRichContentLen {
		s = s[:maxRichContentLen]
	}

	return s
}

// unsafeTags 危险标签黑名单：这些标签及其内容会被整段删除。
// v0.28.0 扩充：补入框架/嵌入类（base/template/frame/frameset/applet）与
// 文本逃逸类（textarea/xmp/plaintext）——后者可吞掉后续标签，是历史上有名的绕过手法。
var unsafeTags = []string{
	// 原 v0.9.3 清单
	"script", "style", "iframe", "object", "embed", "link", "meta", "form", "svg", "math",
	// v0.28.0 新增
	"base", "template", "noscript", "frame", "frameset", "applet",
	"textarea", "xmp", "plaintext",
}

// maxRichContentLen 单条知识正文上限 2MB。
// 纯文本知识条目约 100 万字，极为充裕；超出部分截断而非报错，避免用户丢失整篇内容。
const maxRichContentLen = 2 * 1024 * 1024

var (
	reEventAttr = regexp.MustCompile(`(?i)\son\w+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
	// reProtoAttr 匹配**整个属性**（属性名=值），值以危险协议开头。
	// ⚠️ 不能用「只匹配前缀」的写法（如 `href="javascript:`）——那样只删前缀会留下
	// `alert(1)">` 碎片，被下次解析当成新属性（输出 `<a alert(1)">`）。
	// 这里用引号感知的值捕获，保证整条删除。
	reProtoAttr = regexp.MustCompile(`(?i)\s(href|src|xlink:href|action|formaction|poster|data|srcset|background)\s*=\s*(?:"\s*(?:javascript|vbscript|data|file|blob|filesystem)\s*:[^"]*"|'\s*(?:javascript|vbscript|data|file|blob|filesystem)\s*:[^']*'|[^\s>]*?(?:javascript|vbscript|data|file|blob|filesystem)\s*:[^\s>]*)`)
	// 匹配开始标签（捕获标签名 + 属性串）
	// 属性串用 [^<>]*? 保证不跨标签；内部嵌套引号由 splitTagAttrs 处理
	reOpenTag = regexp.MustCompile(`(?is)<([a-zA-Z][a-zA-Z0-9]*)((?:\s+[^<>]*?)?)\s*(/?)>`)
	// <a href> 正向白名单前缀：http(s) / mailto / tel / 站内相对路径 / 锚点。
	// ⚠️ 不用 `^/` 单条正则判断站内路径——双斜杠 `//evil.com` 是**协议相对 URL**，
	// 会被浏览器解析成 https://evil.com，必须单独拒绝（Go RE2 不支持负向先行断言）。
	reHrefOK    = regexp.MustCompile(`(?i)^(https?://|mailto:|tel:|#)`)
	reHrefSlash = regexp.MustCompile(`^/`) // 单斜杠开头 = 站内路径；配合 isProtoRelative 排除双斜杠
)

// hrefAllowed 判断 href 是否安全：正向白名单 + 排除协议相对 URL。
func hrefAllowed(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "//") { // 协议相对 URL：浏览器会补上当前协议 → 指向外部站
		return false
	}
	return reHrefOK.MatchString(v) || reHrefSlash.MatchString(v)
}

// ==================== 属性级白名单（v0.28.0 新增）====================

// globalAttrWhitelist 所有标签通用的安全属性。
//
// ⚠️ data-att-id / data-temp-id / data-att-name 三个属性是「附件转正机制」的业务约定：
// workspace.go 的 adoptTempAttachments() 用正则 data-temp-id="(\d+)" 扫描正文，
// 找到临时附件并转正。**一旦在此删掉，新建条目粘贴的图片保存后全部变成死链**。
// data-align / data-indent 分别是图片对齐与段落缩进的落库形态，同理不可删。
var globalAttrWhitelist = map[string]bool{
	"style": true, "class": true, "id": true, "title": true, "dir": true, "lang": true,
	"data-att-id": true, "data-temp-id": true, "data-att-name": true,
	"data-align": true, "data-indent": true, "data-type": true,
}

// tagAttrWhitelist 标签专属属性白名单。
// img 的 width/height 是图片缩放的落库形态，必须放行，否则缩放结果保存即丢。
var tagAttrWhitelist = map[string]map[string]bool{
	"a":     {"href": true, "target": true, "rel": true},
	"img":   {"src": true, "alt": true, "width": true, "height": true},
	"td":    {"colspan": true, "rowspan": true, "colwidth": true},
	"th":    {"colspan": true, "rowspan": true, "colwidth": true},
	"col":   {"span": true, "width": true},
	"ol":    {"start": true, "type": true},
	"table": {"border": true, "cellpadding": true, "cellspacing": true},
}

// splitTagAttrs 把开始标签的属性串切成 `名=值` 片段（引号感知）。
//
// 为什么不用正则：style 值可能含**嵌套引号**（font-family:"Times New Roman",serif），
// 任何基于「引号配对」的正则都会在这里提前闭合，把 style 截断成 `font-family:`。
// 手写扫描按「是否在引号内」决定空格是否分隔属性，天然支持嵌套引号。
func splitTagAttrs(attrStr string) []string {
	var out []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(attrStr); i++ {
		ch := attrStr[i]
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			}
			cur.WriteByte(ch)
		case ch == '"' || ch == '\'':
			inQuote = ch
			cur.WriteByte(ch)
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// parseAttr 把单个 `名=值` 片段拆成名字与值（去引号）
func parseAttr(frag string) (string, string, bool) {
	i := strings.Index(frag, "=")
	if i <= 0 {
		// 无值属性（如 disabled）→ 名字即整体，值留空
		name := strings.ToLower(strings.TrimSpace(frag))
		if name == "" {
			return "", "", false
		}
		return name, "", true
	}
	name := strings.ToLower(strings.TrimSpace(frag[:i]))
	val := strings.TrimSpace(frag[i+1:])
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return name, val, name != ""
}

// sanitizeAttrs 遍历所有开始标签，逐个过滤属性：不在白名单内的一律删除。
// 保留标签本身与文本内容，只削属性——避免破坏文档结构。
func sanitizeAttrs(s string) string {
	return reOpenTag.ReplaceAllStringFunc(s, func(m string) string {
		sub := reOpenTag.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		tagName := strings.ToLower(sub[1])
		attrStr := sub[2]
		selfClose := sub[3]

		var keep []string
		for _, frag := range splitTagAttrs(attrStr) {
			name, val, ok := parseAttr(frag)
			if !ok {
				continue
			}
			// 全局白名单 或 该标签专属白名单
			allowed := globalAttrWhitelist[name]
			if !allowed {
				if perTag, ok := tagAttrWhitelist[tagName]; ok && perTag[name] {
					allowed = true
				}
			}
			if !allowed {
				continue
			}
			// 值里若含尖括号，说明属性上下文已被破坏，丢弃该属性
			if strings.ContainsAny(val, "<>") {
				continue
			}
			if val == "" {
				keep = append(keep, name)
				continue
			}
			keep = append(keep, name+`="`+escapeAttrValue(val)+`"`)
		}

		if len(keep) == 0 {
			if selfClose == "/" {
				return "<" + tagName + "/>"
			}
			return "<" + tagName + ">"
		}
		tail := ">"
		if selfClose == "/" {
			tail = "/>"
		}
		return "<" + tagName + " " + strings.Join(keep, " ") + tail
	})
}

// escapeAttrValue 把属性值里的引号转义，避免闭合属性后注入新属性
func escapeAttrValue(v string) string {
	v = strings.ReplaceAll(v, `"`, "&quot;")
	v = strings.ReplaceAll(v, "'", "&#39;")
	return v
}

// enforceHrefWhitelist 对 <a href> 做正向白名单校验：不匹配安全前缀则删除整个 href 属性
// （保留标签与文本，让链接退化成普通文字，而不是整段内容消失）。
func enforceHrefWhitelist(s string) string {
	return reOpenTag.ReplaceAllStringFunc(s, func(m string) string {
		sub := reOpenTag.FindStringSubmatch(m)
		if len(sub) < 4 || strings.ToLower(sub[1]) != "a" {
			return m
		}
		tagName := strings.ToLower(sub[1])
		selfClose := sub[3]

		var keep []string
		for _, frag := range splitTagAttrs(sub[2]) {
			name, val, ok := parseAttr(frag)
			if !ok {
				continue
			}
			if name == "href" && !hrefAllowed(val) {
				continue // 删除非法 href
			}
			if val == "" {
				keep = append(keep, name)
				continue
			}
			keep = append(keep, name+`="`+escapeAttrValue(val)+`"`)
		}
		if len(keep) == 0 {
			if selfClose == "/" {
				return "<" + tagName + "/>"
			}
			return "<" + tagName + ">"
		}
		tail := ">"
		if selfClose == "/" {
			tail = "/>"
		}
		return "<" + tagName + " " + strings.Join(keep, " ") + tail
	})
}

// ==================== style 白名单（v0.28.0 扩充：16 → 41 项）====================

// safeStyleKeys 富文本内联样式白名单。
// 放开「Word 常用排版能力」，但**一律不放行布局类属性**：
// position / top / left / right / bottom / z-index / float / display /
// transform / filter / content / opacity —— 它们是布局劫持与点击劫持的载体
// （如 position:fixed 可覆盖全局导航栏做钓鱼）。
var safeStyleKeys = []string{
	// ① 文本外观
	"color", "background-color", "background",
	"font-family", "font-size", "font-weight", "font-style",
	"text-decoration", "text-decoration-line", "text-decoration-color",
	"text-transform", "letter-spacing", "word-spacing",
	"vertical-align",
	// ② 段落排版
	"text-align", "text-indent", "line-height", "white-space",
	"direction", "word-break", "overflow-wrap",
	// ③ 盒模型
	"margin", "margin-left", "margin-right", "margin-top", "margin-bottom",
	"padding", "padding-left", "padding-right", "padding-top", "padding-bottom",
	// ④ 尺寸
	"width", "height", "max-width", "min-width",
	// ⑤ 表格 / 列表
	"border", "border-width", "border-style", "border-color", "border-collapse",
	"list-style", "list-style-type", "list-style-position",
}

// safeStyleKeySet 白名单查表（避免每个声明都线性扫描 slice）
var safeStyleKeySet = func() map[string]bool {
	m := make(map[string]bool, len(safeStyleKeys))
	for _, k := range safeStyleKeys {
		m[k] = true
	}
	return m
}()

// ==================== style 值级校验（v0.28.0 新增）====================

var (
	styleDangerRe = regexp.MustCompile(`(?i)(expression|javascript|vbscript|behavior|@import|\\|/\*|url\s*\()`)
	cssColorRe    = regexp.MustCompile(`(?i)^(#([0-9a-f]{3,8})|rgba?\([\d\s,.%]+\)|hsla?\([\d\s,.%deg]+\)|[a-z]{3,20}|transparent|currentcolor)$`)
	cssLenRe      = regexp.MustCompile(`(?i)^(-?\d+(\.\d+)?(px|pt|em|rem|%|vh|vw|ch|ex)?|auto|normal|inherit|initial|0)$`)
	cssWeightRe   = regexp.MustCompile(`(?i)^(normal|bold|bolder|lighter|[1-9]00)$`)
	// 字体族允许字母/汉字/空格/逗号/连字符，以及**成对**的引号（"Times New Roman",serif）。
	// 引号的逃逸风险由「成对性 + 字符集封闭 + 无尖括号/分号」共同保证：
	// 值内不含 < > ; &，即使含引号也无法闭合 style 属性后注入新属性。
	fontFamilyRe = regexp.MustCompile(`^[\p{Han}\w\s,'"-]{1,120}$`)
	wordOnlyRe   = regexp.MustCompile(`(?i)^[a-z-]{1,32}$`)
	cssShorthand = regexp.MustCompile(`(?i)^(-?\d+(\.\d+)?(px|pt|em|rem|%)?\s+){0,3}[a-z#0-9(,).%\s-]{0,64}$`)
	// 盒模型简写：1~4 个「长度值或 auto」组合，如 "4px" / "4px 8px" / "1px auto"
	cssBoxRe = regexp.MustCompile(`(?i)^(-?\d+(\.\d+)?(px|pt|em|rem|%|vh|vw|ch|ex)?|auto)(\s+(-?\d+(\.\d+)?(px|pt|em|rem|%|vh|vw|ch|ex)?|auto)){0,3}$`)
	// 边框：宽度 + 可选样式 + 可选颜色，如 "1px solid #ccc"
	cssBorderRe = regexp.MustCompile(`(?i)^(-?\d+(\.\d+)?(px|pt|em|rem)?\s*)?(none|hidden|dotted|dashed|solid|double|groove|ridge|inset|outset)?\s*(#[0-9a-f]{3,8}|rgba?\([\d\s,.%]+\)|[a-z]+)?$`)
)

const maxStyleValueLen = 256

// safeStyleValue 对单个 CSS 声明做值级校验。
// 光有 key 白名单不够：`color:expression(alert(1))` 的 key 是合法的 color。
// 这里按属性类别用正则约束值的形态，并统一拒绝逃逸字符。
func safeStyleValue(key, val string) bool {
	if val == "" || len(val) > maxStyleValueLen {
		return false
	}
	// 危险函数 / 转义 / CSS 注释 / url()
	if styleDangerRe.MatchString(val) {
		return false
	}
	// 跳出属性上下文的危险字符：尖括号 / & 必须拦截（会闭合标签或注入实体）。
	// 引号在 style="..." 内由 styleDangerRe 与下方输出层转义兜底（不闭合外层属性）。
	// ⚠️ v0.28.1 修复：此前对 font-family 也拦截了单引号，导致
	//    `font-family:'Times New Roman', serif`（Word 粘贴常见形态）整条被丢弃，
	//    正文里所有字体声明在保存后丢失 → 用户感知为「排版乱了」。
	//    字体族引号交给字体族白名单 (fontFamilyRe) 约束即可，此处放行。
	if key == "font-family" {
		if strings.ContainsAny(val, "<>&") {
			return false
		}
	} else {
		if strings.ContainsAny(val, "'<>&") {
			return false
		}
	}
	switch key {
	case "color", "background-color", "text-decoration-color", "border-color":
		return cssColorRe.MatchString(val)
	case "font-size", "line-height", "letter-spacing", "word-spacing",
		"width", "height", "max-width", "min-width", "text-indent":
		return cssLenRe.MatchString(val)
	case "margin", "margin-left", "margin-right", "margin-top", "margin-bottom",
		"padding", "padding-left", "padding-right", "padding-top", "padding-bottom":
		// 盒模型可写简写（"4px 8px" / "1px auto" / "4px 8px 4px 8px"）
		return cssBoxRe.MatchString(val)
	case "font-family":
		return fontFamilyRe.MatchString(val)
	case "text-align":
		return oneOf(val, "left", "right", "center", "justify", "start", "end")
	case "font-weight":
		return cssWeightRe.MatchString(val)
	case "vertical-align":
		return oneOf(val, "baseline", "sub", "super", "top", "middle", "bottom", "text-top", "text-bottom")
	case "text-decoration", "text-decoration-line", "text-transform",
		"list-style", "list-style-type", "list-style-position",
		"border-style", "border-collapse", "white-space", "direction",
		"word-break", "overflow-wrap", "font-style", "background":
		return wordOnlyRe.MatchString(val)
	case "border", "border-width":
		// "1px" / "1px solid" / "1px solid #ccc"
		return cssBorderRe.MatchString(val)
	}
	// 兜底：单值长度 或 通用简写
	return cssLenRe.MatchString(val) || cssShorthand.MatchString(val)
}

func oneOf(v string, opts ...string) bool {
	lv := strings.ToLower(strings.TrimSpace(v))
	for _, o := range opts {
		if lv == o {
			return true
		}
	}
	return false
}

// stripUnsafeStyle 逐条收敛 style 属性：按「属性名白名单 + 值级校验」过滤，
// 危险的**声明**被丢弃，安全的**声明**保留。
//
// ⚠️ 安全修复（v0.28.0）：旧实现检测到 style 值含 javascript/expression/url( 等
// 危险函数时直接 `return m` 原样放行**整条 style**，与净化意图完全相反——
// 攻击者只要塞一个 `url(` 就能让整条样式绕过白名单。现已改为逐声明过滤。
func stripUnsafeStyle(s string) string {
	// 形如 style="..." 或 style='...'，逐个收敛。
	//
	// ⚠️ [^"]* 会在 font-family:"Times New Roman",serif 这类**嵌套引号**值上提前闭合，
	// 因此改用 `[^<>]*?` 向前匹配到标签结束前的最后一个同类引号（非贪婪 + 锚定后续 > 或空格）。
	// 该写法不跨标签（靠 [^<>] 排除尖括号），不会误吞相邻标签。
	re := regexp.MustCompile(`(?i)\sstyle\s*=\s*"([^<>]*)"|\sstyle\s*=\s*'([^<>]*)'`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		body := ""
		if len(sub) >= 2 && sub[1] != "" {
			body = sub[1]
		} else if len(sub) >= 3 && sub[2] != "" {
			body = sub[2]
		}
		if body == "" {
			return "" // 空 style 无意义，直接删除整个属性
		}

		var keep []string
		for _, decl := range splitStyleDecls(body) {
			kv := strings.SplitN(decl, ":", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(kv[0]))
			val := strings.TrimSpace(kv[1])

			if !safeStyleKeySet[key] { // ① 属性名白名单
				continue
			}
			if !safeStyleValue(key, val) { // ② 值级校验
				continue
			}
			keep = append(keep, key+":"+val)
		}

		if len(keep) == 0 {
			return "" // 全部声明都不安全 → 删掉整个 style 属性
		}
		// 统一输出双引号形态；值内残留的双引号转义，避免闭合外层 style 属性
		return ` style="` + strings.ReplaceAll(strings.Join(keep, ";"), `"`, "&quot;") + `"`
	})
}

// splitStyleDecls 按分号切分 CSS 声明，且**不在引号内部切分**。
// 用于正确拆分 `font-family:"Times New Roman", serif; color:red` 这类含引号的值。
func splitStyleDecls(body string) []string {
	var out []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(body); i++ {
		ch := body[i]
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			}
			cur.WriteByte(ch)
		case ch == '"' || ch == '\'':
			inQuote = ch
			cur.WriteByte(ch)
		case ch == ';':
			if d := strings.TrimSpace(cur.String()); d != "" {
				out = append(out, d)
			}
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if d := strings.TrimSpace(cur.String()); d != "" {
		out = append(out, d)
	}
	return out
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

// stripAllAttrs 删除所有标签的属性，只保留标签名（供 htmlToPlainText 使用）。
// 复用 reOpenTag + splitTagAttrs（引号感知），因此 style 值内嵌引号也能正确剥离。
func stripAllAttrs(s string) string {
	return reOpenTag.ReplaceAllStringFunc(s, func(m string) string {
		sub := reOpenTag.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		tagName := strings.ToLower(sub[1])
		if sub[3] == "/" {
			return "<" + tagName + "/>"
		}
		return "<" + tagName + ">"
	})
}

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
	// v0.28.0：先把标签属性整体剥掉（保留标签名），再统一去标签。
	// 旧实现直接 `(?s)<[^>]*>` 去标签，遇到属性值内含 `>`（如 style="content:'a>b'"）
	// 会把标签截断在错误位置，导致变更日志出现 style="..." 这类噪声文字。
	// 这里复用 reOpenTag + splitTagAttrs（引号感知），把属性彻底剥掉只留标签名。
	s = stripAllAttrs(s)
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
