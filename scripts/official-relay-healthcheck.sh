#!/usr/bin/env bash
set -euo pipefail

LOCAL_URL="${TASKFERRY_RELAY_LOCAL_HEALTH:-http://127.0.0.1:18080/health}"
PUBLIC_URL="${TASKFERRY_RELAY_PUBLIC_HEALTH:-}"
OPS_URL="${TASKFERRY_RELAY_OPS_URL:-}"
OPS_TOKEN="${TASKFERRY_OPS_TOKEN:-}"

curl -fsS "$LOCAL_URL" >/dev/null
echo "local health ok: $LOCAL_URL"

if [[ -n "$PUBLIC_URL" ]]; then
  curl -fsS "$PUBLIC_URL" >/dev/null
  echo "public health ok: $PUBLIC_URL"
fi

if [[ -n "$OPS_URL" && -n "$OPS_TOKEN" ]]; then
  curl -fsS -H "Authorization: Bearer $OPS_TOKEN" "$OPS_URL"
  echo
fi
