#!/bin/bash
echo "--- 修复前文件尾部(cat -A 显示行尾符) ---"
tail -c 200 /www/apihub-prod/.env | cat -A
# 修复粘连：把紧贴在上一行值后的 GATEWAY 行断开
sed -i 's/\([^=]\)GATEWAY_ALLOW_PRIVATE_IP=true/\1\nGATEWAY_ALLOW_PRIVATE_IP=true/' /www/apihub-prod/.env
# 确保单独成行且值为 true
grep -q '^GATEWAY_ALLOW_PRIVATE_IP=true$' /www/apihub-prod/.env || echo 'GATEWAY_ALLOW_PRIVATE_IP=true' >> /www/apihub-prod/.env
sed -i 's/^GATEWAY_ALLOW_PRIVATE_IP=.*/GATEWAY_ALLOW_PRIVATE_IP=true/' /www/apihub-prod/.env
echo "--- 修复后尾部 ---"
tail -c 120 /www/apihub-prod/.env | cat -A
systemctl restart apihub-prod
sleep 3
echo -n "apihub-prod: "; systemctl is-active apihub-prod
P=$(pgrep -f '/www/apihub-prod/apiHub' | head -1)
echo -n "GATEWAY加载到进程: "
tr '\0' '\n' < /proc/$P/environ | grep -c '^GATEWAY_ALLOW_PRIVATE_IP=true'
SECRET=$(cat /www/api-server/consumer_key.txt)
echo "[gateway-goldprice]"
curl -s -m 25 -X POST http://127.0.0.1:8081/gateway/v1/goldprice/main \
  -H "X-API-Key: $SECRET" -H "Content-Type: application/json" \
  -d '{"codes":["hf_XAU"]}' | head -c 300
echo ""
echo "[gateway-weather]"
curl -s -m 25 -X POST http://127.0.0.1:8081/gateway/v1/weather/main \
  -H "X-API-Key: $SECRET" -H "Content-Type: application/json" \
  -d '{"city":"北京"}' | head -c 300
