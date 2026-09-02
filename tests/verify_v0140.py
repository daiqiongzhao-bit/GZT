# -*- coding: utf-8 -*-
"""v0.14.0 班表无账号警告 + 账号=工号规则 —— 线上验证（含真实导入与清理）"""
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
TEST_NAME = '临时测试校验X'
TEST_EMP = '99991'


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


def upload_csv(token, filename, csv_text):
    boundary = '----wb' + uuid.uuid4().hex
    body = io.BytesIO()
    def w(s):
        body.write(s.encode('utf-8'))
    w('--%s\r\n' % boundary)
    w('Content-Disposition: form-data; name="file"; filename="%s"\r\n' % filename)
    w('Content-Type: text/csv\r\n\r\n')
    w(csv_text)
    w('\r\n--%s--\r\n' % boundary)
    r = urllib.request.Request(BASE + '/api/schedules/import', data=body.getvalue(), method='POST')
    r.add_header('Content-Type', 'multipart/form-data; boundary=' + boundary)
    r.add_header('Authorization', 'Bearer ' + token)
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            return resp.status, json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8', 'ignore')


def sql_exec(q):
    con = sqlite3.connect(DB)
    con.execute(q)
    con.commit()
    con.close()


print('=== 1. 版本号 ===')
st, v = req('/api/version')
check(st == 200 and v.get('version') == 'v0.14.0', '线上版本为 v0.14.0', '实际 %s %s' % (st, v))

print('\n=== 2. 登录 ===')
st, r = req('/api/auth/login', method='POST', body={'username': USER, 'password': PWD})
token = r.get('token') if isinstance(r, dict) else None
check(st == 200 and bool(token), 'admin 登录成功', '实际 %s' % st)
if not token:
    raise SystemExit(1)

print('\n=== 3. 班表导入：无账号人员警告 ===')
csv_text = '日期,班次,人员\n2026-09-30,早班,%s\n' % TEST_NAME
st, r = upload_csv(token, 'test_schedule.csv', csv_text)
print('  导入返回: %s' % json.dumps(r, ensure_ascii=False))
check(st == 200 and r.get('created') == 1, '无账号人员所在行仍正常导入（不阻断）', '实际 %s' % st)
check(TEST_NAME in (r.get('unknown_names') or []),
      'unknown_names 包含「%s」' % TEST_NAME,
      '实际 unknown_names=%s' % r.get('unknown_names'))
# 清理：删除这条测试班表
sql_exec("DELETE FROM schedules WHERE people LIKE '%%%s%%'" % TEST_NAME)
left = sqlite3.connect(DB).execute("SELECT count(*) FROM schedules WHERE people LIKE '%%%s%%'" % TEST_NAME).fetchone()[0]
check(left == 0, '测试班表数据已清理', '残留 %d 条' % left)

print('\n=== 4. 新建用户：工号必填 ===')
st, r = req('/api/users', token, 'POST', {'name': TEST_NAME, 'password': '123456', 'emp_no': '',
                                          'role': 'executor', 'dept_id': 8})
check(st == 400 and '工号' in str(r), '不填工号 → 拒绝并提示工号必填', '实际 %s %s' % (st, str(r)[:80]))

print('\n=== 5. 新建用户：账号自动=工号 ===')
st, r = req('/api/users', token, 'POST', {'name': TEST_NAME, 'password': '123456', 'emp_no': TEST_EMP,
                                          'role': 'executor', 'dept_id': 8})
check(st == 200 and isinstance(r, dict), '填工号 → 创建成功', '实际 %s %s' % (st, str(r)[:80]))
if isinstance(r, dict):
    check(r.get('username') == TEST_EMP, '账号自动 = 工号(%s)' % TEST_EMP, '实际账号 %s' % r.get('username'))
    check(r.get('emp_no') == TEST_EMP, '工号 = %s' % TEST_EMP, '实际 %s' % r.get('emp_no'))
    # 清理测试用户
    uid = r.get('id')
    req('/api/users/%s' % uid, token, 'DELETE')
    gone = sqlite3.connect(DB).execute("SELECT count(*) FROM users WHERE id=?", (uid,)).fetchone()[0]
    check(gone == 0, '测试用户已清理')
else:
    check(False, '创建测试用户失败', str(r)[:120])

print('\n=== 6. 既有账号不受影响 ===')
st, users = req('/api/users', token)
if isinstance(users, list):
    allok = all((u.get('username') == u.get('emp_no')) or (u.get('username') == 'admin') for u in users)
    check(allok, '全部既有账号仍等于工号（admin 例外）')
else:
    check(False, '用户列表接口异常', str(users)[:80])

print('\n=== 结果：%d 通过 / %d 失败 ===' % (ok, fail))
raise SystemExit(1 if fail else 0)
