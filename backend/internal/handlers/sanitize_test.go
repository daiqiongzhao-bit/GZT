package handlers

import (
	"strings"
	"testing"
)

// ==================== sanitizeRichContent：XSS 攻击向量 ====================
//
// 这组用例是知识库存储型 XSS 防线的回归测试。
// 每条用例代表一类真实攻击手法，注释里写明「攻击原理」与「期望行为」。

func TestSanitize_DangerousTagsRemoved(t *testing.T) {
	cases := []struct {
		name string
		in   string
		bad  string // 结果中不得出现
	}{
		{"script标签", `<p>a</p><script>alert(1)</script>`, "<script"},
		{"script自闭合", `<p>a</p><script src="x.js"/>`, "<script"},
		{"iframe", `<iframe src="evil"></iframe>`, "<iframe"},
		{"object", `<object data="x"></object>`, "<object"},
		{"embed", `<embed src="x">`, "<embed"},
		{"svg内嵌脚本", `<svg><script>alert(1)</script></svg>`, "<svg"},
		{"math", `<math><mtext></mtext></math>`, "<math"},
		{"form", `<form action="/x"><input></form>`, "<form"},
		{"style标签", `<style>body{display:none}</style>`, "<style"},
		{"link", `<link rel="stylesheet" href="x">`, "<link"},
		{"meta刷新", `<meta http-equiv="refresh" content="0;url=//evil">`, "<meta"},
		// v0.28.0 新增黑名单
		{"base劫持", `<base href="//evil/">`, "<base"},
		{"template", `<template><script>alert(1)</script></template>`, "<template"},
		{"noscript", `<noscript><img src=x onerror=alert(1)></noscript>`, "<noscript"},
		{"frame", `<frame src="evil">`, "<frame"},
		{"frameset", `<frameset><frame src="x"></frameset>`, "<frameset"},
		{"applet", `<applet code="x"></applet>`, "<applet"},
		// 文本逃逸类：能吞掉后续标签，是经典绕过手法
		{"textarea逃逸", `<textarea></textarea><script>alert(1)</script>`, "<textarea"},
		{"xmp逃逸", `<xmp></xmp><script>alert(1)</script>`, "<xmp"},
		{"plaintext逃逸", `<plaintext><script>alert(1)</script>`, "<plaintext"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichContent(c.in)
			if strings.Contains(strings.ToLower(got), strings.ToLower(c.bad)) {
				t.Errorf("危险标签未被清除\n输入: %s\n输出: %s\n不应包含: %s", c.in, got, c.bad)
			}
		})
	}
}

func TestSanitize_EventHandlersRemoved(t *testing.T) {
	cases := []string{
		`<p onclick="alert(1)">x</p>`,
		`<img src="x" onerror="alert(1)">`,
		`<img src="x" onerror=alert(1)>`,
		`<img src="x" onerror='alert(1)'>`,
		`<body onload="alert(1)">`,
		`<div onmouseover="alert(1)" ONFOCUS="alert(2)">x</div>`,
		`<p onanimationstart="alert(1)">x</p>`, // 罕见事件名也要拦
	}
	for _, in := range cases {
		got := sanitizeRichContent(in)
		low := strings.ToLower(got)
		if strings.Contains(low, "onerror") || strings.Contains(low, "onclick") ||
			strings.Contains(low, "onload") || strings.Contains(low, "onmouseover") ||
			strings.Contains(low, "onfocus") || strings.Contains(low, "onanimationstart") {
			t.Errorf("on* 事件属性未被清除\n输入: %s\n输出: %s", in, got)
		}
	}
}

