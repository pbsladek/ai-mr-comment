#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
pattern="${2:?asset pattern is required}"
dest_dir="${3:?destination directory is required}"
repo="${4:-pbsladek/ai-mr-comment}"
max_attempts="${5:-6}"
sleep_seconds="${6:-5}"

if [ -z "${GH_TOKEN:-}" ]; then
  echo "GH_TOKEN is required to download release assets." >&2
  exit 1
fi

mkdir -p "${dest_dir}"

for attempt in $(seq 1 "${max_attempts}"); do
  if gh release download "${tag}" \
    --repo "${repo}" \
    --pattern "${pattern}" \
    --dir "${dest_dir}" \
    --clobber; then
    exit 0
  fi

  if [ "${attempt}" -eq "${max_attempts}" ]; then
    echo "Failed to download ${pattern} after ${max_attempts} attempts." >&2
    exit 1
  fi

  echo "${pattern} not available yet (attempt ${attempt}/${max_attempts}); retrying in ${sleep_seconds}s..."
  sleep "${sleep_seconds}"
done
