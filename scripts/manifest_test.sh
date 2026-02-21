#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST_SCRIPT="${SCRIPT_DIR}/generate-installer-manifest.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

out="${tmp}/installer-manifest.json"
repo="pbsladek/ai-mr-comment"
tag="v0.6.0"
commit="0123456789abcdef0123456789abcdef01234567"

bash "${MANIFEST_SCRIPT}" --repo "${repo}" --tag "${tag}" --commit "${commit}" --out "${out}"

if ! command -v jq >/dev/null 2>&1; then
  echo "Missing required command: jq" >&2
  exit 1
fi

manifest_repo="$(jq -r '.repository' "${out}")"
manifest_tag="$(jq -r '.tag' "${out}")"
manifest_commit="$(jq -r '.commit_sha' "${out}")"
bootstrap_commit="$(jq -r '.bootstrap.commit_sha' "${out}")"
bash_url="$(jq -r '.bootstrap.bash.url' "${out}")"
ps1_url="$(jq -r '.bootstrap.powershell.url' "${out}")"
bash_sha="$(jq -r '.bootstrap.bash.sha256' "${out}")"
ps1_sha="$(jq -r '.bootstrap.powershell.sha256' "${out}")"

if [ "${manifest_repo}" != "${repo}" ]; then
  echo "unexpected repository in manifest: ${manifest_repo}" >&2
  exit 1
fi
if [ "${manifest_tag}" != "${tag}" ]; then
  echo "unexpected tag in manifest: ${manifest_tag}" >&2
  exit 1
fi
if [ "${manifest_commit}" != "${commit}" ]; then
  echo "unexpected commit_sha in manifest: ${manifest_commit}" >&2
  exit 1
fi
if [ "${bootstrap_commit}" != "${commit}" ]; then
  echo "unexpected bootstrap.commit_sha in manifest: ${bootstrap_commit}" >&2
  exit 1
fi
if [ "${bash_url}" != "https://raw.githubusercontent.com/${repo}/${commit}/scripts/bootstrap-install.sh" ]; then
  echo "unexpected bootstrap bash URL: ${bash_url}" >&2
  exit 1
fi
if [ "${ps1_url}" != "https://raw.githubusercontent.com/${repo}/${commit}/scripts/bootstrap-install.ps1" ]; then
  echo "unexpected bootstrap powershell URL: ${ps1_url}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  expected_bash_sha="$(sha256sum "${SCRIPT_DIR}/bootstrap-install.sh" | awk '{print $1}')"
  expected_ps1_sha="$(sha256sum "${SCRIPT_DIR}/bootstrap-install.ps1" | awk '{print $1}')"
else
  expected_bash_sha="$(shasum -a 256 "${SCRIPT_DIR}/bootstrap-install.sh" | awk '{print $1}')"
  expected_ps1_sha="$(shasum -a 256 "${SCRIPT_DIR}/bootstrap-install.ps1" | awk '{print $1}')"
fi

if [ "${bash_sha}" != "${expected_bash_sha}" ]; then
  echo "unexpected bootstrap bash sha256 in manifest" >&2
  exit 1
fi
if [ "${ps1_sha}" != "${expected_ps1_sha}" ]; then
  echo "unexpected bootstrap powershell sha256 in manifest" >&2
  exit 1
fi

echo "installer-manifest tests passed"
