#!/usr/bin/env bash
set -euo pipefail

ROOT="${TASKFERRY_RELAY_DEPLOY_DIR:-/opt/TaskFerry/deploy/official-relay}"
BACKUP_DIR="${TASKFERRY_RELAY_BACKUP_DIR:-$ROOT/backups}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$BACKUP_DIR/taskferry-relay-$STAMP.tgz"

cd "$ROOT"
mkdir -p "$BACKUP_DIR"

echo "Stopping relay for a consistent SQLite backup..."
docker compose stop relay
restore() {
  docker compose up -d relay >/dev/null
}
trap restore EXIT

tar -czf "$OUT" data
sha256sum "$OUT" > "$OUT.sha256"

echo "Backup written:"
echo "$OUT"
echo "$OUT.sha256"

