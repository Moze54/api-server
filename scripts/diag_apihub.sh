#!/bin/bash
systemctl is-active apihub-prod
P=$(pgrep -f '/www/apihub-prod/apiHub' | head -1)
echo "pid=$P"
echo -n "GATEWAY加载到进程: "
tr '\0' '\n' < /proc/$P/environ | grep -c '^GATEWAY_ALLOW_PRIVATE_IP=true'
echo -n "UPSTREAM_ENCRYPTION_KEY加载: "
tr '\0' '\n' < /proc/$P/environ | grep -c '^UPSTREAM_ENCRYPTION_KEY='
echo -n ".env中GATEWAY行: "
grep -c '^GATEWAY_ALLOW_PRIVATE_IP=true' /www/apihub-prod/.env
echo "--- 启动时间(是否在我改.env之后) ---"
ps -o lstart= -p $P
stat -c '%y' /www/apihub-prod/.env
