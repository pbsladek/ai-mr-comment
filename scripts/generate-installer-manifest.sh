#!/usr/bin/env bash
set -euo pipefail

REPO="pbsladek/ai-mr-comment"
TAG=""
COMMIT_SHA=""
OUT_PATH="installer-manifest.json"
BASH_PATH="scripts/bootstrap-install.sh"
PS1_PATH="scripts/bootstrap-install.ps1"

usage() {
  cat <<'EOF'
Generate installer-manifest.json for release assets.

Usage:
  generate-installer-manifest.sh [options]

Options:
  --repo <owner/repo>      Repository slug (default: pbsladek/ai-mr-comment)
  --tag <tag>              Release tag (required, e.g. v0.6.0)
  --commit <sha>           Tag target commit SHA (required)
  --out <path>             Output file path (default: installer-manifest.json)
  --bash-path <path>       Bootstrap bash script path in repo (default: scripts/bootstrap-install.sh)
  --ps1-path <path>        Bootstrap PowerShell script path in repo (default: scripts/bootstrap-install.ps1)
  -h, --help               Show this help
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return
  fi
  echo "Missing checksum tool: install sha256sum or shasum" >&2
  exit 1
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --repo)
        REPO="${2:?missing value for --repo}"
        shift 2
        ;;
      --tag)
        TAG="${2:?missing value for --tag}"
        shift 2
        ;;
      --commit)
        COMMIT_SHA="${2:?missing value for --commit}"
        shift 2
        ;;
      --out)
        OUT_PATH="${2:?missing value for --out}"
        shift 2
        ;;
      --bash-path)
        BASH_PATH="${2:?missing value for --bash-path}"
        shift 2
        ;;
      --ps1-path)
        PS1_PATH="${2:?missing value for --ps1-path}"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "Unknown argument: $1" >&2
        usage
        exit 1
        ;;
    esac
  done
}

validate_inputs() {
  if [ -z "${TAG}" ]; then
    echo "--tag is required" >&2
    exit 1
  fi
  if [ -z "${COMMIT_SHA}" ]; then
    echo "--commit is required" >&2
    exit 1
  fi
  if [[ ! "${REPO}" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    echo "Invalid --repo value: ${REPO} (expected owner/repo)" >&2
    exit 1
  fi
  if [[ ! "${TAG}" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "Invalid --tag value: ${TAG}" >&2
    exit 1
  fi
  if [[ ! "${COMMIT_SHA}" =~ ^[0-9a-fA-F]{40}$ ]]; then
    echo "Invalid --commit value: ${COMMIT_SHA} (expected 40-hex commit SHA)" >&2
    exit 1
  fi
  if [ ! -f "${BASH_PATH}" ]; then
    echo "Bootstrap bash script not found: ${BASH_PATH}" >&2
    exit 1
  fi
  if [ ! -f "${PS1_PATH}" ]; then
    echo "Bootstrap PowerShell script not found: ${PS1_PATH}" >&2
    exit 1
  fi
}

main() {
  parse_args "$@"
  require_cmd awk
  require_cmd date
  validate_inputs

  local bash_sha ps1_sha generated_at bash_url ps1_url
  bash_sha="$(sha256_file "${BASH_PATH}")"
  ps1_sha="$(sha256_file "${PS1_PATH}")"
  generated_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  bash_url="https://raw.githubusercontent.com/${REPO}/${COMMIT_SHA}/${BASH_PATH}"
  ps1_url="https://raw.githubusercontent.com/${REPO}/${COMMIT_SHA}/${PS1_PATH}"

  cat > "${OUT_PATH}" <<EOF
{
  "schema_version": 1,
  "repository": "${REPO}",
  "tag": "${TAG}",
  "commit_sha": "${COMMIT_SHA}",
  "generated_at_utc": "${generated_at}",
  "bootstrap": {
    "commit_sha": "${COMMIT_SHA}",
    "bash": {
      "path": "${BASH_PATH}",
      "url": "${bash_url}",
      "sha256": "${bash_sha}"
    },
    "powershell": {
      "path": "${PS1_PATH}",
      "url": "${ps1_url}",
      "sha256": "${ps1_sha}"
    }
  }
}
EOF
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
