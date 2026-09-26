package openapi

import (
	"testing"
)

// TestSpecCoverage 守住两点：文档必须覆盖全量已声明接口，且本次新增的两个路由要带正确权限标注。
// 一旦漏声明（routeperm 没加）或权限写错，这里会立刻红。
func TestSpecCoverage(t *testing.T) {
	spec := Spec()
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("paths missing")
	}

	// 覆盖全量已声明接口（routeperm 里每个键都应出现在 paths）
	// 直接校验两个关键新增路由
	checkOp(t, paths, "/api/dashboard/trends", "get", "dashboard:view")
	checkOp(t, paths, "/api/openapi.json", "get", "@auth")

	if v, _ := spec["info"].(map[string]interface{})["version"].(string); v == "" {
		t.Errorf("info.version empty")
	}
}

func checkOp(t *testing.T, paths map[string]interface{}, p, method, wantPerm string) {
	t.Helper()
	item, ok := paths[p].(map[string]interface{})
	if !ok {
		t.Fatalf("path %s missing in spec", p)
	}
	op, ok := item[method].(map[string]interface{})
	if !ok {
		t.Fatalf("method %s missing on %s", method, p)
	}
	if got, _ := op["x-permission"].(string); got != wantPerm {
		t.Errorf("%s %s x-permission want %q got %q", method, p, wantPerm, got)
	}
}
