#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
source_url="${2:?source url is required}"
source_sha256="${3:?source sha256 is required}"

gh api repos/pbsladek/homebrew-tap/dispatches \
  -f event_type=upstream_release \
  -f client_payload[formula]=ai-mr-comment \
  -f client_payload[version]="${tag}" \
  -f client_payload[url]="${source_url}" \
  -f client_payload[sha256]="${source_sha256}"
