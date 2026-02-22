#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
repo="${2:-pbsladek/ai-mr-comment}"

if [ -z "${GH_TOKEN:-}" ]; then
  echo "GH_TOKEN is required to verify release assets." >&2
  exit 1
fi

required_exact=(
  "checksums.txt"
  "installer-manifest.json"
  "installer-manifest.json.sig"
  "installer-manifest.json.pem"
)

max_attempts=6
sleep_seconds=5

for attempt in $(seq 1 "${max_attempts}"); do
  mapfile -t assets < <(gh release view "${tag}" --repo "${repo}" --json assets --jq '.assets[].name')

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

  archive_count=0
  for asset in "${assets[@]}"; do
    if [[ "${asset}" =~ ^ai-mr-comment_.*\.(tar\.gz|zip)$ ]]; then
      archive_count=$((archive_count + 1))
    fi
  done

  if [ "${archive_count}" -lt 4 ]; then
    missing+=("at least 4 build archives (found ${archive_count})")
  fi

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
