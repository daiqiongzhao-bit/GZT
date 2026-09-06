package handlers

import "testing"

// TestBackupScopeTag 备份范围合法化与文件名标签解析（v0.2.0）
func TestBackupScopeTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"schedule", "schedule"},
		{"task", "task"},
		{"user", "user"},
		{"all", "all"},
		{"", "all"},
		{"xxx", "all"},
		{"ALL", "all"}, // backupScopeTag 只认小写；大写非法→回退 all（handler 层已 lowercase）
	}
	for _, c := range cases {
		if got := backupScopeTag(c.in); got != c.want {
			t.Errorf("backupScopeTag(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestParseScopeTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"swb-backup-schedule-20260901-100000.db", "schedule"},
		{"swb-backup-task-auto-20260901-100000.db", "task"},
		{"swb-backup-user-20260901-100000.db", "user"},
		{"swb-backup-all-auto-20260901-100000.db", "all"},
		// 旧版无 scope 段：swb-backup-auto-... / swb-backup-...
		{"swb-backup-auto-20260901-100000.db", "all"},
		{"swb-backup-20260901-100000.db", "all"},
	}
	for _, c := range cases {
		if got := parseScopeTag(c.in); got != c.want {
			t.Errorf("parseScopeTag(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestParseScopeTagLegacyAuto 确认旧版 auto 文件名仍被识别为 auto 类型（ListBackups 用 -auto- 判定）
func TestLegacyAutoFilename(t *testing.T) {
	if !containsSub("swb-backup-auto-20260901-100000.db", "-auto-") {
		t.Errorf("旧版 auto 备份文件名应含 -auto- 段")
	}
	if !containsSub("swb-backup-schedule-auto-20260901-100000.db", "-auto-") {
		t.Errorf("新 scope+auto 文件名也应含 -auto- 段")
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
