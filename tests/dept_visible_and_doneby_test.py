#!/usr/bin/env python3
# 验证：部门内员工互相可见 + 完成任务记录完成人
import json, os, sys, time
import urllib.request, urllib.error

BASE = os.environ.get("TEST_BASE", "http://127.0.0.1:8081")

def req(method, path, token=None, payload=None):
    url = BASE + path
    data = json.dumps(payload).encode() if payload is not None else None
    h = {"Content-Type": "application/json"}
    if token: h["Authorization"] = "Bearer " + token
    r = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        resp = urllib.request.urlopen(r, timeout=5)
        return resp.status, resp.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()
    except Exception as e:
        return -1, str(e)

def login(u, p):
    s, b = req("POST", "/api/auth/login", payload={"username": u, "password": p})
    return s, (json.loads(b).get("token") if s == 200 else None)

results = []
def check(name, expect, actual):
    ok = actual == expect
    results.append((name, expect, actual, "PASS" if ok else "FAIL"))

for _ in range(20):
    if req("GET", "/api/version")[0] == 200: break
    time.sleep(0.5)

s, admin = login("admin", "admin123")
check("超管登录", 200, s)
s, b = req("GET", "/api/departments", token=admin)
depts = json.loads(b)
dept1 = depts[0]["id"]

# 建两个同部门执行者
req("POST", "/api/users", token=admin, payload={"username":"lintest","name":"林测试","role":"executor","dept_id":dept1,"password":"123456"})
req("POST", "/api/users", token=admin, payload={"username":"chentest","name":"陈测试","role":"executor","dept_id":dept1,"password":"123456"})
s, _ = login("lintest","123456"); _, lin = login("lintest","123456")
_, chen = login("chentest","123456")
# 超管给 chen 建一个任务
s, b = req("POST","/api/tasks",token=admin,payload={"title":"陈的任务","type":"daily","assignee":"陈测试","dept_id":dept1})
t_chen = json.loads(b)

# 1) 部门内互见：lin 应能看到 chen 的任务
s, b = req("GET","/api/tasks",token=lin)
lin_tasks = json.loads(b)
visible = any(t["id"]==t_chen["id"] for t in lin_tasks)
check("执行者 lin 可见同部门 chen 的任务", True, visible)

# 2) 完成人记录：超管完成 chen 的任务，completed_by 应为 admin 用户名
s, b = req("POST", f"/api/tasks/{t_chen['id']}/toggle", token=admin)
t = json.loads(b)
check("toggle 后 status=done", "done", t.get("status"))
check("completed_by 记录为 admin", "admin", t.get("completed_by"))

# 3) 重开清空完成人
s, b = req("POST", f"/api/tasks/{t_chen['id']}/toggle", token=admin)
t = json.loads(b)
check("重开后 status=todo", "todo", t.get("status"))
check("重开后 completed_by 清空", "", t.get("completed_by"))

# 4) 执行者只能 toggle 自己的任务（写权限仍收窄）
s, b = req("POST","/api/tasks",token=admin,payload={"title":"林的任务","type":"daily","assignee":"林测试","dept_id":dept1})
t_lin = json.loads(b)
# chen 尝试 toggle lin 的任务 -> 应 403
s, _ = req("POST", f"/api/tasks/{t_lin['id']}/toggle", token=chen)
check("非负责人 chen toggle lin 任务(403)", 403, s)

passed = sum(1 for r in results if r[3]=="PASS")
print(f"\n=== 部门互见 & 完成人 测试：{passed}/{len(results)} 通过 ===")
for n,e,a,st in results:
    print(f"  {'✓' if st=='PASS' else '✗'} {n} (期望:{e} 实际:{a})" if st!='PASS' else f"  ✓ {n}")
sys.exit(0 if passed==len(results) else 1)
