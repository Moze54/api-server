#!/usr/bin/env bash
# 交叉编译：Linux amd64（蓝队云服务器）+ 本机 Windows。
# 产物输出到 dist/，服务器部署取 api-server-linux-amd64。
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p dist

echo "==> 构建 linux/amd64 ..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" \
    -o dist/api-server-linux-amd64 ./cmd/server

echo "==> 构建本机可执行（本地冒烟测试用）..."
go build -trimpath -o dist/api-server.exe ./cmd/server

echo "==> 完成:"
ls -lh dist/
echo ""
echo "服务器部署提示:"
echo "  scp dist/api-server-linux-amd64 user@server:/opt/api-server/api-server"
echo "  config.yaml 与 deploy/api-server.service 的准备步骤见 README.md"
