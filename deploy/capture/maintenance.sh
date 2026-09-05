#!/bin/sh
# 仅记录聚合指标；不输出邀请码、凭证、请求正文或完整链接。
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$root"
mkdir -p metrics
case "${1:-stats}" in
  backup)
    docker compose exec -T capture node dist/backup.js backup
    ;;
  stats)
    file="metrics/$(date -u +%F).jsonl"
    docker compose exec -T capture node dist/admin.js stats >> "$file"
    find "$root/metrics" -maxdepth 1 -type f -name '????-??-??.jsonl' -mtime +7 -delete
    ;;
  *) printf '%s\n' '用法: maintenance.sh backup|stats' >&2; exit 2 ;;
esac
