package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Port       string
	JWTSecret  []byte
	AESKey     []byte
	DBPath     string
	AppVersion string
	BackupDir  string
}

var C *Config

// 弱/默认密钥黑名单：一旦命中说明注入的是公开可猜值，生产下必须拒绝使用，
// 否则攻击者可用公开默认值伪造任意身份 JWT / 解密历史密文。
var weakSecrets = []string{
	"shift-workbench-secret-change-me",
	"shift-workbench-aes-key-2024",
	"change-me", "changeme", "secret", "default", "password",
}

// secretsFileName 密钥持久化文件名，位于数据库所在目录（= 数据卷，跨容器重建保留）。
// 首次启动（或运维显式注入）时自动生成/记录强随机密钥，之后每次启动复用，
// 保证身份签名（JWT）与历史密文（AES）稳定，也方便无 SSH / 仅 UI 的部署（群晖/Portainer/NAS）。
const secretsFileName = "secrets.env"

func Init() error {
	// 强制使用中国时区（北京时间），避免容器默认 UTC 导致任务逾期判断与到点推送错 8 小时
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		time.Local = loc
	} else {
		time.Local = time.FixedZone("CST", 8*3600)
	}

	dbPath := getEnv("DB_PATH", "shift_workbench.db")
	// 显式放行阀：仅限本地开发/单测（go run 无 env 无数据卷），生产不要设置
	allowDefault := strings.EqualFold(os.Getenv("GZT_ALLOW_DEFAULT_SECRET"), "1")

	var jwtV, aesV string
	if allowDefault {
		// 本地开发兜底：仍用内置默认值，便于无任何 env/卷 时直接跑起来
		jwtV = getEnv("JWT_SECRET", "shift-workbench-secret-change-me")
		aesV = getEnv("AES_KEY", "shift-workbench-aes-key-2024")
	} else {
		var err error
		jwtV, aesV, err = resolveSecrets(dbPath)
		if err != nil {
			return err
		}
	}

	C = &Config{
		Port:       getEnv("APP_PORT", "8080"),
		JWTSecret:  []byte(jwtV),
		AESKey:     []byte(padKey(aesV)),
		DBPath:     dbPath,
		AppVersion: "v0.19.4",
		BackupDir:  getEnv("BACKUP_DIR", "backups"),
	}
	return nil
}

// resolveSecrets 解析最终使用的 JWT_SECRET / AES_KEY，返回 (jwt, aes)。
//
// 优先级与安全规则：
//  1. 若显式注入（env 里两者非空）：
//     - 必须两者都提供、且均为强随机值（弱默认值拒绝，防公开默认密钥被伪造/解密）；
//     - 与数据卷已持久化密钥不一致时拒绝（防止把已装好的实例悄悄换 key 导致全员登出 + 密文解不开）；
//     - 首次显式注入则把该强密钥写入数据卷，保证以后无 env 也能稳定复用。
//  2. 若未注入任何密钥（无 SSH / 仅 UI 的首启或空卷）：
//     - 数据卷已有 secrets.env → 直接复用（身份与密文稳定）；
//     - 数据卷也没有 → 首次启动：自动生成 32 字节强随机密钥并写入数据卷，正常启动。
func resolveSecrets(dbPath string) (string, string, error) {
	envJWT := os.Getenv("JWT_SECRET")
	envAES := os.Getenv("AES_KEY")
	provided := envJWT != "" || envAES != ""

	if provided {
		if envJWT == "" || envAES == "" {
			return "", "", fmt.Errorf("JWT_SECRET 与 AES_KEY 必须同时注入；如不愿手动提供，请两者都留空，交由系统在数据卷上首次启动自动生成")
		}
		for _, kv := range []struct{ n, v string }{{"JWT_SECRET", envJWT}, {"AES_KEY", envAES}} {
			if v, err := checkSecretStrength(kv.n, kv.v); err != nil {
				return "", "", err
			} else {
				_ = v
			}
		}
		// 读取数据卷已有密钥，做一致性保护
		persisted := loadSecretsFile(secretsPath(dbPath))
		if persisted != nil {
			if envJWT != persisted.jwt || envAES != persisted.aes {
				return "", "", fmt.Errorf("检测到显式注入的密钥与数据卷 %s 已持久化的密钥不一致。这是既有部署，为避免全员登出 + 历史密文无法解密，禁止更换密钥；如需重置请删除数据卷中的 %s 后重启（将自动生成新密钥，旧密文/会话作废）", secretsPath(dbPath), secretsFileName)
			}
			return envJWT, envAES, nil
		}
		if err := persistSecrets(secretsPath(dbPath), envJWT, envAES); err != nil {
			return "", "", fmt.Errorf("已显式注入强密钥，但写入数据卷 %s 失败: %v", secretsPath(dbPath), err)
		}
		return envJWT, envAES, nil
	}

	// 未注入：复用数据卷密钥，或首次启动自动生成
	if persisted := loadSecretsFile(secretsPath(dbPath)); persisted != nil {
		return persisted.jwt, persisted.aes, nil
	}
	jwtV, aesV, err := genStrongPair()
	if err != nil {
		return "", "", fmt.Errorf("生成安全密钥失败: %v", err)
	}
	if err := persistSecrets(secretsPath(dbPath), jwtV, aesV); err != nil {
		return "", "", fmt.Errorf("首次启动自动生成密钥，但写入数据卷 %s 失败: %v。请确认数据目录可写（Docker 卷 /data 已挂载且权限正常）", secretsPath(dbPath), err)
	}
	return jwtV, aesV, nil
}

