#!/bin/sh
set -e

PUID="${PUID:-0}"
PGID="${PGID:-0}"

cd /app

# 修复挂载的 data 目录权限（root 执行），避免宿主目录对 UID 1000 只读导致
# SQLite 写入失败循环重启（issue #198）。chown 失败不阻断（如 read-only fs），
# 仅打印警告——后续若仍 readonly，Go 侧 wrapSQLitePathError 会给出修复指引。
chown -R octopus:octopus /app/data 2>/dev/null || \
  echo "Warning: failed to chown /app/data, may cause readonly database error" >&2

if [ "$PUID" != "0" ] || [ "$PGID" != "0" ]; then
    # 自定义 UID/GID：chown 整个 /app 并切换到指定用户运行
    chown -R "$PUID:$PGID" /app
    exec su-exec "$PUID:$PGID" ./octopus start
else
    # 默认：降权到 octopus(1000) 运行（主进程不以 root 运行）
    exec su-exec octopus:octopus ./octopus start
fi
