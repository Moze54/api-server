#!/bin/bash
# 把 api-server 11 个接口接入 apiHub（api.uuhb.cn = 本机 8081）。
# 凭据从 /www/apihub-prod/.env 读取，全部操作在服务器本地完成。
# 上游密钥通过环境变量传入（不写入仓库）：
#   API_SERVER_KEY=sk_xxx bash integrate_apihub.sh
set -u
ENVF=/www/apihub-prod/.env
BASE=http://127.0.0.1:8081/api/v1
AH_KEY=${API_SERVER_KEY:?请通过环境变量 API_SERVER_KEY 传入 api-server 的 apihub 密钥}
UPSTREAM=${API_SERVER_UPSTREAM:-http://127.0.0.1:8300}

getv() { grep "^$1=" "$ENVF" | head -1 | cut -d= -f2- | tr -d '\r'; }

# ---- 1. 私网放行 ----
if [ "$(getv GATEWAY_ALLOW_PRIVATE_IP)" != "true" ]; then
  sed -i '/^GATEWAY_ALLOW_PRIVATE_IP=/d' "$ENVF" 2>/dev/null
  echo "GATEWAY_ALLOW_PRIVATE_IP=true" >> "$ENVF"
  systemctl restart apihub-prod
  sleep 3
  echo "[env] GATEWAY_ALLOW_PRIVATE_IP=true 已写入并重启 apihub-prod: $(curl -s -m 5 http://127.0.0.1:8081/health)"
else
  echo "[env] GATEWAY_ALLOW_PRIVATE_IP 已开启"
fi

# ---- 2. 登录 ----
EMAIL=$(getv ADMIN_EMAIL); PASS=$(getv ADMIN_PASSWORD)
LOGIN=$(curl -s -m 15 -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}")
TOKEN=$(echo "$LOGIN" | grep -o '"token":"[^"]*"' | head -1 | sed 's/.*:"//;s/"$//')
if [ -z "$TOKEN" ]; then echo "[login] 失败: $(echo "$LOGIN" | head -c 150)"; exit 1; fi
echo "[login] ok"
AUTH="Authorization: Bearer $TOKEN"
CT='Content-Type: application/json'

# JSON 助手：取 data.id / 按 code 找 id
data_id() { echo "$1" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*'; }
find_by_code() { # $1=列表路径 $2=code → id 或空
  R=$(curl -s -m 15 "$BASE/admin/$1" -H "$AUTH")
  echo "$R" | sed 's/},{/}\n{/g' | grep "\"code\":\"$2\"" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*'
}
post() { curl -s -m 20 -X POST "$BASE/admin/$1" -H "$AUTH" -H "$CT" -d "$2"; }
put() { curl -s -m 20 -X PUT "$BASE/admin/$1" -H "$AUTH" -H "$CT" -d "$2"; }

# ---- 3. provider ----
PID=$(find_by_code providers uuhb-self)
if [ -z "$PID" ]; then
  PID=$(data_id "$(post providers '{"code":"uuhb-self","name":"UUHB自营上游","website":"https://api.uuhb.cn","description":"自建 api-server 接口服务（本机 8300）"}')")
  echo "[provider] 创建 id=$PID"
else
  echo "[provider] 已存在 id=$PID"
fi
[ -z "$PID" ] && { echo "provider 失败"; exit 1; }

# ---- 4. service ----
SID=$(find_by_code upstream-services apiserver)
if [ -z "$SID" ]; then
  SID=$(data_id "$(post upstream-services "{\"provider_id\":$PID,\"code\":\"apiserver\",\"name\":\"api-server 接口服务\",\"base_url\":\"$UPSTREAM\",\"description\":\"自建爬虫/包装接口 11 个\",\"timeout_ms\":30000}")")
  echo "[service] 创建 id=$SID"
else
  echo "[service] 已存在 id=$SID"
fi
[ -z "$SID" ] && { echo "service 失败"; exit 1; }

# ---- 5. channel ----
CID=$(find_by_code "upstream-services/$SID/channels" main)
if [ -z "$CID" ]; then
  CID=$(data_id "$(post "upstream-services/$SID/channels" "{\"service_id\":$SID,\"code\":\"main\",\"name\":\"本机主通道\",\"base_url\":\"$UPSTREAM\",\"auth_type\":\"api_key\",\"credentials\":{\"header\":\"X-API-Key\",\"value\":\"$AH_KEY\"},\"priority\":100,\"weight\":1,\"timeout_ms\":30000}")")
  echo "[channel] 创建 id=$CID"
else
  echo "[channel] 已存在 id=$CID"
fi
[ -z "$CID" ] && { echo "channel 失败"; exit 1; }

# ---- 6. routing group ----
GID=$(find_by_code routing-groups apiserver-main)
if [ -z "$GID" ]; then
  GID=$(data_id "$(post routing-groups "{\"code\":\"apiserver-main\",\"name\":\"api-server 主路由\",\"strategy\":\"failover\",\"channels\":[{\"channel_id\":$CID,\"priority\":100,\"weight\":1,\"status\":1}]}")")
  echo "[routing] 创建 id=$GID"
else
  echo "[routing] 已存在 id=$GID"
fi
[ -z "$GID" ] && { echo "routing 失败"; exit 1; }

# ---- 7. 分类（分类表用 slug 不是 code） ----
CATID=$(curl -s -m 15 "$BASE/admin/categories" -H "$AUTH" | grep -o '"slug":"self-built"[^}]*"id":[0-9]*\|"id":[0-9]*[^}]*"slug":"self-built"' | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*')
if [ -z "$CATID" ]; then
  CATID=$(data_id "$(post categories '{"slug":"self-built","name":"自营接口","description":"api-server 自建能力","sort_order":1}')")
  echo "[category] 创建 id=$CATID"
else
  echo "[category] 已存在 id=$CATID"
fi

# ---- 8. 11 个产品 ----
PRODUCT_IDS=""
mk() { # slug name summary example
  local slug=$1 name=$2 summary=$3 example=$4
  local esc=${example//\"/\\\"}
  local CODE; CODE=$(curl -s -o /dev/null -w '%{http_code}' -m 15 "$BASE/admin/products/slug/$slug" -H "$AUTH")
  if [ "$CODE" = "200" ]; then
    PRODUCT_IDS="$PRODUCT_IDS$(curl -s -m 15 "$BASE/admin/products/slug/$slug" -H "$AUTH" | grep -o '"id":[0-9]*' | head -1 | grep -o '[0-9]*') "
    echo "[product] $slug 已存在，跳过"
    return
  fi
  local PR; PR=$(post products "{\"category_id\":$CATID,\"slug\":\"$slug\",\"name\":\"$name\",\"summary\":\"$summary\",\"description\":\"$name。请求体示例: $esc\",\"icon\":\"\",\"visibility\":\"public\",\"sort_order\":10}")
  local PRID; PRID=$(data_id "$PR")
  if [ -z "$PRID" ]; then echo "[product] $slug 创建失败: $(echo "$PR" | head -c 120)"; return; fi
  local EP; EP=$(post "products/$PRID/endpoints" "{\"code\":\"main\",\"name\":\"$name\",\"description\":\"$summary\",\"method\":\"POST\",\"path\":\"/api/v1/fetch/$slug\",\"request_content_type\":\"application/json\",\"response_content_type\":\"application/json\",\"request_schema\":{\"type\":\"object\"},\"response_schema\":{\"type\":\"object\"},\"example_request\":\"$esc\",\"example_response\":\"{\\\"code\\\":0}\",\"timeout_ms\":30000,\"status\":\"enabled\",\"sort_order\":1}")
  local EPID; EPID=$(data_id "$EP")
  if [ -z "$EPID" ]; then echo "[endpoint] $slug 失败: $(echo "$EP" | head -c 120)"; return; fi
  put "endpoints/$EPID/execution" "{\"routing_group_id\":$GID,\"adapter_id\":null}" >/dev/null
  put "products/$PRID/status" '{"status":"published"}' >/dev/null
  PRODUCT_IDS="$PRODUCT_IDS$PRID "
  echo "[product] $slug 创建并发布 (product=$PRID endpoint=$EPID)"
}
mk weather "天气预报" "中国天气网官方：实况+7天预报+生活指数+预警" '{"city":"北京"}'
mk pageinfo "网页信息提取" "抓取网页标题/描述/摘要/链接" '{"url":"https://example.com"}'
mk goldprice "金价行情" "黄金白银实时行情（伦敦金/COMEX/上海金）" '{"codes":["hf_XAU"]}'
mk hotsearch "热搜榜" "抖音/知乎/B站热搜TopN" '{"platform":"douyin","limit":20}'
mk epicfree "Epic免费游戏" "Epic商城当前/即将免费游戏" '{}'
mk hitokoto "一言" "多分类随机句子：动画/文学/诗词/哲学等" '{"type":"诗词"}'
mk horoscope "星座运势" "十二星座今日运势评分与概述" '{"astro":"aries"}'
mk todayhistory "历史上的今天" "百度百科历史事件按年份排序" '{"date":"11-17"}'
mk qqavatar "QQ头像" "QQ号头像直链官方源" '{"qq":"10000"}'
mk bing "必应壁纸" "必应每日壁纸近N天" '{"days":7}'
mk randompic "随机图片" "风景/二次元随机图直链" '{"type":"scenery"}'

# ---- 9. 消费者密钥 ----
PIDS_JSON=$(echo "$PRODUCT_IDS" | tr ' ' '\n' | grep -v '^$' | sed 's/^/    /' | paste -sd, -)
KEY=$(curl -s -m 15 -X POST "$BASE/api-keys" -H "$AUTH" -H "$CT" -d "{\"name\":\"api-server自营联调\",\"product_ids\":[${PIDS_JSON:-}]}")
SECRET=$(echo "$KEY" | grep -o '"secret":"[^"]*"' | head -1 | sed 's/.*:"//;s/"$//')
echo "[consumer-key] ${SECRET:0:14}..."
if [ -n "$SECRET" ]; then
  echo "$SECRET" > /www/api-server/consumer_key.txt && chmod 600 /www/api-server/consumer_key.txt && chown apiserver:apiserver /www/api-server/consumer_key.txt 2>/dev/null
fi

# ---- 10. 网关端到端验证 ----
if [ -n "$SECRET" ]; then
  sleep 1
  R=$(curl -s -m 25 -X POST "http://127.0.0.1:8081/gateway/v1/goldprice/main" -H "X-API-Key: $SECRET" -H "$CT" -d '{"codes":["hf_XAU"]}')
  echo "[gateway-goldprice] $(echo "$R" | head -c 220)"
  R2=$(curl -s -m 25 -X POST "http://127.0.0.1:8081/gateway/v1/weather/main" -H "X-API-Key: $SECRET" -H "$CT" -d '{"city_code":"101010100"}')
  echo "[gateway-weather] $(echo "$R2" | head -c 220)"
else
  echo "[gateway] 无消费者密钥，跳过验证（可稍后在后台创建）"
fi
echo "=== 接入完成 ==="
