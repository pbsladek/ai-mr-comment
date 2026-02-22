#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
require_docker_provenance="${2:-false}"

summary_file="${GITHUB_STEP_SUMMARY:-}"
if [ -z "${summary_file}" ]; then
  echo "GITHUB_STEP_SUMMARY is not set; skipping summary output."
  exit 0
fi

base_url="https://github.com/pbsladek/ai-mr-comment/releases/download/${tag}"

{
  echo "## Release Assets"
  echo ""
  echo "- Tag: \`${tag}\`"
  echo "- Immutable manifest: [release-manifest.json](${base_url}/release-manifest.json)"
  echo "- Manifest signature: [release-manifest.json.sig](${base_url}/release-manifest.json.sig)"
  echo "- Manifest certificate: [release-manifest.json.pem](${base_url}/release-manifest.json.pem)"
  echo "- Binary provenance: [provenance-binaries.intoto.jsonl](${base_url}/provenance-binaries.intoto.jsonl)"
  if [ "${require_docker_provenance}" = "true" ]; then
    echo "- Docker provenance: [provenance-docker.intoto.jsonl](${base_url}/provenance-docker.intoto.jsonl)"
    echo "- Docker FIPS provenance: [provenance-docker-fips.intoto.jsonl](${base_url}/provenance-docker-fips.intoto.jsonl)"
  else
    echo "- Docker provenance: not published (Docker credentials not configured)"
  fi
} >> "${summary_file}"
