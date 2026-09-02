#!/usr/bin/env python3
# 账号权限回归测试：覆盖 认证/角色功能权限/部门数据隔离/跨部拦截/Webhook 脱敏
import json, os, sys, time
import urllib.request, urllib.error

BASE = os.environ.get("TEST_BASE", "http://127.0.0.1:18080")

def req(method, path, token=None, payload=None):
    url = BASE + path
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        resp = urllib.request.urlopen(r, timeout=5)
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

results = []
infos = []
def check(name, expect, actual, note=""):
    ok = (actual == expect)
    results.append((name, expect, actual, "PASS" if ok else "FAIL"))
def info(name, note):
    infos.append((name, note))

def login(u, p):
    s, b = req("POST", "/api/auth/login", payload={"username": u, "password": p})
    tok = json.loads(b).get("token") if s == 200 else None
    return s, tok

if not wait_ready():
    print("服务未就绪，退出"); sys.exit(2)

# ── A. 认证基础 ──
s, _ = req("GET", "/api/dashboard")
check("无 token 访问 /api/dashboard", 401, s)
s, _ = req("POST", "/api/auth/login", payload={"username": "admin", "password": "wrong"})
check("错误密码登录", 401, s)
for u, p in [("admin", "admin123"), ("wang", "123456"), ("lin", "123456"), ("chen", "123456")]:
    s, _ = login(u, p)
    check(f"登录 {u}", 200, s)

_, admin = login("admin", "admin123")
_, wang  = login("wang", "123456")
_, lin   = login("lin", "123456")
_, chen  = login("chen", "123456")

# ── B. 执行者(lin)写接口全部应 403 ──
write_cases = [
    ("/api/tasks", {"title": "x", "type": "daily"}),
    ("/api/departments", {"name": "x"}),
    ("/api/settings", {"company_name": "x"}),
    ("/api/schedules", {"date": "2026-08-27", "shift": "早班", "people": ["甲"]}),
    ("/api/users", {"username": "u1", "password": "p", "name": "U", "role": "executor", "dept_id": 1}),
    ("/api/webhooks", {"name": "w", "url": "https://x.com/hook"}),
]
for path, pl in write_cases:
    s, _ = req("POST", path, token=lin, payload=pl)
    check(f"执行者 lin POST {path}", 403, s)

# ── C. 部门管(wang)权限边界 ──
s, b = req("POST", "/api/tasks", token=wang, payload={"title": "wang任务", "type": "daily"})
check("部门管 wang POST /api/tasks", 200, s)
t2 = json.loads(b).get("id") if s == 200 else None
s, _ = req("POST", "/api/schedules", token=wang, payload={"date": "2026-08-27", "shift": "早班", "people": ["甲"]})
check("部门管 wang POST /api/schedules", 200, s)
s, _ = req("POST", "/api/departments", token=wang, payload={"name": "x"})
check("部门管 wang POST /api/departments (仅超管)", 403, s)
s, _ = req("POST", "/api/settings", token=wang, payload={"company_name": "x"})
check("部门管 wang POST /api/settings (仅超管)", 403, s)

# ── D. 超管全权 ──
s, _ = req("POST", "/api/departments", token=admin, payload={"name": "超管测试部"})
check("超管 POST /api/departments", 200, s)
s, _ = req("POST", "/api/settings", token=admin, payload={"company_name": "x"})
check("超管 POST /api/settings", 200, s)

# ── E. 执行者可读 ──
s, _ = req("GET", "/api/dashboard", token=lin)
check("执行者 lin GET /api/dashboard", 200, s)
s, _ = req("GET", "/api/tasks", token=lin)
check("执行者 lin GET /api/tasks", 200, s)

# ── F. 部门数据隔离 + 跨部拦截 ──
# chen 是执行者(部门B)，用超管在部门B建任务，再验证跨部拦截
_, b = req("GET", "/api/users", token=admin)
allusers = json.loads(b)
chen_user = next((u for u in allusers if u.get("username") == "chen"), None)
deptB = chen_user["dept_id"] if chen_user else None
s, b = req("POST", "/api/tasks", token=admin, payload={"title": "跨部门任务", "type": "daily", "dept_id": deptB})
check("超管在部门B建任务 (dept_id 可指定)", 200, s)
cross_id = json.loads(b).get("id")
s, _ = req("POST", f"/api/tasks/{cross_id}/toggle", token=lin)
check("执行者 lin(部门A) toggle 部门B任务 (跨部拦截)", 403, s)
s, _ = req("POST", f"/api/tasks/{cross_id}/toggle", token=wang)
check("部门管 wang(部门A) toggle 部门B任务 (跨部拦截)", 403, s)
s, _ = req("POST", f"/api/tasks/{cross_id}/toggle", token=admin)
check("超管 toggle 部门B任务 (豁免跨部)", 200, s)