func TestSanitize_ProtoBlocked(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"href javascript", `<a href="javascript:alert(1)">x</a>`},
		{"href vbscript", `<a href="vbscript:msgbox(1)">x</a>`},
		{"img src data", `<img src="data:image/svg+xml;base64,PHN2Zz4=">`},
		{"href file", `<a href="file:///etc/passwd">x</a>`},
		{"img src blob", `<img src="blob:http://x/y">`},
		{"大小写混淆", `<a href="JaVaScRiPt:alert(1)">x</a>`},
		{"前导空白", `<a href="  javascript:alert(1)">x</a>`},
		{"无引号", `<a href=javascript:alert(1)>x</a>`},
		{"单引号", `<a href='javascript:alert(1)'>x</a>`},
		{"实体编码", `<a href="&#106;avascript:alert(1)">x</a>`},
		{"formaction", `<button formaction="javascript:alert(1)">x</button>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRichContent(c.in)
			low := strings.ToLower(got)
			for _, bad := range []string{"javascript:", "vbscript:", "data:", "file:", "blob:"} {
				if strings.Contains(low, bad) {
					t.Errorf("伪协议未被拦截\n输入: %s\n输出: %s\n命中: %s", c.in, got, bad)
					return
				}
			}
		})
	}
}

func TestSanitize_HrefPositiveWhitelist(t *testing.T) {
	// 允许的 href
	allowed := []string{
		`<a href="https://example.com">x</a>`,
		`<a href="http://example.com">x</a>`,
		`<a href="mailto:a@b.com">x</a>`,
		`<a href="tel:10086">x</a>`,
		`<a href="/workspace/knowledge/1">x</a>`,
		`<a href="#anchor">x</a>`,
	}
	for _, in := range allowed {
		got := sanitizeRichContent(in)
		if !strings.Contains(got, "href=") {
			t.Errorf("合法 href 被误删\n输入: %s\n输出: %s", in, got)
		}
	}
	// 拒绝的 href（删除属性但保留标签与文本）
	rejected := []string{
		`<a href="ftp://x">link</a>`,
		`<a href="//evil.com">link</a>`, // 协议相对 URL 不放行
		`<a href="   ">link</a>`,
	}
	for _, in := range rejected {
		got := sanitizeRichContent(in)
		if strings.Contains(got, "href=") {
			t.Errorf("非白名单 href 未被删除\n输入: %s\n输出: %s", in, got)
		}
		if !strings.Contains(got, "link") {
			t.Errorf("删除 href 时误删了文本内容\n输入: %s\n输出: %s", in, got)
		}
	}
}

// ==================== style 白名单与缺陷修复 ====================

func TestSanitize_StyleWhitelistAllowsWordFeatures(t *testing.T) {
	// 这些是 v0.28.0 新放开的 Word 常用排版样式，必须保留
	keep := []string{
		`color:red`,
		`color:#e11d48`,
		`background-color:rgba(255,0,0,0.5)`,
		`font-family:微软雅黑`,
		`font-family:Times New Roman,serif`,             // Tiptap 常见产出（含空格，无引号）
		`font-family:&quot;Times New Roman&quot;,serif`, // HTML 转义形态（decodeHTMLEntities 解码后处理）
		`font-size:18px`,
		`font-size:1.5em`,
		`font-weight:700`,
		`font-style:italic`,
		`text-align:center`,
		`text-align:justify`,
		`text-indent:2em`,
		`line-height:1.8`,
		`letter-spacing:1px`,
		`vertical-align:super`,
		`text-decoration:underline`,
		`border:1px solid #ccc`,
		`border-collapse:collapse`,
		`width:320px`,
		`max-width:100%`,
		`margin-left:2em`,
		`padding:4px 8px`,
		`list-style-type:disc`,
	}
	for _, decl := range keep {
		in := `<p style="` + decl + `">x</p>`
		got := sanitizeRichContent(in)
		key := decl[:strings.Index(decl, ":")]
		if !strings.Contains(got, key+":") {
			t.Errorf("合法样式被误删\n声明: %s\n输出: %s", decl, got)
		}
	}
}

func TestSanitize_StyleWhitelistRejectsDangerous(t *testing.T) {
	// 布局类与危险值一律拒绝
	reject := []struct {
		name string
		decl string
	}{
		{"position布局劫持", `position:fixed`},
		{"z-index", `z-index:99999`},
		{"float", `float:left`},
		{"display", `display:none`},
		{"transform", `transform:scale(2)`},
		{"filter", `filter:blur(2px)`},
		{"opacity", `opacity:0`},
		{"expression", `width:expression(alert(1))`},
		{"url注入", `background:url(javascript:alert(1))`},
		{"url外部请求", `background-image:url(//evil/track)`},
		{"behavior", `behavior:url(x.htc)`},
		{"import", `background:@import url(x)`},
		{"非白名单属性", `box-shadow:0 0 10px red`},
		{"grid", `grid-template-columns:1fr 1fr`},
		{"flex", `flex:1`},
	}
	for _, c := range reject {
		t.Run(c.name, func(t *testing.T) {
			in := `<p style="` + c.decl + `">x</p>`
			got := sanitizeRichContent(in)
			key := c.decl[:strings.Index(c.decl, ":")]
			if strings.Contains(strings.ToLower(got), strings.ToLower(key)+":") {
				t.Errorf("危险样式未被拒绝\n声明: %s\n输出: %s", c.decl, got)
			}
		})
	}
}

