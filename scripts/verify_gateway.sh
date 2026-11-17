#!/bin/bash
echo -n "apihub-prod: "; systemctl is-active apihub-prod
P=$(pgrep -f '/www/apihub-prod/apiHub' | head -1)
echo -n "GATEWAY加载: "
tr '\0' '\n' < /proc/$P/environ | grep -c '^GATEWAY_ALLOW_PRIVATE_IP=true'
SECRET=$(cat /www/api-server/consumer_key.txt)
echo "[gateway-goldprice]"
curl -s -m 25 -X POST http://127.0.0.1:8081/gateway/v1/goldprice/main \
  -H "X-API-Key: $SECRET" -H "Content-Type: application/json" \
  -d '{"codes":["hf_XAU"]}' | head -c 260
echo ""
echo "[gateway-weather]"
curl -s -m 25 -X POST http://127.0.0.1:8081/gateway/v1/weather/main \
  -H "X-API-Key: $SECRET" -H "Content-Type: application/json" \
  -d '{"city":"北京"}' | head -c 260
echo ""
echo "[gateway-hitokoto]"
curl -s -m 25 -X POST http://127.0.0.1:8081/gateway/v1/hitokoto/main \
  -H "X-API-Key: $SECRET" -H "Content-Type: application/json" \
  -d '{"type":"诗词"}' | head -c 200
