#!/usr/bin/env bash
set -euo pipefail

log_file="${1:-/tmp/ollama.log}"
ready_url="${2:-http://127.0.0.1:11434/api/tags}"
timeout_seconds="${3:-60}"

nohup ollama serve > "${log_file}" 2>&1 &

for ((i=1; i<=timeout_seconds; i++)); do
  if curl -fsS "${ready_url}" >/dev/null; then
    exit 0
  fi
  sleep 1
done

echo "Ollama failed to start"
tail -n 200 "${log_file}" || true
exit 1
