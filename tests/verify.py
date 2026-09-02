#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""GZT v0.0.1 全面体检：动态断言，不硬编码版本/日期/人数"""
import json
import sqlite3
import urllib.error
import urllib.request
import datetime

BASE = 'http://127.0.0.1:8090'
DB = '/opt/swb/data/swb.db'
ok, fail = 0, 0


def check(cond, msg, detail=''):
    global ok, fail
    if cond:
        ok += 1
        print('PASS  %s' % msg)
    else:
        fail += 1
        print('FAIL  %s  ← %s' % (msg, detail))


def req(path, token=None, method='GET', body=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header('Content-Type', 'application/json')
    if token:
        r.add_header('Authorization', 'Bearer ' + token)
    try:
        with urllib.request.urlopen(r, timeout=25) as resp:
            raw = resp.read().decode('utf-8')
            try:
                return resp.status, json.loads(raw)
            except Exception:
                return resp.status, raw
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8', 'ignore')


def sql(q, args=()):
    con = sqlite3.connect(DB)
    con.row_factory = sqlite3.Row
    rows = [dict(r) for r in con.execute(q, args).fetchall()]
    con.close()
    return rows


def norm(s):
    out = ''.join(ch for ch in s if ch.isdigit())
    if len(out) == 13 and out.startswith('86'):
        out = out[2:]
    if len(out) == 15 and out.startswith('0086'):
        out = out[4:]
    return out


today = datetime.date.today().isoformat()
token = None

print('=' * 60)
print('GZT v0.0.1 全面体检 · %s' % today)
print('=' * 60)

# ---------- [A] 版本与认证 ----------
print('\n[A] 版本与认证')
st, v = req('/api/version')
check(st == 200 and isinstance(v, dict) and v.get('version') == 'v0.0.1', '版本号为 v0.0.1（实际 %s）' % (v.get('version') if isinstance(v, dict) else v))
st, auth = req('/api/auth/login', method='POST', body={'username': 'admin', 'password': 'admin123'})
check(st == 200 and isinstance(auth, dict) and auth.get('token'), 'admin 登录成功')
token = auth.get('token', '') if isinstance(auth, dict) else ''
st, me = req('/api/auth/me', token)
check(st == 200 and isinstance(me, dict) and me.get('role') == 'super_admin', '当前用户为超管')
st, _ = req('/api/auth/login', method='POST', body={'username': 'admin', 'password': 'wrong-pass'})
check(st == 401, '错误密码被拒绝')

# ---------- [B] 部门与人员 ----------
print('\n[B] 部门与人员')
st, depts = req('/api/departments', token)
check(st == 200 and isinstance(depts, list) and len(depts) >= 1, '部门列表正常（%d 个）' % len(depts) if isinstance(depts, list) else 0)
dept_id = depts[0]['id'] if isinstance(depts, list) and depts else 1
st, users = req('/api/users', token)
check(st == 200 and isinstance(users, list) and len(users) >= 10, '人员列表正常（%d 人）' % (len(users) if isinstance(users, list) else 0))
emp_ok = all((u.get('role') == 'super_admin') or (u.get('username') == u.get('emp_no')) for u in users) if isinstance(users, list) else False
check(emp_ok, '全部账号 = 工号（admin 例外）')

suffix = datetime.datetime.now().strftime('%H%M%S')
tmp_no = 'T%s' % suffix[-5:]
tmp_mob_in = '139 0000 0001'
st, u = req('/api/users', token, 'POST', {
    'name': '体检临时', 'emp_no': tmp_no, 'password': 'test123456',
    'mobile': tmp_mob_in, 'role': 'executor', 'dept_id': dept_id})
check(st == 200 and isinstance(u, dict) and u.get('id'), '创建临时用户成功')
tmp_id = u.get('id') if isinstance(u, dict) else None
if tmp_id:
    check(u.get('username') == tmp_no, '临时用户账号=工号')
    check(u.get('mobile') == norm(tmp_mob_in), '手机号空格自动归一化（%s -> %s）' % (tmp_mob_in, u.get('mobile')))
    st2, u2 = req('/api/users/%d' % tmp_id, token, 'PUT', {'mobile': '+86 139 0000 0001'})
    check(st2 == 200 and isinstance(u2, dict) and u2.get('mobile') == '13900000001', '编辑手机号归一化（+86 前缀剥离）')
    req('/api/users/%d' % tmp_id, token, 'DELETE')
    st3, body3 = req('/api/users', token)
    check(st3 == 200 and isinstance(body3, list) and not any(x.get('id') == tmp_id for x in body3), '临时用户已清理')

# ---------- [C] 任务 ----------
print('\n[C] 任务')
st, tasks = req('/api/tasks', token)
tasks = tasks if isinstance(tasks, list) else []
daily = [t for t in tasks if t.get('type') == 'daily']
monthly = [t for t in tasks if t.get('type') == 'monthly']
check(st == 200 and len(daily) >= 5 and len(monthly) >= 5, '任务列表正常（每日 %d + 月度 %d）' % (len(daily), len(monthly)))
check(any(t.get('status') == 'todo' for t in daily), '每日任务存在待办（周期重置后状态正常）')
st, dash = req('/api/dashboard', token)
check(st == 200, '仪表盘统计接口正常')

tmp_task_no = datetime.datetime.now().strftime('%H%M%S')
st, t = req('/api/tasks', token, 'POST', {'title': '体检临时任务%s' % tmp_task_no, 'type': 'once',
                                          'shift': '全员', 'deadline': today + 'T23:00', 'dept_id': dept_id})
check(st == 200 and isinstance(t, dict) and t.get('id'), '创建临时任务成功')
tid = t.get('id') if isinstance(t, dict) else None
if tid:
    st, _ = req('/api/tasks/%d/toggle' % tid, token, 'POST')
    st, t2 = req('/api/tasks', token)
    done = [x for x in t2 if isinstance(x, dict) and x.get('id') == tid] if isinstance(t2, list) else []
    check(done and done[0].get('status') == 'done', '临时任务完成')
    req('/api/tasks/%d/toggle' % tid, token, 'POST')
    st, t3 = req('/api/tasks', token)
    redo = [x for x in t3 if isinstance(x, dict) and x.get('id') == tid] if isinstance(t3, list) else []
    check(redo and redo[0].get('status') == 'todo', '临时任务重开')
    st, _ = req('/api/tasks/batch-delete', token, 'POST', {'ids': [tid]})
    st, t4 = req('/api/tasks', token)
    gone = not any(x.get('id') == tid for x in t4) if isinstance(t4, list) else False
    check(gone, '临时任务已清理')

# ---------- [D] 班表 ----------
print('\n[D] 班表')
st, sch = req('/api/schedules?month=%s' % today[:7], token)
n_month = len(sch) if isinstance(sch, list) else 0
check(st == 200 and n_month > 0, '班表接口正常（当月 %d 条）' % n_month)
rows = sql("select shift, people from schedules where date = ?", (today,))
import json as J
on_duty = []
has_rest = False
for r in rows:
    pl = J.loads(r['people'] or '[]')
    if r['shift'] == '休息':
        if pl:
            has_rest = True
    else:
        on_duty += pl
check(has_rest, '今日含休息班次（排除逻辑可验证）')
dups = [p for p in set(on_duty) if on_duty.count(p) > 1]
check(not dups, '今日无一人多班次')
st, dash2 = req('/api/dashboard', token)
if isinstance(dash2, dict):
    dd = dash2.get('on_duty') or dash2.get('today_on_duty')
    check(dd is not None, '仪表盘含当班统计')
else:
    check(False, '仪表盘含当班统计', str(st))

# ---------- [E] 周期规则 ----------
print('\n[E] 周期规则')
m_this = [t for t in monthly if t.get('status') == 'todo']
m_bad = [t.get('deadline', '') for t in m_this if t.get('deadline', '')[:10] < today[:8] + '01']
check(not m_bad, '月度任务截止日 ≥ 本月 1 日（跨月已推进）')

# ---------- [F] Webhook ----------
print('\n[F] Webhook 与通知')
st, hooks = req('/api/webhooks', token)
check(st == 200 and isinstance(hooks, list) and len(hooks) >= 1, 'Webhook 列表正常（%d 个）' % (len(hooks) if isinstance(hooks, list) else 0))

# ---------- [G] 备份与日志 ----------
print('\n[G] 备份与日志')
st, _ = req('/api/backups', token)
check(st == 200, '备份列表接口正常')
st, logs = req('/api/logs?limit=5', token)
check(st == 200 and isinstance(logs, list), '日志列表正常')
st, _ = req('/api/notifications', token)
check(st == 200, '站内通知接口正常')

print('\n' + '=' * 60)
print('结果：%d 通过 / %d 失败' % (ok, fail))
print('=' * 60)
if fail:
    raise SystemExit(1)
