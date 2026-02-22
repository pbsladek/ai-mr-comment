#!/usr/bin/env bash
set -euo pipefail

log_file="${1:-/tmp/promptfoo-eval.log}"

if make eval-quality > "${log_file}" 2>&1; then
  echo "promptfoo eval completed successfully"
  exit 0
fi

status=$?
echo "promptfoo eval failed with exit code ${status}"
echo "Last 120 lines of eval log:"
tail -n 120 "${log_file}" || true
exit "${status}"
