# -*- coding: utf-8 -*-
"""v0.13.1 月度任务「提醒时点 09:00」与「完成期限一整天」分离 —— 线上验证"""
import json
import urllib.error
import urllib.request

BASE = 'http://127.0.0.1:8090'
USER, PWD = 'admin', 'admin123'
ok, fail = 0, 0


def check(cond, msg, detail=''):
    global ok, fail
    if cond:
        ok += 1
        print('  PASS  %s' % msg)
    else:
        fail += 1
        print('  FAIL  %s %s' % (msg, detail))


def req(path, token=None, method='GET', body=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header('Content-Type', 'application/json')
    if token:
        r.add_header('Authorization', 'Bearer ' + token)
    try:
        with urllib.request.urlopen(r, timeout=20) as resp:
            return resp.status, json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8', 'ignore')


print('=== 1. 版本号 ===')
st, v = req('/api/version')
check(st == 200 and v.get('version') == 'v0.13.1', '线上版本为 v0.13.1', '实际 %s %s' % (st, v))

print('\n=== 2. 登录 ===')
st, r = req('/api/auth/login', method='POST', body={'username': USER, 'password': PWD})
token = r.get('token') if isinstance(r, dict) else None
check(st == 200 and bool(token), 'admin 登录成功', '实际 %s' % st)
if not token:
    raise SystemExit(1)

print('\n=== 3. 逾期任务的类型分布 ===')
st, tasks = req('/api/tasks', token)
check(st == 200 and isinstance(tasks, list), 'GET /api/tasks 返回 200')

overdue = [t for t in tasks if t.get('overdue')]
month_overdue = [t for t in overdue if t.get('type') == 'monthly']
daily_overdue = [t for t in overdue if t.get('type') == 'daily']

print('  逾期合计 %d 条（每日 %d / 月度 %d）' % (len(overdue), len(daily_overdue), len(month_overdue)))
for t in overdue:
    print('    · [%s] %s  截止 %s' % (t.get('type'), t.get('title'), t.get('deadline') or t.get('time')))

check(len(month_overdue) == 0,
      '截止日为今天的月度任务不再被判逾期（完成期限是一整天）',
      '仍被判逾期的月度任务：%s' % [t.get('title') for t in month_overdue])

print('\n=== 4. 今明两天截止的月度任务状态 ===')
today = [t for t in tasks if t.get('type') == 'monthly' and (t.get('deadline') or '').startswith('2026-09-01')]
for t in today:
    print('    · %s  状态=%s 逾期=%s 今日应办=%s'
          % (t.get('title'), t.get('status'), t.get('overdue'), t.get('due_today')))
check(all(not t.get('overdue') for t in today),
      '9/1 截止的月度任务全部未标逾期', )

print('\n=== 5. 统计接口一致性 ===')
st, cnt = req('/api/tasks/counts', token)
if isinstance(cnt, dict):
    print('  逾期 %s / 今日 %s / 应处理合计 %s' % (cnt.get('overdue'), cnt.get('today'), cnt.get('due_total')))
    check(cnt.get('overdue') == len(overdue),
          '统计接口逾期数与列表一致', '列表 %d，接口 %s' % (len(overdue), cnt.get('overdue')))
else:
    check(False, 'GET /api/tasks/counts 返回异常', str(cnt)[:120])

print('\n=== 6. 每日任务的时段逾期不受影响 ===')
check(len(daily_overdue) > 0,
      '每日任务仍按执行时段判定逾期（过点未做照样标红）',
      '实际 %d 条' % len(daily_overdue))

print('\n=== 结果：%d 通过 / %d 失败 ===' % (ok, fail))
raise SystemExit(1 if fail else 0)
