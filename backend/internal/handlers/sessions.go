package handlers

import (
	"time"

	"shiftworkbench/internal/session"

	"github.com/gin-gonic/gin"
)

// onlineWindow 在线判定窗口：15 分钟内有活跃请求即视为在线
const onlineWindow = 15 * time.Minute

// GetSessions GET /api/sessions 在线用户列表（仅超管）
// 按用户聚合多设备会话：登录方式 / 来源 IP / 在线时长 / 最后活跃
func GetSessions(c *gin.Context) {
	infos := session.List(onlineWindow)
	type aggInfo struct {
		UserID    uint     `json:"user_id"`
		Username  string   `json:"username"`
		Name      string   `json:"name"`
		DeptID    uint     `json:"dept_id"`
		Clients   []string `json:"clients"`   // 登录方式：web/pwa/extension
		IPs       []string `json:"ips"`       // 来源 IP
		Count     int      `json:"count"`     // 在线会话数（多设备）
		LoginAt   string   `json:"login_at"`  // 本次在线最早登录时间
		LastSeen  string   `json:"last_seen"` // 最后活跃时间
		OnlineSec int64    `json:"online_sec"`
	}
	now := time.Now()
	agg := map[uint]*aggInfo{}
	var order []uint
	for _, s := range infos {
		a, ok := agg[s.UserID]
		if !ok {
			a = &aggInfo{
				UserID: s.UserID, Username: s.Username, Name: s.Name, DeptID: s.DeptID,
				LoginAt: s.LoginAt.Format("2006-01-02 15:04:05"), LastSeen: s.LastSeen.Format("2006-01-02 15:04:05"),
			}
			agg[s.UserID] = a
			order = append(order, s.UserID)
		}
		a.Count++
		addUniqueStr(&a.Clients, s.Client)
		addUniqueStr(&a.IPs, s.IP)
		if s.LastSeen.After(mustParse(a.LastSeen)) {
			a.LastSeen = s.LastSeen.Format("2006-01-02 15:04:05")
		}
		if s.LoginAt.Before(mustParse(a.LoginAt)) {
			a.LoginAt = s.LoginAt.Format("2006-01-02 15:04:05")
		}
		a.OnlineSec = int64(now.Sub(s.LoginAt).Seconds())
	}
	list := make([]*aggInfo, 0, len(order))
	for _, id := range order {
		list = append(list, agg[id])
	}
	c.JSON(200, list)
}

func addUniqueStr(list *[]string, v string) {
	if v == "" {
		v = "web"
	}
	for _, x := range *list {
		if x == v {
			return
		}
	}
	*list = append(*list, v)
}

func mustParse(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	return t
}
