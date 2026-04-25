#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
repo="${2:-pbsladek/ai-mr-comment}"
require_docker_provenance="${3:-false}"

if [ -z "${GH_TOKEN:-}" ]; then
  echo "GH_TOKEN is required to verify release assets." >&2
  exit 1
fi

required_exact=(
  "checksums.txt"
  "release-manifest.json"
  "release-manifest.json.bundle"
  "provenance-binaries.intoto.jsonl"
)

if [ "${require_docker_provenance}" = "true" ]; then
  required_exact+=("provenance-docker.intoto.jsonl")
  required_exact+=("provenance-docker-fips.intoto.jsonl")
fi

max_attempts=6
sleep_seconds=5

for attempt in $(seq 1 "${max_attempts}"); do
  if ! mapfile -t assets < <(gh release view "${tag}" --repo "${repo}" --json assets --jq '.assets[].name'); then
    if [ "${attempt}" -lt "${max_attempts}" ]; then
      echo "Attempt ${attempt}/${max_attempts}: release not ready yet; retrying in ${sleep_seconds}s..."
      sleep "${sleep_seconds}"
      continue
    fi
    echo "Failed to query release ${tag} in ${repo}." >&2
    exit 1
  fi

  missing=()
  for name in "${required_exact[@]}"; do
    found="false"
    for asset in "${assets[@]}"; do
      if [ "${asset}" = "${name}" ]; then
        found="true"
        break
      fi
    done
    if [ "${found}" != "true" ]; then
      missing+=("${name}")
    fi
  done

  required_archives=(
    "ai-mr-comment_Linux_x86_64.tar.gz"
    "ai-mr-comment_Linux_arm64.tar.gz"
    "ai-mr-comment_Darwin_x86_64.tar.gz"
    "ai-mr-comment_Darwin_arm64.tar.gz"
    "ai-mr-comment_Windows_x86_64.zip"
    "ai-mr-comment_Windows_arm64.zip"
  )

  for name in "${required_archives[@]}"; do
    found="false"
    for asset in "${assets[@]}"; do
      if [ "${asset}" = "${name}" ]; then
        found="true"
        break
      fi
    done
    if [ "${found}" != "true" ]; then
      missing+=("${name}")
    fi
  done

  if [ "${#missing[@]}" -eq 0 ]; then
    echo "Release ${tag} asset check passed."
    exit 0
  fi

  if [ "${attempt}" -lt "${max_attempts}" ]; then
    echo "Attempt ${attempt}/${max_attempts}: release assets not complete yet; retrying in ${sleep_seconds}s..."
    sleep "${sleep_seconds}"
  fi
done

echo "Release ${tag} is missing required assets:" >&2
for item in "${missing[@]}"; do
  echo " - ${item}" >&2
done
exit 1
