package session

// 在线会话跟踪（内存态）：key 为 token 摘要，记录登录方式/IP/UA/最后活跃。
// 在线判定：最后活跃在 onlineWithin 内。进程重启会清空（属正常，登录态本就以 JWT 为准）。

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Info 单个在线会话
type Info struct {
	UserID   uint
	Username string
	Name     string
	DeptID   uint
	Client   string // web / pwa / extension
	IP       string
	UA       string
	LoginAt  time.Time
	LastSeen time.Time
}

var (
	mu sync.Mutex
	m  = map[string]*Info{}
)

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// Track 记录或刷新会话活跃时间
func Track(token string, userID uint, username, name string, deptID uint, client, ip, ua string) {
	if token == "" || userID == 0 {
		return
	}
	k := tokenKey(token)
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if s, ok := m[k]; ok {
		s.LastSeen = now
		return
	}
	m[k] = &Info{
		UserID: userID, Username: username, Name: name, DeptID: deptID,
		Client: client, IP: ip, UA: ua, LoginAt: now, LastSeen: now,
	}
}

// RemoveByUser 清除某用户全部会话（登出 / 强制下线 / 冻结）
func RemoveByUser(userID uint) {
	mu.Lock()
	defer mu.Unlock()
	for k, s := range m {
		if s.UserID == userID {
			delete(m, k)
		}
	}
}

func prune(maxAge time.Duration) {
	now := time.Now()
	for k, s := range m {
		if now.Sub(s.LastSeen) > maxAge {
			delete(m, k)
		}
	}
}

// List 返回在线会话（最后活跃在 onlineWithin 内），并顺带清理 24 小时以上的僵尸记录
func List(onlineWithin time.Duration) []Info {
	mu.Lock()
	defer mu.Unlock()
	prune(24 * time.Hour)
	now := time.Now()
	var out []Info
	for _, s := range m {
		if now.Sub(s.LastSeen) <= onlineWithin {
			out = append(out, *s)
		}
	}
	return out
}
