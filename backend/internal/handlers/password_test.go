package handlers

import "testing"

// 密码强度策略（v0.3.0）：至少 8 位，且必须同时包含字母与数字
func TestValidPasswordPolicy(t *testing.T) {
	// 合法：8 位且含字母+数字
	valid := []string{
		"Abc12345",
		"Cdf123456",
		"admin123",
		"abcd1234",
		"1234abcd",
		"Passw0rd!",
		"A1b2c3d4",
	}
	for _, p := range valid {
		if !validPassword(p) {
			t.Errorf("期望通过但被拒绝: %q", p)
		}
	}
	// 非法：纯数字 / 纯字母 / 不足 8 位 / 空
	invalid := []string{
		"123456",   // 纯数字 <8
		"12345678", // 纯数字 8 位，无字母
		"abcdefgh", // 纯字母 8 位，无数字
		"abc123",   // <8 位
		"Abc123",   // <8 位
		"",         // 空
		"123",      // 太短纯数字
		"admin",    // 太短
		"abcd efgh", // 纯字母(含空格) 无数字
	}
	for _, p := range invalid {
		if validPassword(p) {
			t.Errorf("期望被拒绝却通过: %q", p)
		}
	}
}
