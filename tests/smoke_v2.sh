#!/usr/bin/env bash
# v0.7 新功能冒烟：角标计数 / 保存前校验(SMTP+Webhook) / 审计IP与保留策略
set -u
cd "$(dirname "$0")/.."
BIN=/workspace/shift-workbench/swb
DB=/tmp/swb_smoke2.db
rm -f "$DB"
PORT=8078
APP_PORT=$PORT DB_PATH="$DB" JWT_SECRET=testsecret AES_KEY=testaeskey-1234567890abcdef "$BIN" >/tmp/swb_smoke2.log 2>&1 &
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

# 1. 任务角标计数接口
res=$(curl -s "$B/api/tasks/counts" -H "$AUTH"); echo "counts: $res"
echo "$res" | grep -q '"overdue":0' && echo "PASS counts-zero" || { echo "FAIL counts-zero"; fail=$((fail+1)); }

# 2. 造一条逾期任务，计数应增加
curl -s -o /dev/null -X POST "$B/api/tasks" -H "$AUTH" -H 'Content-Type: application/json' -d '{"title":"昨日逾期任务","type":"once","deadline":"2020-01-01T09:00","shift":"全员","priority":"high","note":"测试"}'
res=$(curl -s "$B/api/tasks/counts" -H "$AUTH"); echo "counts-after: $res"
echo "$res" | grep -q '"overdue":1' && echo "PASS counts-overdue" || { echo "FAIL counts-overdue"; fail=$((fail+1)); }

# 3. 完成该任务后计数归零
TID=$(curl -s "$B/api/tasks" -H "$AUTH" | grep -o '"id":[0-9]*,"title":"昨日逾期任务"' | grep -o '[0-9]*' | head -1)
curl -s -o /dev/null -X POST "$B/api/tasks/$TID/toggle" -H "$AUTH"
res=$(curl -s "$B/api/tasks/counts" -H "$AUTH"); echo "counts-done: $res"
echo "$res" | grep -q '"overdue":0' && echo "PASS counts-cleared" || { echo "FAIL counts-cleared"; fail=$((fail+1)); }

# 4. 日志保留配置：非法值拒绝，合法值通过
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/settings/log-retention" -H "$AUTH" -H 'Content-Type: application/json' -d '{"days":-1}')
ck "retention-neg-400" "$code" "400"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/settings/log-retention" -H "$AUTH" -H 'Content-Type: application/json' -d '{"days":99999}')
ck "retention-big-400" "$code" "400"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/settings/log-retention" -H "$AUTH" -H 'Content-Type: application/json' -d '{"days":365}')
ck "retention-ok-200" "$code" "200"
res=$(curl -s "$B/api/settings" -H "$AUTH"); echo "settings: $res"
echo "$res" | grep -q '"log_retention_days":365' && echo "PASS retention-in-settings" || { echo "FAIL retention-in-settings"; fail=$((fail+1)); }

# 5. Webhook 保存前校验：不可达地址应拒绝保存
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/webhooks" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"坏渠道","url":"http://127.0.0.1:1/xx","type":"wecom"}')
echo "webhook-bad http=$code"
ck "webhook-reject-400" "$code" "400"
cnt=$(curl -s "$B/api/webhooks" -H "$AUTH" | grep -o '"id"' | wc -l)
echo "webhooks count=$cnt"; [ "$cnt" = "0" ] && echo "PASS webhook-not-saved" || { echo "FAIL webhook-not-saved"; fail=$((fail+1)); }

# 6. SMTP 保存前校验：坏主机应拒绝（连接失败），空配置可直接保存
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/settings/smtp" -H "$AUTH" -H 'Content-Type: application/json' -d '{"smtp_host":"smtp.invalid","smtp_port":465,"smtp_user":"u","smtp_pass":"p","smtp_from":"u@x.com","notify_emails":"a@x.com"}')
echo "smtp-bad http=$code"
ck "smtp-reject-400" "$code" "400"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/settings/smtp" -H "$AUTH" -H 'Content-Type: application/json' -d '{"smtp_host":"","smtp_port":0,"smtp_user":"","smtp_pass":"","smtp_from":"","notify_emails":""}')
ck "smtp-empty-save-200" "$code" "200"

# 7. 审计日志带 IP（登录/操作后查询）
ip=$(curl -s "$B/api/logs?limit=10" -H "$AUTH" | grep -o '"ip":"[^"]*"' | head -1)
echo "log ip sample: $ip"
[ -n "$ip" ] && echo "PASS log-has-ip" || { echo "FAIL log-has-ip"; fail=$((fail+1)); }

echo "===================="
echo "PASS=$pass FAIL=$fail"
kill "$PID" 2>/dev/null
exit $fail