_, b = req("GET", "/api/users", token=wang)
depts = sorted({u.get("dept_id") for u in json.loads(b)})
info("部门管 wang GET /api/users 可见部门", str(depts))

chen_id = chen_user["id"] if chen_user else None
s, _ = req("DELETE", f"/api/users/{chen_id}", token=wang)
check("部门管 wang DELETE 其他部门用户(chen)", 403, s)

s, _ = req("POST", f"/api/tasks/{t2}/toggle", token=wang)
check("部门管 wang toggle 本部门任务", 200, s)

# ── F2. 执行者只能完成本人负责的任务 ──
s, b = req("POST", "/api/tasks", token=wang, payload={"title": "__not_lin__", "type": "daily", "assignee": "王主管"})
check("部门管 wang 建任务(Assignee=王主管)", 200, s)
not_lin_id = json.loads(b).get("id") if s == 200 else None
s, _ = req("POST", f"/api/tasks/{not_lin_id}/toggle", token=lin)
check("执行者 lin toggle 本部门非本人任务 (负责人隔离)", 403, s)

s, b = req("POST", "/api/tasks", token=wang, payload={"title": "__lin_task__", "type": "daily", "assignee": "林晓"})
check("部门管 wang 建任务(Assignee=林晓)", 200, s)
lin_own_id = json.loads(b).get("id") if s == 200 else None
s, _ = req("POST", f"/api/tasks/{lin_own_id}/toggle", token=lin)
check("执行者 lin toggle 本人负责任务", 200, s)

# ── F3. 删除接口部门级隔离 ──
s, _ = req("DELETE", f"/api/tasks/{cross_id}", token=wang)
check("部门管 wang DELETE 部门B任务 (跨部拦截)", 403, s)
s, _ = req("DELETE", f"/api/tasks/{t2}", token=wang)
check("部门管 wang DELETE 本部门任务", 200, s)

s, b = req("POST", "/api/schedules", token=wang, payload={"date": "2026-08-27", "shift": "中班", "people": ["甲"]})
check("部门管 wang POST 班表(中班)", 200, s)
sched_local_id = json.loads(b).get("id") if s == 200 else None
s, b = req("POST", "/api/schedules", token=admin, payload={"date": "2026-08-27", "shift": "夜班", "people": ["乙"], "dept_id": deptB})
check("超管 POST 部门B班表", 200, s)
sched_cross_id = json.loads(b).get("id") if s == 200 else None
s, _ = req("DELETE", f"/api/schedules/{sched_cross_id}", token=wang)
check("部门管 wang DELETE 部门B班表 (跨部拦截)", 403, s)
s, _ = req("DELETE", f"/api/schedules/{sched_local_id}", token=wang)
check("部门管 wang DELETE 本部门班表", 200, s)

# ── G. Webhook 脱敏（仅超管可见明文） ──
s, _ = req("POST", "/api/webhooks", token=wang, payload={"name": "wh", "url": "https://hooks.example.com/secret123"})
check("部门管 wang POST /api/webhooks", 200, s)
_, b = req("GET", "/api/webhooks", token=admin)
wh = next((w for w in json.loads(b) if w.get("name") == "wh"), {})
admin_url = wh.get("url", "")
check("超管可见 Webhook 明文 (含 secret123)", True, "secret123" in admin_url)
_, b = req("GET", "/api/webhooks", token=lin)
whl = next((w for w in json.loads(b) if w.get("name") == "wh"), {})
lin_url = whl.get("url", "")
masked_ok = ("••••" in lin_url) and ("secret123" not in lin_url)
check("执行者所见 Webhook 已脱敏", True, masked_ok)

# ── 输出 ──
passed = sum(1 for r in results if r[3] == "PASS")
print("\n==== 账号权限回归测试结果 ====")
for name, exp, act, res in results:
    if res == "PASS":
        print(f"[PASS] {name}  (实际 {act})")
    else:
        print(f"[FAIL] {name}  (期望 {exp}, 实际 {act})")
for name, note in infos:
    print(f"[INFO] {name}: {note}")
print(f"\n断言项: {passed}/{len(results)} 通过")
sys.exit(0 if passed == len(results) else 1)
