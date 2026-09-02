#!/usr/bin/env bash
# 部署前本地冒烟测试：覆盖新端点 + 安全三项（使用官方中文表头格式）
set -u
cd "$(dirname "$0")/.."
BIN=/workspace/shift-workbench/swb
DB=/tmp/swb_smoke.db
rm -f "$DB"
PORT=8077
APP_PORT=$PORT DB_PATH="$DB" JWT_SECRET=testsecret AES_KEY=testaeskey-1234567890abcdef "$BIN" >/tmp/swb_smoke.log 2>&1 &
PID=$!
echo "server pid=$PID port=$PORT"
for i in $(seq 1 40); do
  if curl -s -o /dev/null "http://127.0.0.1:$PORT/api/version" 2>/dev/null; then break; fi
  sleep 0.3
done

B="http://127.0.0.1:$PORT"
pass=0; fail=0
ck(){ if [ "$2" = "$3" ]; then echo "PASS $1"; pass=$((pass+1)); else echo "FAIL $1 (want $3 got $2)"; fail=$((fail+1)); fi; }
TOK=$(curl -s -X POST "$B/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}' | grep -o '"token":"[^"]*"' | sed 's/"token":"//;s/"//')
echo "token len=${#TOK}"
AUTH="Authorization: Bearer $TOK"

# 1. 任务样例下载 (BOM)
code=$(curl -s -o /tmp/task_sample.csv -w '%{http_code}' "$B/api/templates/task-sample" -H "$AUTH")
ck "task-sample-http" "$code" "200"
head -c 3 /tmp/task_sample.csv | od -An -tx1 | grep -q 'ef bb bf' && echo "PASS task-sample-BOM" || { echo "FAIL task-sample-BOM"; fail=$((fail+1)); }

# 2. 任务导入 CSV（官方中文表头）
printf '标题,班次,类型,时间,优先级,备注,负责人\n开门检查,早班,每日,09:00,高,注意安全,admin\n晚班盘点,晚班,每日,21:00,中,,\n' > /tmp/tasks.csv
res=$(curl -s -X POST "$B/api/tasks/import" -H "$AUTH" -F "file=@/tmp/tasks.csv")
echo "import tasks: $res"
echo "$res" | grep -q '"created":2' && echo "PASS tasks-import" || { echo "FAIL tasks-import"; fail=$((fail+1)); }

# 3. 任务列表
cnt=$(curl -s "$B/api/tasks" -H "$AUTH" | grep -o '"id"' | wc -l)
echo "tasks count=$cnt"; [ "$cnt" -ge 2 ] && echo "PASS tasks-list" || { echo "FAIL tasks-list"; fail=$((fail+1)); }

# 4. 任务导出
code=$(curl -s -o /tmp/tasks_out.csv -w '%{http_code}' "$B/api/tasks/export" -H "$AUTH")
ck "tasks-export-http" "$code" "200"

# 5. 批量完成 + 重开
ids=$(curl -s "$B/api/tasks" -H "$AUTH" | grep -o '"id":[0-9]*' | head -2 | grep -o '[0-9]*' | paste -sd, -)
res=$(curl -s -X POST "$B/api/tasks/batch" -H "$AUTH" -H 'Content-Type: application/json' -d "{\"action\":\"complete\",\"ids\":[$ids]}")
echo "batch complete: $res"
echo "$res" | grep -q '"processed":2' && echo "PASS batch-complete" || { echo "FAIL batch-complete"; fail=$((fail+1)); }
res=$(curl -s -X POST "$B/api/tasks/batch" -H "$AUTH" -H 'Content-Type: application/json' -d "{\"action\":\"reopen\",\"ids\":[$ids]}")
echo "$res" | grep -q '"processed":2' && echo "PASS batch-reopen" || echo "FAIL batch-reopen"

# 6. 模板 增/列/下载 (超级管理员)
res=$(curl -s -X POST "$B/api/templates" -H "$AUTH" -H 'Content-Type: application/json' -d '{"type":"task","name":"标准巡检模版","content":"标题,班次,类型,时间,优先级,备注,负责人","dept_id":0}')
echo "create template: $res"
tid=$(echo "$res" | grep -o '"id":[0-9]*' | grep -o '[0-9]*' | head -1)
[ -n "$tid" ] && echo "PASS template-create (id=$tid)" || { echo "FAIL template-create"; fail=$((fail+1)); }
code=$(curl -s -o /tmp/tpl.csv -w '%{http_code}' "$B/api/templates/$tid/download" -H "$AUTH")
ck "template-download-http" "$code" "200"

# 7. 日志导出
code=$(curl -s -o /tmp/logs.csv -w '%{http_code}' "$B/api/logs/export" -H "$AUTH")
ck "logs-export-http" "$code" "200"

# 8. 改密长度校验（在限流锁定之前做，避免级联失败）
rp=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/users/1/reset-password" -H "$AUTH" -H 'Content-Type: application/json' -d '{"password":"123"}')
ck "resetpw-short-400" "$rp" "400"
rp2=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/users/1/reset-password" -H "$AUTH" -H 'Content-Type: application/json' -d '{"password":"newpass123"}')
ck "resetpw-ok-200" "$rp2" "200"
lg=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"newpass123"}')
ck "login-newpw-200" "$lg" "200"
# 改回 admin123 方便线上默认
curl -s -o /dev/null -X POST "$B/api/users/1/reset-password" -H "$AUTH" -H 'Content-Type: application/json' -d '{"password":"admin123"}'

# 9. 登出失效：登出后再用旧 token -> 401
lo=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/logout" -H "$AUTH")
echo "logout http=$lo"
reuse=$(curl -s -o /dev/null -w '%{http_code}' "$B/api/tasks" -H "$AUTH")
ck "logout-token-invalid" "$reuse" "401"

# 10. 登录限流：5次失败 -> 401, 第6次(含正确) -> 429
for i in $(seq 1 5); do curl -s -o /dev/null -X POST "$B/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"wrong"}'; done
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}')
ck "login-throttle-429" "$code" "429"

echo "===================="
echo "PASS=$pass FAIL=$fail"
kill "$PID" 2>/dev/null
exit $fail
