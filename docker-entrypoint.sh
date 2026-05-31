#!/bin/sh
# docker-entrypoint.sh
# 볼륨(/data)이 비어있을 때 시드 데이터를 복사합니다.
# 이미 파일이 존재하면 덮어쓰지 않으므로 기존 데이터를 보호합니다.

set -e

DATA_DIR="${DATA_DIR:-/data}"
SEED_DIR="/app/data-seed"

mkdir -p "$DATA_DIR"

for f in "$SEED_DIR"/*.json; do
  fname="$(basename "$f")"
  dest="$DATA_DIR/$fname"
  if [ ! -f "$dest" ]; then
    echo "[entrypoint] 초기 데이터 복사: $fname → $dest"
    cp "$f" "$dest"
  fi
done

exec "$@"
