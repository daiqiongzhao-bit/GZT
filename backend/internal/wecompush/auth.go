package wecompush

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// 重新授权（扫码 / 手动输入 Bot ID + Secret）
//
// 旧独立版 WecomPush 在「设置」页提供了「重新授权」卡片：
//   - 方式一：Bot ID + Secret（手动模式，需要真实终端）
//   - 方式二：扫码（--noninteractive，非交互环境推荐，服务端可直接跑）
// 这里把这套能力迁进合并包，作为 GZT 企微推送「设置」页的「重新授权」卡片。
//
// 单实例部署（GZT 只起一个 swb 进程），用包级单例保存进行中的授权进程。
// ---------------------------------------------------------------------------

var reScanURL = regexp.MustCompile(`https://\S*scode=[A-Za-z0-9_]+`)

// reANSI 匹配 ANSI 控制序列（CSI 与简单 ESC 序列），用于清洗 CLI 的伪终端输出
var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[@-Z\\-_]`)

// authManager 保存一次进行中的授权流程状态
type authManager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	mode      string // "qr" | "manual"
	scanURL   string
	startedAt time.Time
	backup    string // 备份凭据目录
}

var authMgr = &authManager{}

// cleanCLIOutput 把 wecom-cli 在伪终端下的输出整理成人类可读的错误摘要。
// CLI 的交互提示会输出大量 ANSI 控制序列（光标移动 / 清行 / 逐字符重绘），
// 直接回给前端会是一屏乱码。这里剥掉控制序列、去掉重复重绘帧，只保留末尾几行。
func cleanCLIOutput(s string, max int) string {
	s = reANSI.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var kept []string
	last := ""
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		ln = strings.Trim(ln, "|—*- ")
		if ln == "" || ln == last {
			continue // 跳过空行与重绘产生的连续重复
		}
		kept = append(kept, ln)
		last = ln
	}
	if len(kept) > 6 {
		kept = kept[len(kept)-6:]
	}
	return truncate(strings.Join(kept, " / "), max)
}

// urlCapturer 边读 CLI 输出边抽取扫码链接
type urlCapturer struct {
	mu  sync.Mutex
	sb  strings.Builder
	url string
}

func (u *urlCapturer) Write(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sb.Write(p)
	if m := reScanURL.FindString(u.sb.String()); m != "" {
		u.url = m
	}
	return len(p), nil
}

func (u *urlCapturer) URL() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.url
}

func (u *urlCapturer) String() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sb.String()
}

// backupCreds 把当前 ~/.config/wecom 下的凭据文件备份到一个带时间戳的目录
func (h *H) backupCreds() (string, error) {
	srcDir := h.Cfg.CLIConfig
	if _, err := os.Stat(srcDir); err != nil {
		// 目录尚不存在也能继续（首次授权）
		return "", nil
	}
	dst := filepath.Join(h.Cfg.OutDir, ".cred_backup", time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{"credentials.enc", ".encryption_key"} {
		s := filepath.Join(srcDir, name)
		if _, err := os.Stat(s); err != nil {
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dst, name), b, 0o600); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// restoreCreds 从备份目录还原凭据
func (h *H) restoreCreds(backup string) error {
	if backup == "" {
		return nil
	}
	srcDir := h.Cfg.CLIConfig
	for _, name := range []string{"credentials.enc", ".encryption_key"} {
		s := filepath.Join(backup, name)
		if _, err := os.Stat(s); err != nil {
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(srcDir, name), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// AuthStatusResp 授权状态响应
type AuthStatusResp struct {
	Authorized bool   `json:"authorized"`
	Status     string `json:"status"` // waiting | authorized | idle | error
	BotID      string `json:"bot_id"`
	ScanURL    string `json:"scan_url"`
	QrURL      string `json:"qr_url"`
	Mode       string `json:"mode"`
	Err        string `json:"err,omitempty"`
}

// StartWecomAuth 方式二：启动扫码授权（--noninteractive 非阻塞等待扫码）
// StartWecomAuth 方式二：启动扫码授权（--noninteractive 非阻塞等待扫码）
//
// 关键实现细节：wecom-cli 在「管道 / 文件」下会块缓冲，scode 扫码链接不会实时刷新，
// 导致服务端拿不到。用 `script` 包一层 PTY 强制行缓冲（与方式一手动授权一致），
// 并把输出写到一个日志文件，前端轮询期间从这里抽取扫码链接。同时把进程放进独立
// 进程组，取消时一次性杀掉 script 及其子进程 wecom-cli，避免孤儿进程。
func (h *H) StartWecomAuth(c *gin.Context) {
	authMgr.mu.Lock()
	if authMgr.cmd != nil && authMgr.cmd.Process != nil {
		authMgr.mu.Unlock()
		fail(c, 409, "已有授权流程进行中，请先完成扫码或点击「取消并恢复原授权」")
		return
	}
	backup, berr := h.backupCreds()
	if berr != nil {
		authMgr.mu.Unlock()
		fail(c, 500, "备份现有凭据失败："+berr.Error())
		return
	}
	qrPath := h.Cfg.OutPath("qr.png")
	logPath := h.Cfg.OutPath("auth_init.log")
	_ = os.Remove(qrPath)
	_ = os.Remove(logPath)

	inner := h.Cfg.CLIPath + " auth init --noninteractive --no-browser --output-qrcode " + qrPath
	var cmd *exec.Cmd
	if _, lerr := exec.LookPath("script"); lerr == nil {
		cmd = exec.Command("script", "-qec", inner, "/dev/null")
	} else {
		// 退化：无 script 时直接跑（可能拿不到实时扫码链接，但二维码文件仍可用）
		cmd = exec.Command(h.Cfg.CLIPath, "auth", "init", "--noninteractive", "--no-browser", "--output-qrcode", qrPath)
	}
	cmd.Env = h.cliEnv()
	var logF *os.File
	if f, ferr := os.Create(logPath); ferr == nil {
		logF = f
		cmd.Stdout = logF
		cmd.Stderr = logF
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			logF.Close()
		}
		authMgr.mu.Unlock()
		fail(c, 500, "启动授权失败："+err.Error())
		return
	}
	if logF != nil {
		defer logF.Close()
	}
	authMgr.cmd = cmd
	authMgr.mode = "qr"
	authMgr.backup = backup
	authMgr.startedAt = time.Now()
	authMgr.scanURL = ""
	authMgr.mu.Unlock() // 释放锁，避免阻塞 /auth/status 与 /auth/cancel 轮询

	go func() {
		_ = cmd.Wait()
		// 进程退出（扫码成功 / 超时 / 失败）后清理二维码图片
		_ = os.Remove(qrPath)
		authMgr.mu.Lock()
		authMgr.cmd = nil
		authMgr.mu.Unlock()
	}()

	// 轮询日志文件抽取 scode 链接（PTY 行缓冲，实时可读取）
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, rerr := os.ReadFile(logPath); rerr == nil {
			if m := reScanURL.FindString(string(b)); m != "" {
				authMgr.mu.Lock()
				authMgr.scanURL = m
				authMgr.mu.Unlock()
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	authMgr.mu.Lock()
	scanURL := authMgr.scanURL
	authMgr.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"status":   "waiting",
		"scan_url": scanURL,
		"qr_url":   "/api/wecom-push/auth/qr",
		"mode":     "qr",
	})
}

// AuthQR 直接把二维码 PNG 流式返回给前端 <img>
func (h *H) AuthQR(c *gin.Context) {
	path := h.Cfg.OutPath("qr.png")
	if _, err := os.Stat(path); err != nil {
		fail(c, 404, "二维码尚未生成，请先点击「生成二维码」")
		return
	}
	c.File(path)
}

// WecomAuthStatus 轮询授权是否完成
func (h *H) WecomAuthStatus(c *gin.Context) {
	st := h.CLIStatus()
	authMgr.mu.Lock()
	running := authMgr.cmd != nil && authMgr.cmd.Process != nil
	mode := authMgr.mode
	scanURL := authMgr.scanURL
	authMgr.mu.Unlock()

	// 优先看是否还有进行中的授权进程：重新授权时机器人本就处于「已授权」，
	// 必须按「进程是否还在」来判定是否处于等待扫码，否则会误报已完成。
	if running {
		ok(c, AuthStatusResp{Authorized: false, Status: "waiting", Mode: mode, ScanURL: scanURL, QrURL: "/api/wecom-push/auth/qr"})
		return
	}
	if st.Authorized {
		ok(c, AuthStatusResp{Authorized: true, Status: "authorized", BotID: st.BotID})
		return
	}
	ok(c, AuthStatusResp{Authorized: false, Status: "idle", Mode: mode, ScanURL: scanURL})
}

// CancelWecomAuth 取消进行中的授权，并把凭据还原到备份
func (h *H) CancelWecomAuth(c *gin.Context) {
	authMgr.mu.Lock()
	defer authMgr.mu.Unlock()
	if authMgr.cmd != nil && authMgr.cmd.Process != nil {
		// 杀掉整个进程组（script + 子进程 wecom-cli），避免取消后留下孤儿进程
		_ = syscall.Kill(-authMgr.cmd.Process.Pid, syscall.SIGKILL)
		_ = authMgr.cmd.Process.Kill() // 兜底
	}
	authMgr.cmd = nil
	_ = os.Remove(h.Cfg.OutPath("qr.png"))
	_ = os.Remove(h.Cfg.OutPath("auth_init.log"))
	if authMgr.backup != "" {
		if err := h.restoreCreds(authMgr.backup); err != nil {
			fail(c, 500, "恢复原授权失败："+err.Error())
			return
		}
	}
	authMgr.backup = ""
	authMgr.scanURL = ""
	st := h.CLIStatus()
	ok(c, gin.H{"status": "idle", "authorized": st.Authorized, "bot_id": st.BotID})
}

// WecomAuthManual 方式一：手动输入 Bot ID + Secret（需要伪终端，用 script 包装）
func (h *H) WecomAuthManual(c *gin.Context) {
	var req struct {
		BotID  string `json:"bot_id"`
		Secret string `json:"secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.BotID) == "" || strings.TrimSpace(req.Secret) == "" {
		fail(c, 400, "请提供 bot_id 与 secret")
		return
	}

	authMgr.mu.Lock()
	if authMgr.cmd != nil && authMgr.cmd.Process != nil {
		authMgr.mu.Unlock()
		fail(c, 409, "已有授权流程进行中")
		return
	}
	backup, berr := h.backupCreds()
	if berr != nil {
		authMgr.mu.Unlock()
		fail(c, 500, "备份失败："+berr.Error())
		return
	}
	authMgr.backup = backup
	authMgr.mu.Unlock()

	out, err := h.runManualAuth(strings.TrimSpace(req.BotID), strings.TrimSpace(req.Secret))
	if err != nil {
		// 失败时还原凭据，避免污染现有授权
		_ = h.restoreCreds(backup)
		log.Printf("[wecom-auth] 手动授权失败: %v | %s", err, cleanCLIOutput(out, 400))
		fail(c, 400, "授权未成功，请检查 Bot ID / Secret 是否正确（原授权已保留）")
		return
	}
	st := h.CLIStatus()
	if !st.Authorized {
		_ = h.restoreCreds(backup)
		log.Printf("[wecom-auth] 手动授权未生效: %s", cleanCLIOutput(out, 400))
		fail(c, 400, "授权未成功，请检查 Bot ID / Secret 是否正确（原授权已保留）")
		return
	}
	ok(c, gin.H{"status": "authorized", "authorized": true, "bot_id": st.BotID})
}

