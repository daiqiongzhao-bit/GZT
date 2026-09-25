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
		// 1) 查询参数 docid= 优先
		if v := strings.TrimSpace(u.Query().Get("docid")); v != "" {
			return v
		}
		// 2) 路径取最后一段
		p := strings.Trim(u.Path, "/")
		if p != "" {
			if i := strings.LastIndex(p, "/"); i >= 0 {
				p = p[i+1:]
			}
			// 路径段形似 docid（s3_*/d3_*/带下划线长串）才采用；
			// 否则（smartsheet/sheet/overview 等路由词）继续看 hash。
			if isDocIDLike(p) {
				return p
			}
		}
		// 3) hash（# 之后）里找：?docid= 参数 或 路径形如 detail/s3_xxx
		if h := u.Fragment; h != "" {
			if i := strings.IndexByte(h, '?'); i >= 0 {
				if v := parseDocIDQuery(h[i+1:]); v != "" {
					return v
				}
			}
			if v := lastSegDocID(h); v != "" {
				return v
			}
		}
	}
	return s // 4) 认不出来原样返回，交给下游报错
}

// isDocIDLike 判断一段是否形似企微 docid。
// 已知非 docid 的路由词一律视为否；s3_*/d3_*/带下划线长串视为是。
func isDocIDLike(seg string) bool {
	if seg == "" {
		return false
	}
	switch strings.ToLower(seg) {
	case "smartsheet", "sheet", "doc", "overview", "detail",
		"app", "apps", "list", "home", "page":
		return false
	}
	ls := strings.ToLower(seg)
	if len(seg) >= 4 && (ls == "s3_"+ls[3:] || ls == "d3_"+ls[3:]) {
		return true
	}
	// 形如 s3_xxxxxxxx / d3_xxxxxxxx（首两位 + 下划线）
	if len(seg) >= 3 && (ls[0] == 's' || ls[0] == 'd') && ls[1] == '3' && len(seg) > 2 && seg[2] == '_' {
		return true
	}
	// 其它带下划线的长串也当 docid 处理（避免误伤）
	if strings.Contains(seg, "_") && len(seg) >= 8 {
		return true
	}
	return false
}

// parseDocIDQuery 从 query string 取 docid 参数。
func parseDocIDQuery(qs string) string {
	for _, kv := range strings.Split(qs, "&") {
		if i := strings.IndexByte(kv, '='); i > 0 {
			if strings.EqualFold(kv[:i], "docid") {
				return strings.TrimSpace(kv[i+1:])
			}
		}
	}
	return ""
}

// lastSegDocID 取 hash 末尾的「最后一段」（按 / 或 # 切分），且形似 docid 才返回。
func lastSegDocID(h string) string {
	cur := strings.TrimSpace(h)
	for cur != "" {
		// 反复切分：形如 "#/detail/s3_xxx" 一路剥到 "s3_xxx"
		n := strings.TrimLeft(cur, "#/")
		if n == "" {
			break
		}
		if i := strings.LastIndexByte(n, '/'); i >= 0 {
			cur = n[i+1:]
		} else {
			cur = n
			break
		}
		if isDocIDLike(cur) {
			return cur
		}
	}
	// 若剥到最后一截仍形似 docid，返回它
	if isDocIDLike(cur) {
		return cur
	}
	return ""
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
