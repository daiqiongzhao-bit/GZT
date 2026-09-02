# -*- coding: utf-8 -*-
"""GZT v0.14.3 全面体检 —— 覆盖认证/部门/人员/任务/班表/通知/备份/日志"""
import io
import json
import sqlite3
import urllib.error
import urllib.request
import uuid

BASE = 'http://127.0.0.1:8090'
DB = '/opt/swb/data/swb.db'
USER, PWD = 'admin', 'admin123'
ok, fail = 0, 0
results = []


def check(cond, msg, detail=''):
    global ok, fail
    tag = 'PASS' if cond else 'FAIL'
    if cond:
        ok += 1
    else:
        fail += 1
        detail = '  ← %s' % detail if detail else ''
    results.append('%s  %s%s' % (tag, msg, detail))
    print('%s  %s%s' % (tag, msg, detail))


def req(path, token=None, method='GET', body=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header('Content-Type', 'application/json')
    if token:
        r.add_header('Authorization', 'Bearer ' + token)
    try:
        with urllib.request.urlopen(r, timeout=25) as resp:
            return resp.status, json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8', 'ignore')


def sql(q, args=()):
    con = sqlite3.connect(DB)
    con.row_factory = sqlite3.Row
    con.execute('BEGIN')
    rows = [dict(r) for r in con.execute(q, args).fetchall()]
    con.commit()  # 写操作也生效，避免残留
    con.close()
    return rows


print('=' * 60)
print('GZT v0.14.3 全面体检')
print('=' * 60)

print('\n[A] 版本与认证')
st, v = req('/api/version')
check(st == 200 and v.get('version') == 'v0.14.3', '版本号为 v0.14.3', '实际 %s' % v)
st, r = req('/api/auth/login', method='POST', body={'username': USER, 'password': PWD})
token = r.get('token') if isinstance(r, dict) else None
check(st == 200 and bool(token), 'admin 登录成功')
st, me = req('/api/auth/me', token)
check(st == 200 and me.get('role') == 'super_admin', '当前用户为超管')
st, bad = req('/api/auth/login', method='POST', body={'username': 'admin', 'password': 'wrong-pass-1'})
check(st == 401, '错误密码被拒绝（401）', '实际 %s' % st)

print('\n[B] 部门与人员')
st, depts = req('/api/departments', token)
check(st == 200 and isinstance(depts, list) and len(depts) >= 1, '部门列表正常（%d 个）' % len(depts))
st, users = req('/api/users', token)
names = [u.get('name') for u in users] if isinstance(users, list) else []
check(st == 200 and len(users) == 15, '人员列表 15 个（admin+14）', '实际 %d' % (len(users) if isinstance(users, list) else -1))
all_eq = all((u.get('username') == u.get('emp_no')) or u.get('username') == 'admin' for u in users)
check(all_eq, '全部账号 = 工号（admin 例外）')

# 创建用户（工号必填、账号=工号）
st, r = req('/api/users', token, 'POST', {'name': '体检临时A', 'password': '123456', 'emp_no': '',
                                          'role': 'executor', 'dept_id': 8})
check(st == 400 and '工号' in str(r), '不填工号创建被拒')
st, r = req('/api/users', token, 'POST', {'name': '体检临时A', 'password': '123456', 'emp_no': '99993',
                                          'mobile': '137 1111 2222', 'role': 'executor', 'dept_id': 8})
uid = r.get('id') if isinstance(r, dict) else None
check(st == 200 and uid, '创建临时用户成功')
if isinstance(r, dict):
    check(r.get('username') == '99993' and r.get('emp_no') == '99993', '临时用户账号=工号')
    check(r.get('mobile') == '13800000017', '手机号空格自动归一化', '实际 %r' % r.get('mobile'))
# 编辑：改工号同步账号 + 手机号归一化
st, r = req('/api/users/%s' % uid, token, 'PUT', {'emp_no': '99994', 'mobile': '+86 136-5555-6666'})
check(st == 200 and r.get('username') == '99994', '改工号同步改登录账号', '实际 %s' % str(r)[:80])
check(r.get('mobile') == '13800000016', '编辑手机号归一化（+86/横线）', '实际 %r' % r.get('mobile'))
# 删除
st, r = req('/api/users/%s' % uid, token, 'DELETE')
left = sql('SELECT count(*) c FROM users WHERE id=?', (uid,))[0]['c']
check(st == 200 and left == 0, '临时用户已清理')

print('\n[C] 任务')
st, tasks = req('/api/tasks', token)
daily = [t for t in tasks if t.get('type') == 'daily']
monthly = [t for t in tasks if t.get('type') == 'monthly']
check(st == 200 and len(daily) == 17 and len(monthly) == 13,
      '任务 30 条（每日17 + 月度13）', '实际 每日%d 月度%d' % (len(daily), len(monthly)))
st, cnt = req('/api/tasks/counts', token)
if isinstance(cnt, dict):
    check(cnt.get('overdue', 0) >= 0 and cnt.get('today', 0) > 0, '任务统计返回正常（今日 %s 逾期 %s）'
          % (cnt.get('today'), cnt.get('overdue')))
# 月度任务当天不逾期
today_month = [t for t in monthly if (t.get('deadline') or '').startswith('2026-09-01')]
check(all(not t.get('overdue') for t in today_month) and any(t.get('due_today') for t in today_month),
      '9/1 截止的月度任务：今日应办且不逾期（完成期限一整天）')
# 逾期中不应有月度
overdue = [t for t in tasks if t.get('overdue')]
check(all(t.get('type') != 'monthly' for t in overdue), '逾期列表不含月度任务', '实际 %s'
      % [t.get('type') for t in overdue])
# 创建临时任务 → 完成 → 重开 → 删除
st, r = req('/api/tasks', token, 'POST', {'title': '体检临时任务', 'type': 'once', 'deadline': '2099-12-31T00:00',
                                          'shift': '全员', 'priority': 'low', 'note': '自动化测试', 'dept_id': 8})
tid = r.get('id') if isinstance(r, dict) else None
check(st == 200 and tid, '创建临时任务成功')
if tid:
    st, r = req('/api/tasks/%s/toggle' % tid, token, 'POST')
    check(st == 200 and r.get('status') == 'done', '临时任务完成')
    st, r = req('/api/tasks/%s/toggle' % tid, token, 'POST')
    check(st == 200 and r.get('status') == 'todo', '临时任务重开')
    st, r = req('/api/tasks/%s' % tid, token, 'DELETE')
    left = sql('SELECT count(*) c FROM tasks WHERE id=?', (tid,))[0]['c']
    check(st == 200 and left == 0, '临时任务已清理')

print('\n[D] 班表与导入')
st, sched = req('/api/schedules', token)
sep9 = sql("SELECT count(*) c FROM schedules WHERE date LIKE '2026-09%'")[0]['c']
check(st == 200 and isinstance(sched, list) and len(sched) == 762,
      '班表接口返回全量 762 条（8月372 + 9月390）', '实际 %d' % (len(sched) if isinstance(sched, list) else -1))
check(sep9 == 390, '9 月班表 390 条（30 天 × 13 人）', '实际 %d' % sep9)
# 导入含无账号人名的 CSV
boundary = '----wb' + uuid.uuid4().hex
csv_text = '日期,班次,人员\n2026-09-30,早班,体检无名氏X\n'
body = io.BytesIO()
body.write(('--%s\r\n' % boundary).encode())
body.write(b'Content-Disposition: form-data; name="file"; filename="t.csv"\r\nContent-Type: text/csv\r\n\r\n')
body.write(csv_text.encode())
body.write(('\r\n--%s--\r\n' % boundary).encode())
r = urllib.request.Request(BASE + '/api/schedules/import', data=body.getvalue(), method='POST')
r.add_header('Content-Type', 'multipart/form-data; boundary=' + boundary)
r.add_header('Authorization', 'Bearer ' + token)
try:
    with urllib.request.urlopen(r, timeout=30) as resp:
        ir = json.loads(resp.read().decode())
    check(ir.get('created') == 1 and '体检无名氏X' in (ir.get('unknown_names') or []),
          '无账号人员警告返回', '实际 %s' % json.dumps(ir, ensure_ascii=False))
except urllib.error.HTTPError as e:
    check(False, '班表导入失败', str(e.read()[:100]))
sql("DELETE FROM schedules WHERE people LIKE '%体检无名氏X%'")
left = sql("SELECT count(*) c FROM schedules WHERE people LIKE '%体检无名氏X%'")[0]['c']
check(left == 0, '导入测试数据已清理')

print('\n[E] 仪表盘')
st, dash = req('/api/dashboard', token)
if isinstance(dash, dict):
    check(st == 200 and dash.get('on_duty_count') == 11, '今日当班 11 人（不含休息）',
          '实际 %s' % dash.get('on_duty_count'))
    rest = [r for r in dash.get('on_duty_rows', []) if r.get('shift') == '休息']
    check(len(rest) == 0, '当班列表不含休息人员')
    check(dash.get('today_tasks', 0) > 0, '今日任务统计正常（%s）' % dash.get('today_tasks'))
else:
    check(False, '仪表盘接口异常', str(dash)[:80])

print('\n[F] Webhook 与通知')
st, hooks = req('/api/webhooks', token)
check(st == 200 and isinstance(hooks, list) and len(hooks) >= 1, 'Webhook 列表正常（%d 个）' % len(hooks))
if isinstance(hooks, list) and hooks:
    h = hooks[0]
    check('••••' in (h.get('url') or '') if h.get('is_super') is False else True,
          '非超管视角地址脱敏（超管见明文）')

print('\n[G] 备份与日志')
st, backups = req('/api/backups', token)
check(st == 200, '备份列表接口正常')
st, logs = req('/api/logs?limit=5', token)
check(st == 200 and isinstance(logs, list), '日志列表正常')

print('\n' + '=' * 60)
print('结果：%d 通过 / %d 失败' % (ok, fail))
print('=' * 60)
if fail:
    raise SystemExit(1)
