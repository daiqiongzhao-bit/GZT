package wecompush

import (
	"encoding/json"
	"net/http"
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
