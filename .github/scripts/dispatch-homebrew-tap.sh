#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
source_url="${2:?source url is required}"
source_sha256="${3:?source sha256 is required}"
commit="${4:-}"
commit_full="${5:-}"

if [ -z "${GH_TOKEN:-}" ]; then
  echo "HOMEBREW_TAP_DISPATCH_TOKEN is not set; skipping homebrew-tap dispatch."
  exit 0
fi

gh api repos/pbsladek/homebrew-tap/dispatches \
  -f 'client_payload[formula]=ai-mr-comment' \
  -f event_type=upstream_release \
  -f "client_payload[version]=${tag}" \
  -f "client_payload[url]=${source_url}" \
  -f "client_payload[sha256]=${source_sha256}" \
  -f "client_payload[commit]=${commit}" \
  -f "client_payload[commit_full]=${commit_full}"
