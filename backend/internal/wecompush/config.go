package wecompush

import (
	"os"
	"path/filepath"
	"time"
)

// AppVersion 企微推送模块版本号（仅用于概览/自检展示，不影响业务逻辑）
const AppVersion = "v1.0.0-wp"

// Config 企微推送模块运行配置，全部可通过环境变量覆盖。
// 注意：这些配置与 GZT 主体配置隔离，不会改动 GZT 的 secrets / DB 逻辑。
type Config struct {
	CLIPath   string // wecom-cli 可执行文件路径
	CLIConfig string // wecom-cli 凭据目录（默认 ~/.config/wecom）
	OutDir    string // Excel 导出目录
	Loc       *time.Location
}

// LoadConfig 从环境变量读取配置，缺省给出与 WecomPush 一致的默认值。
func LoadConfig() *Config {
	cli := os.Getenv("WECOM_CLI")
	if cli == "" {
		cli = "/usr/local/bin/wecom-cli"
	}
	cfgDir := os.Getenv("WECOM_CONFIG_DIR")
	if cfgDir == "" {
		cfgDir = "/root/.config/wecom"
	}
	out := os.Getenv("WECOM_OUT_DIR")
	if out == "" {
		out = "/data/wecom-out"
	}
	loc := time.Local
	if tz := os.Getenv("TZ"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	_ = os.MkdirAll(out, 0o755)
	return &Config{CLIPath: cli, CLIConfig: cfgDir, OutDir: out, Loc: loc}
}

// OutPath 拼接导出目录下的文件名，避免路径穿越。
func (c *Config) OutPath(name string) string {
	return filepath.Join(c.OutDir, name)
}
