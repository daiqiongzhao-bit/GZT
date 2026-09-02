package handlers

// WebDAV 异地备份客户端（纯标准库实现：MKCOL / PUT / PROPFIND / DELETE）
// 目标格式：https://user:pass@dav.example.com/remote.php/dav/files/user/backup/（或 http://）

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// isWebDAVURL 判断异地目标是否为 WebDAV URL（http/https 前缀）
func isWebDAVURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// davAuth 解析后的 WebDAV 目标：base 为不带凭据的目录 URL
type davAuth struct {
	base string
	user string
	pass string
}

// parseWebDAV 解析 https://user:pass@host/path → base + user/pass
func parseWebDAV(raw string) (*davAuth, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("地址解析失败: %v", err)
	}
	a := &davAuth{}
	if u.User != nil {
		a.user = u.User.Username()
		a.pass, _ = u.User.Password()
		// 从原始串剔除 user:pass@，保留原始路径编码
		idx := strings.Index(raw, "@")
		schemeEnd := strings.Index(raw, "://")
		if idx > 0 && schemeEnd >= 0 && idx > schemeEnd {
			raw = raw[:schemeEnd+3] + raw[idx+1:]
		}
	}
	a.base = strings.TrimRight(raw, "/")
	if a.base == "" {
		return nil, fmt.Errorf("目标地址为空")
	}
	return a, nil
}

func davClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func davDo(a *davAuth, method, target string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return nil, err
	}
	if a.user != "" || a.pass != "" {
		req.SetBasicAuth(a.user, a.pass)
	}
	return davClient().Do(req)
}

// webdavEnsureDir 创建远程目录（已存在返回 405/301 视为成功）
func webdavEnsureDir(a *davAuth) error {
	resp, err := davDo(a, "MKCOL", a.base, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 301 || resp.StatusCode == 405 {
		return nil
	}
	return fmt.Errorf("创建目录失败: HTTP %d", resp.StatusCode)
}

// webdavTestConnect 保存配置前的连通性校验（建目录 + 探测）
func webdavTestConnect(rawURL string) error {
	a, err := parseWebDAV(rawURL)
	if err != nil {
		return err
	}
	return webdavEnsureDir(a)
}

// uploadToWebDAV 上传本地文件到 WebDAV 远程目录
func uploadToWebDAV(rawURL, localPath, name string) error {
	a, err := parseWebDAV(rawURL)
	if err != nil {
		return err
	}
	if err := webdavEnsureDir(a); err != nil {
		return err
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	target := a.base + "/" + name
	resp, err := davDo(a, "PUT", target, f)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("上传失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

// listWebDAV 列出远程目录下备份文件名（swb-backup-*.db）
func listWebDAV(rawURL string) ([]string, error) {
	a, err := parseWebDAV(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("PROPFIND", a.base, nil)
	if err != nil {
		return nil, err
	}
	if a.user != "" || a.pass != "" {
		req.SetBasicAuth(a.user, a.pass)
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	resp, err := davClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 207 {
		return nil, fmt.Errorf("列目录失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ms struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, fmt.Errorf("解析目录失败: %v", err)
	}
	var names []string
	for _, r := range ms.Responses {
		seg := r.Href
		if i := strings.LastIndex(seg, "/"); i >= 0 {
			seg = seg[i+1:]
		}
		if strings.HasPrefix(seg, backupPrefix) && strings.HasSuffix(seg, backupExt) {
			names = append(names, seg)
		}
	}
	return names, nil
}

// deleteWebDAV 删除远程文件
func deleteWebDAV(rawURL, name string) error {
	a, err := parseWebDAV(rawURL)
	if err != nil {
		return err
	}
	resp, err := davDo(a, "DELETE", a.base+"/"+name, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("删除失败: HTTP %d", resp.StatusCode)
}

// cleanupRemoteRetention 远端同样按保留份数清理，超出删最旧（文件名含时间戳，字典序即时间序）
func cleanupRemoteRetention(rawURL string, retention int) {
	if retention <= 0 {
		return
	}
	names, err := listWebDAV(rawURL)
	if err != nil {
		return
	}
	sort.Strings(names)
	if len(names) > retention {
		for _, old := range names[:len(names)-retention] {
			_ = deleteWebDAV(rawURL, old)
		}
	}
}
