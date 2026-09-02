#!/usr/bin/env python3
# 浏览器插件端鉴权验证：client_type=extension 签发 / CORS 跨域 / 跨域拉取任务
# 对应建议 3 中"三端统一鉴权"的 extension 端落地验证
import json, os, base64, urllib.request, urllib.error

BASE = os.environ.get("TEST_BASE", "http://127.0.0.1:8080")

def req(method, path, token=None, payload=None, origin=None, extra_headers=None):
    url = BASE + path
    data = json.dumps(payload).encode() if payload is not None else None
    h = {"Content-Type": "application/json"}
    if token: h["Authorization"] = "Bearer " + token
    if origin: h["Origin"] = origin
    if extra_headers: h.update(extra_headers)
    r = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        resp = urllib.request.urlopen(r, timeout=5)
        return resp.status, resp.read().decode(), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(), dict(e.headers)
    except Exception as e:
        return -1, str(e), {}

def jwt_payload(tok):
    seg = tok.split(".")[1]
    seg += "=" * (-len(seg) % 4)
    return json.loads(base64.b64decode(seg).decode())

results = []
def check(name, expect, actual):
    ok = actual == expect
    results.append((name, expect, actual, "PASS" if ok else "FAIL"))

# 1) CORS 预检：插件 origin 的 OPTIONS 应返回 CORS 头
s, _, hdrs = req("OPTIONS", "/api/tasks", origin="chrome-extension://abc123",
                 extra_headers={"Access-Control-Request-Method": "GET",
                                "Access-Control-Request-Headers": "authorization"})
check("OPTIONS 预检返回 204", 204, s)
check("CORS 头 Allow-Origin 存在", True, "access-control-allow-origin" in {k.lower() for k in hdrs})

# 2) extension 登录：token 应带 client=extension
s, b, _ = req("POST", "/api/auth/login", payload={"username": "admin", "password": "admin123", "client_type": "extension"})
tok = json.loads(b).get("token", "") if s == 200 else ""
check("extension 登录 200", 200, s)
if tok:
    pl = jwt_payload(tok)
    check("JWT payload client=extension", "extension", pl.get("client"))

# 3) 跨域带 token 拉任务（模拟插件 popup 行为）
s, b, hdrs2 = req("GET", "/api/tasks", token=tok, origin="chrome-extension://abc123")
check("跨域 GET 任务 200", 200, s)
check("跨域响应带 CORS 头", True, "access-control-allow-origin" in {k.lower() for k in hdrs2})
if s == 200:
    arr = json.loads(b)
    check("返回任务为数组", True, isinstance(arr, list))

passed = sum(1 for r in results if r[3] == "PASS")
print(f"\n=== 插件端鉴权测试：{passed}/{len(results)} 通过 ===")
for n, e, a, st in results:
    print(f"  {'✓' if st=='PASS' else '✗'} {n} (期望:{e} 实际:{a})" if st!='PASS' else f"  ✓ {n}")
sys_exit = 0 if passed == len(results) else 1
import sys
sys.exit(sys_exit)