// checkSecretStrength 校验单个注入密钥：非弱默认值且长度达标。
func checkSecretStrength(name, val string) (bool, error) {
	for _, w := range weakSecrets {
		if val == w {
			return false, fmt.Errorf("%s 使用了公开的弱默认值，极易被伪造/解密。请改为至少 32 字节的强随机密钥，或将该变量留空交由系统自动生成；本地开发可设 GZT_ALLOW_DEFAULT_SECRET=1", name)
		}
	}
	if len(val) < 16 {
		return false, fmt.Errorf("%s 长度过短（%d 字符），建议至少 32 字符强随机值，或留空交由系统自动生成", name, len(val))
	}
	return true, nil
}

// genStrongPair 生成一对 32 字节（64 hex 字符）随机密钥，用于 JWT 签名与 AES-256。
func genStrongPair() (string, string, error) {
	g := func() (string, error) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b), nil
	}
	j, err := g()
	if err != nil {
		return "", "", err
	}
	a, err := g()
	if err != nil {
		return "", "", err
	}
	return j, a, nil
}

type persistedSecrets struct {
	jwt string
	aes string
}

// secretsPath 返回密钥持久化文件路径：与数据库同目录（即挂载的数据卷）。
func secretsPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), secretsFileName)
}

// loadSecretsFile 读取数据卷中的密钥文件；缺失/损坏返回 nil（视为无持久化密钥）。
func loadSecretsFile(path string) *persistedSecrets {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var jwtV, aesV string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "JWT_SECRET":
			jwtV = v
		case "AES_KEY":
			aesV = v
		}
	}
	if jwtV == "" || aesV == "" {
		return nil
	}
	return &persistedSecrets{jwt: jwtV, aes: aesV}
}

// persistSecrets 将密钥写入数据卷（0600 权限），目录不存在则创建。
func persistSecrets(path, jwtV, aesV string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := "# GZT 自动生成/持久化的安全密钥。请勿提交到版本库；删除后重启将生成新密钥（旧会话/密文作废）。\n" +
		"JWT_SECRET=" + jwtV + "\n" +
		"AES_KEY=" + aesV + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return nil
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// padKey 将 AES key 补齐/截断到 32 字节（AES-256）
func padKey(s string) string {
	b := []byte(s)
	if len(b) >= 32 {
		return string(b[:32])
	}
	for len(b) < 32 {
		b = append(b, 0)
	}
	return string(b)
}
