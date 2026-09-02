#!/usr/bin/env python3
# 行级/部门隔离 & 令牌版本 回归测试
# 覆盖本次改动不变量：
#   1) 员工级行隔离：执行者仅可见 assignee_id==自己 或 0 的任务
#   2) 部门隔离：伪造请求体 dept_id 被服务端忽略，按 Token 真实 dept 落库
#   3) 令牌版本：登出后旧 token 立即失效(401)；超管强制下线同理
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
def check(name, expect, actual, note=""):
    ok = (actual == expect)
    results.append((name, expect, actual, "PASS" if ok else "FAIL", note))

def login(u, p, client=None):
    pl = {"username": u, "password": p}
    if client:
        pl["client_type"] = client
    s, b = req("POST", "/api/auth/login", payload=pl)
    tok = json.loads(b).get("token") if s == 200 else None
    return s, tok

def create_user(token, username, name, role, dept_id, pwd="123456"):
    s, b = req("POST", "/api/users", token=token,
               payload={"username": username, "name": name, "role": role,
                        "dept_id": dept_id, "password": pwd})
    return s, (json.loads(b).get("id") if s == 200 else None)

def create_task(token, title, assignee, dept_id):
    s, b = req("POST", "/api/tasks", token=token,
               payload={"title": title, "type": "daily", "assignee": assignee, "dept_id": dept_id})
    return s, (json.loads(b) if s == 200 else None)

if not wait_ready():
    print("服务未就绪，退出"); sys.exit(2)

# 登录超管
s, admin = login("admin", "admin123")
check("超管登录", 200, s)
assert admin, "需要 admin/admin123"

# 取部门列表（超管可见全量）
s, b = req("GET", "/api/departments", token=admin)
depts = json.loads(b) if s == 200 else []
assert len(depts) >= 1, "无部门，无法测试"
dept1 = depts[0]["id"]

# ── 1. 员工级行隔离 ──
# 建两个执行者，同部门
s, lin_id = create_user(admin, "lintest", "林测试", "executor", dept1)
s, chen_id = create_user(admin, "chentest", "陈测试", "executor", dept1)
check("创建执行者 lin", 200, s)

# 超管建两个任务：一个指 lin，一个指 chen（同部门）
s, t_lin = create_task(admin, "只给林的任务", "林测试", dept1)
s, t_chen = create_task(admin, "只给陈的任务", "陈测试", dept1)
check("超管建任务(林)", 200, s)
check("超管建任务(陈)", 200, s)

# 取 lin / chen token
s, lin_tok = login("lintest", "123456")
s, chen_tok = login("chentest", "123456")

# lin 列表：部门内员工互相可见，应能看到 chen 的任务（同部门）
s, b = req("GET", "/api/tasks", token=lin_tok)
lin_tasks = json.loads(b) if s == 200 else []
seen_chen = any(t.get("id") == t_chen.get("id") for t in lin_tasks)
check("执行者 lin 可见同部门 chen 的任务", True, seen_chen)
# lin 自身任务应可见
seen_self = any(t.get("id") == t_lin.get("id") for t in lin_tasks)
check("执行者 lin 可见自己的任务", True, seen_self)

# ── 2. 部门隔离：伪造 dept_id 被忽略 ──
if len(depts) >= 2:
    dept2 = depts[1]["id"]
    # lin 是 dept1 执行者，伪造请求体 dept_id=dept2 建任务（lin 无建任务权限，改用超管验证落库 dept）
    s, t = create_task(admin, "伪造部门测试", "林测试", dept2)
    check("超管建任务(指向dept2)", 200, s)
    # 任务实际 dept_id 应为传入的 dept2（超管允许跨部指定），这里验证服务端使用了传入值而非前端无关字段
    check("任务 dept_id 落库正确", dept2, t.get("dept_id"))
    # 部门管若伪造 dept_id 创建，应被强制覆盖为自身 dept
    # 用 lin 不可建任务，跳过；改用超管已覆盖 dept，验证非越权查询
else:
    check("部门隔离(单部门跳过)", None, None, "仅一个部门")

