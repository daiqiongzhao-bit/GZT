package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ---------- 知识条目内容类型（v0.30.0）----------
//
// 知识条目此前只有一种形态：富文本文档（Content 存 HTML）。
// v0.30.0 起支持「思维导图」与「流程图」两种图形化条目，Content 改存对应绘图库的数据 JSON：
//   - mind：mind-elixir 的 MindElixirData，根键 nodeData，节点字段 topic / children
//   - flow：LogicFlow 的 GraphData，nodes[].properties.text + edges[].sourceNodeId / targetNodeId
//
// 仍然复用 Content 字段而不是新加字段，是为了零改动共享挂在 KnowledgeEntry 上的全部能力：
// 目录树、分类、标签、可见范围、协作者、评论、收藏置顶、回收站、版本快照、附件、变更记录。
//
// ⚠️ 关键约束：图形条目的 Content 是 JSON 而不是 HTML，因此凡是「要文本」的地方
// （全文检索索引、变更记录、Markdown/Word 导出、卡片预览）都必须走 knowledgePlainText()
// 转成可读大纲，否则会把 {"id":..,"topic":..} 这类记号当正文暴露出去。
const (
	KindDoc  = "doc"  // 富文本文档（历史数据默认值）
	KindMind = "mind" // 思维导图
	KindFlow = "flow" // 流程图
)

// normalizeKind 内容类型白名单归一：空值 / 未知值一律回落 doc，
// 保证老客户端（不带 kind 字段）与历史数据继续按文档处理。
func normalizeKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case KindMind:
		return KindMind
	case KindFlow:
		return KindFlow
	default:
		return KindDoc
	}
}

// isDiagramKind 是否为图形化条目（正文是 JSON：不能当 HTML 净化，也不能直接当正文渲染）
func isDiagramKind(k string) bool {
	k = normalizeKind(k)
	return k == KindMind || k == KindFlow
}

// kindLabel 类型中文名（用于日志、导出与报错文案）
func kindLabel(k string) string {
	switch normalizeKind(k) {
	case KindMind:
		return "思维导图"
	case KindFlow:
		return "流程图"
	default:
		return "文档"
	}
}

// validateDiagramContent 校验图形条目的正文确实是可解析的 JSON。
//
// 故意不做深度 schema 校验：绘图库升级可能新增/调整字段，校验过严会误伤正常内容。
// 这里只拦住「HTML 被当成导图存进来」这一类明显错误（前端类型选择被绕过时的兜底）。
func validateDiagramContent(kind, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil // 允许空内容：新建后还没开始画
	}
	if !json.Valid([]byte(content)) {
		return errors.New(kindLabel(kind) + "的内容不是合法 JSON，已拒绝保存")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &probe); err != nil {
		return errors.New(kindLabel(kind) + "的内容结构不正确，已拒绝保存")
	}
	switch normalizeKind(kind) {
	case KindMind:
		if _, ok := probe["nodeData"]; !ok {
			return errors.New("思维导图缺少 nodeData 根节点，已拒绝保存")
		}
	case KindFlow:
		if _, ok := probe["nodes"]; !ok {
			return errors.New("流程图缺少 nodes 字段，已拒绝保存")
		}
	}
	return nil
}

// ---------- 图形条目 → 可读纯文本 ----------

// mindNode 仅取大纲所需字段；其余样式/图标/备注等字段解析时自动忽略
type mindNode struct {
	Topic    string     `json:"topic"`
	Children []mindNode `json:"children"`
}

// mindOutline 思维导图 → 缩进大纲（层级用两空格 + "- " 表示）
func mindOutline(raw string) string {
	var root struct {
		NodeData *mindNode `json:"nodeData"`
	}
	if err := json.Unmarshal([]byte(raw), &root); err != nil || root.NodeData == nil {
		return ""
	}
	var b strings.Builder
	var walk func(n *mindNode, depth int)
	walk = func(n *mindNode, depth int) {
		if n == nil || depth > 24 { // 深度上限：防畸形数据递归过深
			return
		}
		if topic := strings.TrimSpace(n.Topic); topic != "" {
			b.WriteString(strings.Repeat("  ", depth))
			b.WriteString("- ")
			b.WriteString(topic)
			b.WriteByte('\n')
		}
		for i := range n.Children {
			walk(&n.Children[i], depth+1)
		}
	}
	walk(root.NodeData, 0)
	return strings.TrimRight(b.String(), "\n")
}

// flowOutline 流程图 → 可读文本：先列节点，再列连线。
// 连线用「节点文字 → 节点文字」而不是 id，人（和搜索）才看得懂。
func flowOutline(raw string) string {
	var g struct {
		Nodes []struct {
			ID         string `json:"id"`
			Properties struct {
				Text string `json:"text"`
			} `json:"properties"`
			// 兼容 text: { value } 这种写法（LogicFlow 的文本也可放在 text.value）
			Text struct {
				Value string `json:"value"`
			} `json:"text"`
		} `json:"nodes"`
		Edges []struct {
			SourceNodeID string `json:"sourceNodeId"`
			TargetNodeID string `json:"targetNodeId"`
			Properties   struct {
				Text string `json:"text"`
			} `json:"properties"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return ""
	}
	label := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		t := strings.TrimSpace(n.Properties.Text)
		if t == "" {
			t = strings.TrimSpace(n.Text.Value)
		}
		if t == "" {
			t = "（未命名）"
		}
		label[n.ID] = t
	}
	var b strings.Builder
	if len(g.Nodes) > 0 {
		b.WriteString("节点：\n")
		for _, n := range g.Nodes {
			b.WriteString("- " + label[n.ID] + "\n")
		}
	}
	if len(g.Edges) > 0 {
		b.WriteString("连线：\n")
		for _, e := range g.Edges {
			s, t := label[e.SourceNodeID], label[e.TargetNodeID]
			if s == "" {
				s = "（未知节点）"
			}
			if t == "" {
				t = "（未知节点）"
			}
			line := fmt.Sprintf("- %s → %s", s, t)
			if txt := strings.TrimSpace(e.Properties.Text); txt != "" {
				line += "（" + txt + "）"
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// knowledgePlainText 按内容类型取「可读纯文本」。
// 全文检索索引、变更记录、Markdown/Word 导出、列表预览统一走这里，
// 保证图形条目在任何「要文本」的场景都不会漏出 JSON 记号。
func knowledgePlainText(kind, content string) string {
	switch normalizeKind(kind) {
	case KindMind:
		return mindOutline(content)
	case KindFlow:
		return flowOutline(content)
	default:
		return htmlToPlainText(content)
	}
}
