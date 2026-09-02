package handlers

import "testing"

// TestNormalizeMobile 手机号归一化：去空格/横线/括号，兼容 +86/0086 前缀
func TestNormalizeMobile(t *testing.T) {
	cases := []struct{ in, want string }{
		{"139 0000 0001", "13900000001"},
		{"139-0000-0001", "13900000001"},
		{"13900000001", "13900000001"},
		{" 139 0000 0001 ", "13900000001"},
		{"+86 139 0000 0001", "13900000001"},
		{"+86-13900000001", "13900000001"},
		{"0086 139 0000 0001", "13900000001"},
		{"(139) 0000-0001", "13900000001"},
		{"", ""},
		{"13800000015", "13800000015"},
	}
	for _, c := range cases {
		if got := normalizeMobile(c.in); got != c.want {
			t.Errorf("normalizeMobile(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
