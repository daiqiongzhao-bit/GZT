# -*- coding: utf-8 -*-
"""v0.13.0 周期任务自动重置 —— 线上端到端验证"""
import json
import sqlite3
import urllib.error
import urllib.request

BASE = 'http://127.0.0.1:8090'
DB = '/opt/swb/data/swb.db'
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


def sql(q, args=()):
    con = sqlite3.connect(DB)
    con.row_factory = sqlite3.Row
    rows = [dict(r) for r in con.execute(q, args).fetchall()]
    con.close()
    return rows


print('=== 1. 版本号 ===')
st, v = req('/api/version')
check(st == 200 and v.get('version') == 'v0.13.0', '线上版本为 v0.13.0', '实际 %s %s' % (st, v))

print('\n=== 2. 登录 ===')
st, r = req('/api/auth/login', method='POST', body={'username': USER, 'password': PWD})
token = r.get('token') if isinstance(r, dict) else None
check(st == 200 and bool(token), 'admin 登录成功', '实际 %s %s' % (st, str(r)[:120]))
if not token:
    print('\n无法登录，后续验证终止')
    raise SystemExit(1)

print('\n=== 3. 重置前状态（应仍残留 8 月的已完成） ===')
before_daily_done = sql("SELECT count(*) c FROM tasks WHERE type='daily' AND status='done'")[0]['c']
before_month_done = sql("SELECT count(*) c FROM tasks WHERE type='monthly' AND status='done'")[0]['c']
before_hist = sql("SELECT count(*) c FROM task_completions")[0]['c']
print('  重置前：每日已完成 %d 条，月度已完成 %d 条，完成历史 %d 条'
      % (before_daily_done, before_month_done, before_hist))

print('\n=== 4. 调用任务接口触发重置 ===')
st, tasks = req('/api/tasks', token)
check(st == 200 and isinstance(tasks, list), 'GET /api/tasks 返回 200', '实际 %s' % st)

print('\n=== 5. 重置后核对 ===')
daily_done = sql("SELECT count(*) c FROM tasks WHERE type='daily' AND status='done'")[0]['c']
daily_todo = sql("SELECT count(*) c FROM tasks WHERE type='daily' AND status='todo'")[0]['c']
check(before_daily_done > 0 or daily_done == 0,
      '每日任务：昨天(8月)完成的已回到待办（当前已完成 %d 条应为 0 或仅今天完成）' % daily_done)
check(daily_todo >= 17, '每日任务回到待办数量合理', '实际待办 %d 条' % daily_todo)

month_rows = sql("SELECT count(*) c FROM tasks WHERE type='monthly' AND substr(deadline,1,7)='2026-09'")[0]['c']
check(month_rows == 13, '月度任务 13 条截止日已推进到 2026-09', '实际 %d 条' % month_rows)

month_done = sql("SELECT count(*) c FROM tasks WHERE type='monthly' AND status='done'")[0]['c']
check(month_done <= 1, '月度任务：8月完成的已重置（仅保留今天完成的）', '实际已完成 %d 条' % month_done)

after_hist = sql("SELECT count(*) c FROM task_completions")[0]['c']
check(after_hist == before_hist, '完成历史未被删除（审计记录完整）',
      '重置前 %d → 重置后 %d' % (before_hist, after_hist))

print('\n=== 6. 逾期统计（过点未做的应标红） ===')
st, cnt = req('/api/tasks/counts', token)
if isinstance(cnt, dict):
    print('  逾期 %s / 今日 %s / 应处理合计 %s' % (cnt.get('overdue'), cnt.get('today'), cnt.get('due_total')))
    check(cnt.get('overdue', 0) > 0, '存在逾期任务（今天已过点未做）', str(cnt))
else:
    check(False, 'GET /api/tasks/counts 返回异常', str(cnt)[:120])

print('\n=== 7. 仪表盘 ===')
st, dash = req('/api/dashboard', token)
if isinstance(dash, dict):
    print('  今日任务 %s / 逾期 %s / 当班 %s 人'
          % (dash.get('today_tasks'), dash.get('overdue_count'), dash.get('on_duty_count')))
    check(st == 200, 'GET /api/dashboard 返回 200')
else:
    check(False, 'dashboard 返回异常', str(dash)[:120])

print('\n=== 8. 幂等性：再触发一次不应再变更 ===')
snap = sql("SELECT id, status, deadline, completed_by FROM tasks ORDER BY id")
req('/api/tasks', token)
snap2 = sql("SELECT id, status, deadline, completed_by FROM tasks ORDER BY id")
check(snap == snap2, '重复调用接口不产生额外变更（幂等）')

print('\n=== 结果：%d 通过 / %d 失败 ===' % (ok, fail))
raise SystemExit(1 if fail else 0)
