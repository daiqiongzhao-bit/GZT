package wecompush

import "testing"

// v0.40.9：用户诉求「文档 ID 直接粘贴整条链接、后台自己提取」。
// 分享链接形态有好几种，这里把真实见到的几种都锁住，避免以后改提取逻辑时回归。
func TestExtractDocID(t *testing.T) {
	cases := []struct{ in, want string }{
		// 1) 直接填 docid：原样返回
		{"s3_AEoAjnizAK4CNvgxVYrptSzSCo1hh_a", "s3_AEoAjnizAK4CNvgxVYrptSzSCo1hh_a"},
		{"  d3_abcdef123456  ", "d3_abcdef123456"},
		// 2) 路径里带 docid 的分享链接
		{"https://doc.weixin.qq.com/smartsheet/s3_AEoAjnizAK4CNvgxVYrptSzSCo1hh_a?scode=ABC123", "s3_AEoAjnizAK4CNvgxVYrptSzSCo1hh_a"},
		{"https://doc.weixin.qq.com/smartsheet/s3_AEoAjnizAK4CNvgxVYrptSzSCo1hh_a/", "s3_AEoAjnizAK4CNvgxVYrptSzSCo1hh_a"},
		// 3) query 参数里带 docid
		{"https://doc.weixin.qq.com/smartsheet/overview?docid=d3_abcdef123456&tab=1", "d3_abcdef123456"},
		// 4) fragment / 其它形态：整串里能搜到就取
		{"https://doc.weixin.qq.com/sheet/zzz#/detail/s3_abcdef123456", "s3_abcdef123456"},
		// 5) 认不出来时原样返回（交给下游报错，不静默改写）
		{"https://doc.weixin.qq.com/smartsheet/", "https://doc.weixin.qq.com/smartsheet/"},
		{"", ""},
	}
	for _, c := range cases {
		if got := ExtractDocID(c.in); got != c.want {
			t.Errorf("ExtractDocID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderTpl(t *testing.T) {
	vars := map[string]string{
		"title": "【定时提醒】", "date": "2026-09-23", "count": "25",
		"filename": "满25天明细_2026-09-23.xlsx", "task": "高风险满25天推送",
	}
	// {filename} 与 {count} 同时出现，验证不会被子串键误替换
	got := RenderTpl("{title} {date} 共 **{count}** 条\n明细见上方附件《{filename}》。", vars)
	want := "【定时提醒】 2026-09-23 共 **25** 条\n明细见上方附件《满25天明细_2026-09-23.xlsx》。"
	if got != want {
		t.Errorf("RenderTpl = %q, want %q", got, want)
	}
	// 未知变量原样保留（便于用户发现写错变量名）
	if got := RenderTpl("{title} {nope}", vars); got != "【定时提醒】 {nope}" {
		t.Errorf("未知变量应原样保留，got %q", got)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"满25天明细_2026-09-23", "满25天明细_2026-09-23"},
		{"a/b\\c:d*e?f", "a_b_c_d_e_f"},
		{"  带空格  ", "带空格"},
		{"", "明细"},
	}
	for _, c := range cases {
		if got := SanitizeFileName(c.in); got != c.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 路径穿越：文件名模板里的 ../ 必须被消掉，且调用方还会再补 .xlsx
	if got := SanitizeFileName("../../etc/passwd"); got != ".._.._etc_passwd" {
		t.Errorf("路径穿越未被清洗: %q", got)
	}
}
