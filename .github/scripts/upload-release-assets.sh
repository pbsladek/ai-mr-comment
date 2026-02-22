#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
shift

if [ -z "${GH_TOKEN:-}" ]; then
  echo "GH_TOKEN is required to upload release assets." >&2
  exit 1
fi

if [ "$#" -eq 0 ]; then
  echo "No assets provided for upload; skipping."
  exit 0
fi

assets=()
for path in "$@"; do
  if [ -f "${path}" ]; then
    assets+=("${path}")
  else
    echo "Skipping missing release asset: ${path}"
  fi
done

if [ "${#assets[@]}" -eq 0 ]; then
  echo "No existing assets to upload; skipping."
  exit 0
fi

gh release upload "${tag}" "${assets[@]}" --clobber
