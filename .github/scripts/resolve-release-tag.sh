#!/usr/bin/env bash
set -euo pipefail

input_tag="${1:-}"
output_file="${2:?output file is required}"

if [ -n "${input_tag}" ]; then
  tag="${input_tag}"
else
  tag="$(git tag --points-at HEAD --list 'v*' | sort -V | tail -n 1 || true)"
fi

if [ -z "${tag}" ]; then
  echo "No release tag found on HEAD; skipping release job."
  {
    echo "skip=true"
    echo "tag="
  } >> "${output_file}"
  exit 0
fi

{
  echo "skip=false"
  echo "tag=${tag}"
} >> "${output_file}"