// TestSanitize_StyleDefectFixed 是本批改造的核心回归测试。
//
// 旧实现（sanitize.go:69-75）在检测到 style 值含 url(/javascript/expression 等
// 危险函数时直接 `return m` 原样放行**整条 style**，与净化意图完全相反。
// 修复后应改为「逐声明过滤」：危险声明丢弃，安全声明保留。
func TestSanitize_StyleDefectFixed(t *testing.T) {
	// 混合样式：color 安全，background 危险
	in := `<p style="background:url(javascript:alert(1));color:red">x</p>`
	got := sanitizeRichContent(in)

	if strings.Contains(strings.ToLower(got), "url(") ||
		strings.Contains(strings.ToLower(got), "javascript") {
		t.Fatalf("【安全缺陷未修复】危险声明被放行\n输入: %s\n输出: %s", in, got)
	}
	if !strings.Contains(got, "color:red") {
		t.Errorf("修复时误删了同一条 style 中的安全声明\n输出: %s", got)
	}
}

func TestSanitize_StyleEscapesBlocked(t *testing.T) {
	// 通过属性值闭合逃逸的三种手法
	cases := []string{
		`<p style="color:red"onmouseover="alert(1)">x</p>`, // 无空格闭合
		`<p style="color:red">x</p>`,
		`<p style="color:red;` + `"onload="alert(1)">x</p>`, // 引号逃逸
	}
	for _, in := range cases {
		got := sanitizeRichContent(in)
		if strings.Contains(strings.ToLower(got), "alert(1)") {
			t.Errorf("属性闭合逃逸未被阻止\n输入: %s\n输出: %s", in, got)
		}
	}
}

// ==================== 属性级白名单 ====================

// TestSanitize_AttachmentAttrsPreserved 是「附件转正机制」的红线测试。
//
// adoptTempAttachments() 用正则 data-temp-id="(\d+)" 扫描正文找临时附件。
// 一旦属性白名单误删这三个属性，新建条目粘贴的图片保存后全部变成死链。
func TestSanitize_AttachmentAttrsPreserved(t *testing.T) {
	// 临时附件（新建条目未保存时）
	in := `<img src="/api/workspace/temp-attachments/abc.png/download" data-temp-id="12" data-att-name="截图.png" alt="截图.png">`
	got := sanitizeRichContent(in)
	if !strings.Contains(got, `data-temp-id="12"`) {
		t.Fatalf("【附件转正红线】data-temp-id 丢失，图片将变成死链\n输出: %s", got)
	}
	if !strings.Contains(got, `data-att-name="截图.png"`) {
		t.Errorf("data-att-name 丢失\n输出: %s", got)
	}
	if !strings.Contains(got, "/download") {
		t.Errorf("src 丢失\n输出: %s", got)
	}

	// 正式附件（已有条目）
	in2 := `<img src="/api/workspace/knowledge_attachments/xyz.png/download" data-att-id="7" data-att-name="文档.png">`
	got2 := sanitizeRichContent(in2)
	if !strings.Contains(got2, `data-att-id="7"`) {
		t.Fatalf("data-att-id 丢失\n输出: %s", got2)
	}
}

// TestSanitize_ImageResizeAttrsPreserved 图片缩放/对齐属性必须可落库
func TestSanitize_ImageResizeAttrsPreserved(t *testing.T) {
	in := `<img src="/api/x/download" width="320" height="240" data-align="center" alt="a">`
	got := sanitizeRichContent(in)
	for _, want := range []string{`width="320"`, `height="240"`, `data-align="center"`} {
		if !strings.Contains(got, want) {
			t.Errorf("图片属性丢失: %s\n输出: %s", want, got)
		}
	}
}

func TestSanitize_UnknownAttrsRemoved(t *testing.T) {
	in := `<p data-evil="1" contenteditable="true" draggable="true" style="color:red">x</p>`
	got := sanitizeRichContent(in)
	for _, bad := range []string{"data-evil", "contenteditable", "draggable"} {
		if strings.Contains(got, bad) {
			t.Errorf("未授权属性未被删除: %s\n输出: %s", bad, got)
		}
	}
	if !strings.Contains(got, "color:red") {
		t.Errorf("白名单属性被误删\n输出: %s", got)
	}
}

