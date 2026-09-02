#!/usr/bin/env python3
# 线上实例权限验证：指向 TEST_BASE（默认 https://gzt.example.com）
# 原则：覆盖完整权限矩阵，但所有测试数据结尾清理，不破坏默认账号（admin/wang/lin/chen）。
import json, os, sys, time
import urllib.request, urllib.error

BASE = os.environ.get("TEST_BASE", "https://gzt.example.com")

def req(method, path, token=None, payload=None):
    url = BASE + path
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        resp = urllib.request.urlopen(r, timeout=10)
        return resp.status, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8")
    except Exception as e:
        return -1, str(e)

def wait_ready():
    for _ in range(20):
        s, _ = req("GET", "/api/version")
        if s == 200:
            return True
        time.sleep(0.5)
    return False

def login(u, p):
    s, b = req("POST", "/api/auth/login", payload={"username": u, "password": p})
    tok = json.loads(b).get("token") if s == 200 else None
    return s, tok

results = []
cleanup = []  # (method, path, token) 结尾清理
def check(name, expect, actual):
    results.append((name, expect, actual, "PASS" if actual == expect else "FAIL"))
def must(b):  # 取首个 id
    return b

if not wait_ready():
    print("服务未就绪"); sys.exit(2)

# A. 认证基础
s, _ = req("GET", "/api/dashboard")
check("无 token 访问 /api/dashboard", 401, s)
s, _ = req("POST", "/api/auth/login", payload={"username": "admin", "password": "wrong"})
check("错误密码登录", 401, s)
for u, p in [("admin", "admin123"), ("wang", "123456"), ("lin", "123456"), ("chen", "123456")]:
    s, _ = login(u, p)
    check(f"默认账号登录 {u}", 200, s)

_, admin = login("admin", "admin123")
_, wang  = login("wang", "123456")
_, lin   = login("lin", "123456")
_, chen  = login("chen", "123456")

# B. 执行者(lin)写接口全 403（零写入）
for path, pl in [
    ("/api/tasks", {"title": "x", "type": "daily"}),
    ("/api/departments", {"name": "x"}),
    ("/api/settings", {"company_name": "x"}),
    ("/api/schedules", {"date": "2026-08-27", "shift": "早班", "people": ["甲"]}),
    ("/api/users", {"username": "u1", "password": "p", "name": "U", "role": "executor", "dept_id": 1}),
    ("/api/webhooks", {"name": "w", "url": "https://x.com/hook"}),
]:
    s, _ = req("POST", path, token=lin, payload=pl)
    check(f"执行者 lin POST {path}", 403, s)

# C. 部门管(wang)边界
s, _ = req("POST", "/api/departments", token=wang, payload={"name": "x"})
check("部门管 wang POST /api/departments (仅超管)", 403, s)
s, _ = req("POST", "/api/settings", token=wang, payload={"company_name": "x"})
check("部门管 wang POST /api/settings (仅超管)", 403, s)
s, _ = req("POST", "/api/settings", token=admin, payload={"company_name": "线上验证-无碍"})
check("超管 POST /api/settings", 200, s)

# D. 部门隔离 + 跨部拦截（用超管建临时部门/任务，结尾清理）
_, b = req("GET", "/api/departments", token=admin)
depts = json.loads(b)
test_dept_id = None
if depts:
    s, b = req("POST", "/api/departments", token=admin, payload={"name": "__perm_test_dept__"})
    if s == 200:
        test_dept_id = json.loads(b).get("id")
        cleanup.append(("DELETE", f"/api/departments/{test_dept_id}", admin))
s, b = req("POST", "/api/tasks", token=admin, payload={"title": "__perm_test_task__", "type": "daily", "dept_id": test_dept_id})
check("超管在临时部门建任务", 200, s)
cross_id = json.loads(b).get("id")
if cross_id:
    cleanup.append(("DELETE", f"/api/tasks/{cross_id}", admin))
s, _ = req("POST", f"/api/tasks/{cross_id}/toggle", token=lin)
check("执行者 lin toggle 非本部门任务 (跨部拦截)", 403, s)
s, _ = req("POST", f"/api/tasks/{cross_id}/toggle", token=wang)
check("部门管 wang toggle 非本部门任务 (跨部拦截)", 403, s)
s, _ = req("POST", f"/api/tasks/{cross_id}/toggle", token=admin)
check("超管 toggle 非本部门任务 (豁免)", 200, s)

