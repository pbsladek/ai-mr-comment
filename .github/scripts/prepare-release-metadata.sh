#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
output_file="${2:?output file is required}"

source_url="https://github.com/pbsladek/ai-mr-comment/archive/refs/tags/${tag}.tar.gz"
source_sha256="$(curl -fsSL "${source_url}" | shasum -a 256 | awk '{print $1}')"

{
  echo "tag=${tag}"
  echo "source_url=${source_url}"
  echo "source_sha256=${source_sha256}"
} >> "${output_file}"