func TestSanitize_TableAttrsPreserved(t *testing.T) {
	in := `<table border="1"><tbody><tr><td colspan="2" rowspan="3">x</td></tr></tbody></table>`
	got := sanitizeRichContent(in)
	for _, want := range []string{`border="1"`, `colspan="2"`, `rowspan="3"`} {
		if !strings.Contains(got, want) {
			t.Errorf("表格属性丢失: %s\n输出: %s", want, got)
		}
	}
}

// ==================== 内容保留（不能误删用户内容）====================

func TestSanitize_PreservesLegitimateContent(t *testing.T) {
	in := `<h2>标题</h2><p>正文<strong>加粗</strong><em>斜体</em><u>下划线</u>` +
		`<s>删除线</s><sup>上标</sup><sub>下标</sub></p>` +
		`<ul><li>项一</li><li>项二</li></ul>` +
		`<ol start="3"><li>三</li></ol>` +
		`<blockquote>引用</blockquote>` +
		`<pre><code>code</code></pre>` +
		`<hr><p style="text-align:center">居中</p>`
	got := sanitizeRichContent(in)
	for _, want := range []string{"标题", "正文", "加粗", "斜体", "下划线", "删除线", "上标", "下标",
		"项一", "项二", "三", "引用", "code", "<hr", "text-align:center"} {
		if !strings.Contains(got, want) {
			t.Errorf("合法内容被误删: %s\n输出: %s", want, got)
		}
	}
}

// 旧编辑器（contenteditable + execCommand）产出的存量 HTML 必须能被正常清洗
func TestSanitize_LegacyEditorOutput(t *testing.T) {
	// execCommand 会产出 <font>、align 属性、border 表格
	in := `<p align="center"><font size="3" color="red" face="宋体">老内容</font></p>` +
		`<table border="1"><tbody><tr><td>&nbsp;</td></tr></tbody></table>` +
		`<img src="/api/workspace/temp-attachments/a.png/download" data-temp-id="9" alt="i">`
	got := sanitizeRichContent(in)
	// 内容与关键属性不能丢（font/align 的形态转换由前端 upgradeLegacyHtml 负责，
	// 后端只保证不破坏内容与附件属性）
	for _, want := range []string{"老内容", "<font", "data-temp-id=\"9\"", "<table"} {
		if !strings.Contains(got, want) {
			t.Errorf("存量内容被破坏: %s\n输出: %s", want, got)
		}
	}
}

func TestSanitize_CommentRemoved(t *testing.T) {
	in := `<!--StartFragment--><p>正文</p><!--EndFragment-->`
	got := sanitizeRichContent(in)
	if strings.Contains(got, "<!--") {
		t.Errorf("HTML 注释未被清除\n输出: %s", got)
	}
	if !strings.Contains(got, "正文") {
		t.Errorf("注释清理误伤正文\n输出: %s", got)
	}
}

func TestSanitize_EmptyAndBoundary(t *testing.T) {
	if got := sanitizeRichContent(""); got != "" {
		t.Errorf("空串应返回空串，实际: %q", got)
	}
	if got := sanitizeRichContent("<p>正常</p>"); got == "" {
		t.Error("正常内容不应被清空")
	}
	// 超长截断
	long := "<p>" + strings.Repeat("字", maxRichContentLen) + "</p>"
	got := sanitizeRichContent(long)
	if len(got) > maxRichContentLen {
		t.Errorf("超长内容未被截断: %d > %d", len(got), maxRichContentLen)
	}
}

func TestSanitize_StyleNormalizedQuotes(t *testing.T) {
	// 单引号形态的 style 应被统一为双引号，避免引号风格混用
	in := `<p style='color:red'>x</p>`
	got := sanitizeRichContent(in)
	if !strings.Contains(got, `style="color:red"`) {
		t.Errorf("style 引号未归一化\n输出: %s", got)
	}
	// 全部声明都不安全 → 整个 style 属性删除
	in2 := `<p style="position:fixed;z-index:9">x</p>`
	got2 := sanitizeRichContent(in2)
	if strings.Contains(got2, "style=") {
		t.Errorf("全不安全时 style 属性应被删除\n输出: %s", got2)
	}
}

// ==================== safeStyleValue 单测 ====================

