#!/usr/bin/env bash
set -euo pipefail

version="${1:?version is required}"
sha256="${2:?sha256 is required}"

archive="/tmp/ollama-linux-amd64.tar.zst"
url="https://github.com/ollama/ollama/releases/download/v${version}/ollama-linux-amd64.tar.zst"

curl -fsSL "${url}" -o "${archive}"
echo "${sha256}  ${archive}" | sha256sum -c -
sudo tar --zstd -xvf "${archive}" -C /usr
ollama --version