// runManualAuth 在伪终端里执行 `wecom-cli auth init --manual`，避免 "需要终端" 报错。
// 优先用镜像内的 script（util-linux）包装；若不可用则退化为直接执行（CLI 会明确报错）。
func (h *H) runManualAuth(botID, secret string) (string, error) {
	input := botID + "\n" + secret + "\n"
	if _, lerr := exec.LookPath("script"); lerr == nil {
		cmd := exec.Command("script", "-qec", h.Cfg.CLIPath+" auth init --manual", "/dev/null")
		cmd.Env = h.cliEnv()
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdin, _ := cmd.StdinPipe()
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		// 必须先 Start 再 Wait（此前漏了 Start，Wait 直接返回 "exec: not started"，
		// 导致方式一手动授权永远失败）
		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			return buf.String(), err
		}
		go func() { defer stdin.Close(); _, _ = stdin.Write([]byte(input)) }()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case e := <-done:
			return buf.String(), e
		case <-time.After(90 * time.Second):
			// 杀掉整个进程组，避免超时后留下孤儿 wecom-cli
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				_ = cmd.Process.Kill()
			}
			return buf.String(), fmt.Errorf("授权超时（90s）")
		}
	}
	// 退化路径：直接运行（通常 CLI 会报「需要终端」）
	cmd := exec.Command(h.Cfg.CLIPath, "auth", "init", "--manual")
	cmd.Env = h.cliEnv()
	stdin, _ := cmd.StdinPipe()
	go func() { defer stdin.Close(); _, _ = stdin.Write([]byte(input)) }()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