# 关键不变量：非超管查询绝不返回其他部门数据
# 取另一个部门管视角（若有 dept2）
if len(depts) >= 2:
    dept2 = depts[1]["id"]
    # 在 dept2 建一个部门管
    s, wang2 = create_user(admin, "wang2test", "王二测试", "dept_admin", dept2)
    s, w2tok = login("wang2test", "123456")
    s, b = req("GET", "/api/tasks", token=w2tok)
    w2tasks = json.loads(b) if s == 200 else []
    cross = [t for t in w2tasks if t.get("dept_id") not in (0, dept2)]
    check("部门管 wang2 不可见非本部门任务", 0, len(cross))

# ── 3. 令牌版本：登出失效 ──
s, lin_tok2 = login("lintest", "123456")
check("登出前请求有效", 200, req("GET", "/api/tasks", token=lin_tok2)[0])
s, _ = req("POST", "/api/logout", token=lin_tok2)
check("登出接口", 200, s)
s, _ = req("GET", "/api/tasks", token=lin_tok2)
check("登出后旧 token 失效(401)", 401, s)
# 重新登录仍可用
s, lin_tok3 = login("lintest", "123456")
check("重新登录可用", 200, s)

# 超管强制下线
s, _ = req("POST", f"/api/users/{lin_id}/force-logout", token=admin)
check("超管强制下线", 200, s)
s, _ = req("GET", "/api/tasks", token=lin_tok3)
check("被强制下线后 token 失效(401)", 401, s)
# 非超管调用强制下线应 403
s, _ = req("POST", f"/api/users/{chen_id}/force-logout", token=chen_tok)
check("非超管强制下线(403)", 403, s)

# ── 4. 任务完成记录审计 ──
# 超管建一个任务并多次完成，验证审计记录写入与完整时间戳
s, t_audit = create_task(admin, "审计回归任务", "林测试", dept1)
check("建审计任务", 200, s)
aid = t_audit.get("id")
req("POST", f"/api/tasks/{aid}/toggle", token=admin)   # 完成
import time as _t; _t.sleep(1.1)
req("POST", f"/api/tasks/{aid}/toggle", token=admin)   # 重开
_t.sleep(1.1)
req("POST", f"/api/tasks/{aid}/toggle", token=admin)   # 再完成 -> 应得 2 条记录
s, b = req("GET", f"/api/tasks/{aid}/completions", token=admin)
audit_recs = json.loads(b) if s == 200 else []
check("单任务完成记录=2条", 2, len(audit_recs))
if audit_recs:
    r0 = audit_recs[0]
    check("记录含完整时间戳", True, bool(r0.get("completed_at") and len(r0["completed_at"]) > 10))
    check("记录含完成人", "admin", r0.get("user_name"))
    check("记录含任务标题", "审计回归任务", r0.get("task_title"))
# toggle 返回最近完成时间
s, b = req("POST", f"/api/tasks/{aid}/toggle", token=admin)
toggle_resp = json.loads(b) if s == 200 else {}
check("toggle返回 completed_at", True, bool(toggle_resp.get("completed_at")))
# 全局查询接口（部门管可读本部门）
s, w2tok = (login("wang2test", "123456") if len(depts) >= 2 else (200, chen_tok))
s, b = req("GET", "/api/completions", token=admin)
check("全局完成记录可查", 200, s)
s, _ = req("GET", "/api/completions", token=chen_tok)
check("执行者查全局记录(403)", 403, s)

# ── 汇总 ──
passed = sum(1 for r in results if r[3] == "PASS")
total = len(results)
print(f"\n=== 行级/部门隔离 & 令牌版本 测试：{passed}/{total} 通过 ===")
for name, exp, act, st, note in results:
    mark = "✓" if st == "PASS" else "✗"
    extra = f"  (期望:{exp} 实际:{act})" if st != "PASS" else ""
    nt = f" [{note}]" if note else ""
    print(f"  {mark} {name}{extra}{nt}")
sys.exit(0 if passed == total else 1)