func TestSafeStyleValue(t *testing.T) {
	ok := []struct{ k, v string }{
		{"color", "#fff"}, {"color", "red"}, {"color", "rgba(1,2,3,0.5)"},
		{"font-size", "16px"}, {"font-size", "1.5em"}, {"line-height", "1.8"},
		{"text-align", "center"}, {"font-weight", "700"}, {"font-weight", "bold"},
		{"vertical-align", "super"}, {"font-family", "微软雅黑"},
		{"width", "320px"}, {"width", "100%"}, {"margin-left", "auto"},
		{"border", "1px solid #ccc"}, {"list-style-type", "disc"},
	}
	for _, c := range ok {
		if !safeStyleValue(c.k, c.v) {
			t.Errorf("应放行但被拒绝: %s:%s", c.k, c.v)
		}
	}
	bad := []struct{ k, v string }{
		{"color", "expression(alert(1))"},
		{"color", "red;background:url(x)"}, // 分号已由外层切分，这里模拟残余
		{"font-size", "url(x)"},
		{"color", "red\"onload=\"alert(1)"}, // 引号逃逸
		{"font-family", "<script>"},
		{"width", "expression(1)"},
		{"color", strings.Repeat("a", 300)}, // 超长
		{"color", ""},
		{"text-align", "center;position:fixed"},
		{"font-size", "1px}body{display:none"},
	}
	for _, c := range bad {
		if safeStyleValue(c.k, c.v) {
			t.Errorf("应拒绝但被放行: [%s]:[%s]", c.k, c.v)
		}
	}
}

// ==================== htmlToPlainText 变更日志 ====================

func TestHtmlToPlainText(t *testing.T) {
	cases := []struct{ name, in, wantSub string }{
		{"去标签", `<p>你好</p>`, "你好"},
		{"列表成行", `<ul><li>a</li><li>b</li></ul>`, "• a"},
		{"实体解码", `<p>a&nbsp;b&amp;c</p>`, "a b&c"},
		{"数字实体", `<p>&#65;&#x42;</p>`, "AB"},
		{"去注释", `<!--x--><p>正文</p>`, "正文"},
		{"去script", `<p>a</p><script>alert(1)</script>`, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := htmlToPlainText(c.in)
			if !strings.Contains(got, c.wantSub) {
				t.Errorf("期望包含 %q，实际 %q", c.wantSub, got)
			}
		})
	}
}

// 属性值内含 `>` 时不应产生属性噪声（v0.28.0 正则修正回归）
//
// 说明：`<p style="content:'a>b'">` 在 HTML 规范下 `>` 会终止标签，
// 浏览器同样如此解析。本用例只要求「不把 `style=` 之类的属性文字泄漏成正文」。
func TestHtmlToPlainText_AttrWithGt(t *testing.T) {
	got := htmlToPlainText(`<p style="content:'a>b'">正文</p>`)
	if strings.Contains(got, "style=") || strings.Contains(got, "content:") {
		t.Errorf("属性噪声未被清除干净: %q", got)
	}
	if !strings.Contains(got, "正文") {
		t.Errorf("正文丢失: %q", got)
	}
}

// 合法形态：属性值内的 `>` 以实体 &gt; 出现（这是唯一符合规范的写法）
func TestHtmlToPlainText_EscapedGt(t *testing.T) {
	got := htmlToPlainText(`<p title="a&gt;b">正文</p>`)
	if !strings.Contains(got, "正文") {
		t.Errorf("正文丢失: %q", got)
	}
	if strings.Contains(got, "title=") {
		t.Errorf("属性未剥离: %q", got)
	}
}

func TestHtmlToPlainText_NoScriptContent(t *testing.T) {
	got := htmlToPlainText(`<p>可见</p><script>var x=1;</script>`)
	if strings.Contains(got, "var x") {
		t.Errorf("script 内容泄漏到纯文本: %q", got)
	}
}

// ==================== sanitizeLink ====================

func TestSanitizeLink(t *testing.T) {
	ok := []string{"https://a.com", "http://a.com/x?y=1"}
	for _, in := range ok {
		if sanitizeLink(in) == "" {
			t.Errorf("合法链接被拒绝: %s", in)
		}
	}
	bad := []string{"javascript:alert(1)", "data:text/html,x", "vbscript:x", "", "  ", "ftp://a.com", "/relative"}
	for _, in := range bad {
		if sanitizeLink(in) != "" {
			t.Errorf("非法链接未被拒绝: %s", in)
		}
	}
}
