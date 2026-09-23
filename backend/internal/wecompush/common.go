package wecompush

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// H 汇聚所有处理器共用的依赖
type H struct {
	DB  *gorm.DB
	Cfg *Config
}

// New 构造处理器
func New(d *gorm.DB, c *Config) *H {
	return &H{DB: d, Cfg: c}
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, data)
}

func fail(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

// ParseColumns 解析任务的输出列（JSON 数组；也兼容逗号分隔的写法）
func ParseColumns(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var arr []string
	if strings.HasPrefix(s, "[") {
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, v := range arr {
				if v = strings.TrimSpace(v); v != "" {
					out = append(out, v)
				}
			}
			return out
		}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NormalizeColumns 把列配置统一存成 JSON 数组形式，便于前端直接消费
func NormalizeColumns(s string) string {
	cols := ParseColumns(s)
	if len(cols) == 0 {
		return ""
	}
	b, err := json.Marshal(cols)
	if err != nil {
		return ""
	}
	return string(b)
}

// ============================================================================
// v0.40.8：链接提取 / 模板渲染 / 文件名清洗
// ============================================================================

// ExtractDocID 从用户输入中提取智能表格 docid。
// 允许直接粘贴完整链接（如 https://doc.weixin.qq.com/smartsheet/xxxx?docid=ABC 或 .../smartsheet/ABC），
// 也兼容直接填 docid。规则：
//  1. URL 查询参数里带 docid= → 取参数值；
//  2. 是 http(s) 链接 → 取路径最后一段；
//  3. 其余原样返回（去掉首尾空白）。
func ExtractDocID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		return s // 已经是 docid
	}
	if u, err := url.Parse(s); err == nil {
		if v := strings.TrimSpace(u.Query().Get("docid")); v != "" {
			return v
		}
		p := strings.Trim(u.Path, "/")
		if p != "" {
			if i := strings.LastIndex(p, "/"); i >= 0 {
				p = p[i+1:]
			}
			if p != "" {
				return p
			}
		}
	}
	return s
}

// RenderTpl 渲染模板变量：{var} 形式；未知变量原样保留。
func RenderTpl(tpl string, vars map[string]string) string {
	out := tpl
	// 先长后短，避免 {filename} 被 {file} 之类的短键截断（当前变量集无嵌套前缀，顺序仅为稳妥）
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		out = strings.ReplaceAll(out, "{"+k+"}", vars[k])
	}
	return out
}

// SanitizeFileName 去掉文件名里的路径分隔符与非法字符（Windows/Unix 通吃），防路径穿越。
func SanitizeFileName(name string) string {
	repl := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\r", "", "\n", "",
	)
	out := strings.TrimSpace(repl.Replace(name))
	if out == "" {
		out = "明细"
	}
	return out
}
