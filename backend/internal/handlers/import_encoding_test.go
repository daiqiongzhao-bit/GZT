package handlers

import (
	"os"
	"testing"
)

func TestSniffFileKind(t *testing.T) {
	// 真 xlsx（ZIP 魔数）
	xlsx, err := os.ReadFile("/tmp/test_tasks.xlsx")
	if err != nil {
		t.Skip("缺少测试文件")
	}
	cases := []struct {
		name     string
		buf      []byte
		file     string
		wantKind string
	}{
		{"xlsx扩展名", xlsx, "任务.xlsx", "xlsx"},
		{"xlsx改成csv扩展名", xlsx, "任务.csv", "xlsx"},   // 关键：magic 优先于扩展名
		{"xlsx改成txt扩展名", xlsx, "任务.txt", "xlsx"},
		{"老xls魔数", []byte{0xD0, 0xCF, 0x11, 0xE0, 1, 2}, "旧表.xls", "xls"},
		{"老xls伪装csv", []byte{0xD0, 0xCF, 0x11, 0xE0, 1, 2}, "旧表.csv", "xls"},
		{"普通csv", []byte("标题,班次\n早报,全员\n"), "a.csv", "csv"},
		{"无扩展名文本", []byte("a,b\n"), "data", "csv"},
	}
	for _, c := range cases {
		if got := sniffFileKind(c.buf, c.file); got != c.wantKind {
			t.Errorf("%s: 期望 %s，实际 %s", c.name, c.wantKind, got)
		}
	}
}

func TestDecodeTextBytes(t *testing.T) {
	// GBK 编码的 "早报,全员"
	gbk := []byte{0xD4, 0xE7, 0xB1, 0xA8, ',', 0xC8, 0xAB, 0xD4, 0xB1}
	got, err := decodeTextBytes(gbk)
	if err != nil {
		t.Fatalf("GBK 解码失败: %v", err)
	}
	if got != "早报,全员" {
		t.Errorf("GBK 解码错误，得到 %q", got)
	}

	// UTF-8 无 BOM
	utf8 := []byte("提交周报,全员")
	got, _ = decodeTextBytes(utf8)
	if got != "提交周报,全员" {
		t.Errorf("UTF-8 解码错误，得到 %q", got)
	}

	// UTF-8 带 BOM：应剥掉 BOM
	bom := append([]byte{0xEF, 0xBB, 0xBF}, []byte("盘点库存,全员")...)
	got, _ = decodeTextBytes(bom)
	if got != "盘点库存,全员" {
		t.Errorf("BOM 处理错误，得到 %q", got)
	}
}