# D2. 执行者只能完成本人负责的任务 + 删除接口部门隔离
s, b = req("POST", "/api/tasks", token=wang, payload={"title": "__not_lin__", "type": "daily", "assignee": "王主管"})
check("部门管建任务(Assignee=王主管)", 200, s)
not_lin_id = json.loads(b).get("id") if s == 200 else None
if not_lin_id:
    cleanup.append(("DELETE", f"/api/tasks/{not_lin_id}", admin))
s, _ = req("POST", f"/api/tasks/{not_lin_id}/toggle", token=lin)
check("执行者 lin toggle 本部门非本人任务 (负责人隔离)", 403, s)
s, b = req("POST", "/api/tasks", token=wang, payload={"title": "__lin_task__", "type": "daily", "assignee": "林晓"})
check("部门管建任务(Assignee=林晓)", 200, s)
lin_own_id = json.loads(b).get("id") if s == 200 else None
if lin_own_id:
    cleanup.append(("DELETE", f"/api/tasks/{lin_own_id}", admin))
s, _ = req("POST", f"/api/tasks/{lin_own_id}/toggle", token=lin)
check("执行者 lin toggle 本人负责任务", 200, s)
# 删除接口部门隔离
s, _ = req("DELETE", f"/api/tasks/{cross_id}", token=wang)
check("部门管 wang DELETE 非本部门任务 (跨部拦截)", 403, s)
s, b = req("POST", "/api/schedules", token=admin, payload={"date": "2026-08-27", "shift": "夜班", "people": ["乙"], "dept_id": test_dept_id})
check("超管建部门B班表", 200, s)
sched_cross_id = json.loads(b).get("id") if s == 200 else None
if sched_cross_id:
    cleanup.append(("DELETE", f"/api/schedules/{sched_cross_id}", admin))
s, _ = req("DELETE", f"/api/schedules/{sched_cross_id}", token=wang)
check("部门管 wang DELETE 非本部门班表 (跨部拦截)", 403, s)

# E. 跨部删用户（超管建临时用户，部门管删应 403，结尾超管清理）
s, b = req("POST", "/api/users", token=admin, payload={"username": "__perm_test_user__", "password": "p123456", "name": "测试", "role": "executor", "dept_id": test_dept_id})
check("超管建临时用户", 200, s)
tu_id = json.loads(b).get("id")
if tu_id:
    cleanup.append(("DELETE", f"/api/users/{tu_id}", admin))
s, _ = req("DELETE", f"/api/users/{tu_id}", token=wang)
check("部门管 wang 删其他部门用户 (跨部拦截)", 403, s)

# F. Webhook 脱敏（建临时 webhook，结尾删除）
s, _ = req("POST", "/api/webhooks", token=wang, payload={"name": "__perm_test_wh__", "url": "https://hooks.example.com/secret123"})
check("超管建临时 webhook", 200, s)
_, b = req("GET", "/api/webhooks", token=admin)
wh = next((w for w in json.loads(b) if w.get("name") == "__perm_test_wh__"), {})
wh_id = wh.get("id")
if wh_id:
    cleanup.append(("DELETE", f"/api/webhooks/{wh_id}", admin))
admin_url = wh.get("url", "")
check("超管可见 Webhook 明文", True, "secret123" in admin_url)
_, b = req("GET", "/api/webhooks", token=lin)
lin_url = next((w for w in json.loads(b) if w.get("name") == "__perm_test_wh__"), {}).get("url", "")
check("执行者所见 Webhook 已脱敏", True, ("••••" in lin_url) and ("secret123" not in lin_url))

# 清理
print("\n--- 清理临时数据 ---")
for method, path, tok in cleanup:
    st, _ = req(method, path, token=tok)
    print(f"  {method} {path} -> {st}")

# 输出
passed = sum(1 for r in results if r[3] == "PASS")
print("\n==== 线上实例权限验证结果 ====")
for name, exp, act, res in results:
    print(f"[{res}] {name}  (期望 {exp}, 实际 {act})")
print(f"\n断言项: {passed}/{len(results)} 通过")
sys.exit(0 if passed == len(results) else 1)
