package handlers

import (
	"strings"
	"testing"
)

// TestDiagramOutline 图形条目 → 可读大纲：导图按层级缩进，流程图按「节点 + 连线」两段。
// 这是检索索引 / 变更记录 / 导出 / 卡片预览共用的转换，错了会全链路漏出 JSON 记号。
func TestDiagramOutline(t *testing.T) {
	mind := `{"nodeData":{"id":"root","topic":"中心主题","children":[` +
		`{"id":"a","topic":"分支一","children":[{"id":"a1","topic":"叶子"}]},` +
		`{"id":"b","topic":"分支二"}]}}`
	got := mindOutline(mind)
	want := "- 中心主题\n  - 分支一\n    - 叶子\n  - 分支二"
	if got != want {
		t.Errorf("mindOutline 结果不符\n实际:\n%s\n期望:\n%s", got, want)
	}

	flow := `{"nodes":[` +
		`{"id":"n1","type":"rect","properties":{"text":"开始"}},` +
		`{"id":"n2","type":"diamond","properties":{"text":"判断"}},` +
		`{"id":"n3","type":"rect"}],` +
		`"edges":[` +
		`{"sourceNodeId":"n1","targetNodeId":"n2"},` +
		`{"sourceNodeId":"n2","targetNodeId":"n3","properties":{"text":"是"}}]}`
	fout := flowOutline(flow)
	for _, want := range []string{"节点：", "- 开始", "- 判断", "- （未命名）", "连线：", "- 开始 → 判断", "- 判断 → （未命名）（是）"} {
		if !strings.Contains(fout, want) {
			t.Errorf("flowOutline 缺少 %q\n实际:\n%s", want, fout)
		}
	}

	// 畸形 / 空数据不应 panic，返回空串即可
	for _, bad := range []string{"", "   ", "not json", `{"nodeData":null}`, `{"nodes":"x"}`} {
		if s := mindOutline(bad); s != "" {
			t.Errorf("mindOutline(%q) 期望空串，得到 %q", bad, s)
		}
		if s := flowOutline(bad); s != "" {
			t.Errorf("flowOutline(%q) 期望空串，得到 %q", bad, s)
		}
	}
}

// TestKnowledgePlainTextDispatch 文本化入口必须按类型分流：
// 文档走 HTML 转文本，图形走大纲，未知/空类型回落文档（保证历史数据可用）。
func TestKnowledgePlainTextDispatch(t *testing.T) {
	// 文档：HTML 标签必须被摘掉
	doc := knowledgePlainText(KindDoc, "<ol><li>第一步</li><li>第二步</li></ol>")
	if strings.Contains(doc, "<") || !strings.Contains(doc, "第一步") {
		t.Errorf("文档条目应转成纯文本，实际 %q", doc)
	}
	// 空类型（历史数据）等价于文档
	if got := knowledgePlainText("", "<p>老数据</p>"); !strings.Contains(got, "老数据") || strings.Contains(got, "<") {
		t.Errorf("空 kind 应按文档处理，实际 %q", got)
	}
	// 图形：HTML 不该出现，大纲该出现
	got := knowledgePlainText(KindMind, `{"nodeData":{"id":"r","topic":"根","children":[]}}`)
	if got != "- 根" {
		t.Errorf("导图条目应返回大纲，实际 %q", got)
	}
	// 若把 HTML 误当导图内容，转出来应为空（而不是把 HTML 原样吐出去）
	if got := knowledgePlainText(KindMind, "<p>误存成 HTML</p>"); got != "" {
		t.Errorf("导图条目遇到非 JSON 应返回空串，实际 %q", got)
	}
}

// TestValidateDiagramContent 图形条目入库校验：
// 拦住「HTML 被当成导图存进来」和「结构对不上」，同时不能误伤合法内容与空内容。
func TestValidateDiagramContent(t *testing.T) {
	reject := []struct {
		kind, content, why string
	}{
		{KindMind, "<p>hello</p>", "HTML 不是合法 JSON"},
		{KindMind, `{"nodes":[]}`, "导图缺 nodeData"},
		{KindFlow, `{"nodeData":{}}`, "流程图缺 nodes"},
		{KindMind, `{bad json`, "JSON 语法错误"},
	}
	for _, c := range reject {
		if err := validateDiagramContent(c.kind, c.content); err == nil {
			t.Errorf("应拒绝（%s）：%s", c.why, c.content)
		}
	}

	accept := []struct {
		kind, content string
	}{
		{KindMind, `{"nodeData":{"topic":"根"}}`},
		{KindFlow, `{"nodes":[],"edges":[]}`},
		{KindMind, ""},   // 新建后还没画
		{KindFlow, "  "}, // 同上
	}
	for _, c := range accept {
		if err := validateDiagramContent(c.kind, c.content); err != nil {
			t.Errorf("不应拒绝：kind=%s content=%q err=%v", c.kind, c.content, err)
		}
	}
}

// TestNormalizeKind 类型白名单：空 / 未知 / 大小写混写都必须归一到已知值
func TestNormalizeKind(t *testing.T) {
	cases := map[string]string{
		"":       KindDoc,
		"doc":    KindDoc,
		"DOC":    KindDoc,
		"  doc ": KindDoc,
		"mind":   KindMind,
		"MIND":   KindMind,
		"flow":   KindFlow,
		"Flow":   KindFlow,
		"weird":  KindDoc, // 未知值回落文档
	}
	for in, want := range cases {
		if got := normalizeKind(in); got != want {
			t.Errorf("normalizeKind(%q) = %q，期望 %q", in, got, want)
		}
	}
	if !isDiagramKind(KindMind) || !isDiagramKind(KindFlow) || isDiagramKind(KindDoc) || isDiagramKind("") {
		t.Error("isDiagramKind 判定有误")
	}
	if kindLabel(KindMind) != "思维导图" || kindLabel(KindFlow) != "流程图" || kindLabel("") != "文档" {
		t.Error("kindLabel 文案有误")
	}
}
