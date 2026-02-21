#!/usr/bin/env bash
set -euo pipefail

DEFAULT_REPO="pbsladek/ai-mr-comment"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

validate_repo() {
  local repo="$1"
  if [[ ! "${repo}" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    echo "Invalid --repo value: ${repo} (expected owner/repo)" >&2
    exit 1
  fi
}

validate_tag() {
  local tag="$1"
  if [[ ! "${tag}" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "Invalid --version value: ${tag}" >&2
    exit 1
  fi
}

download_file() {
  local url="$1"
  local out="$2"
  curl -fL --retry 3 --retry-delay 1 --proto '=https' --tlsv1.3 -o "${out}" "${url}"
}

resolve_latest_tag() {
  local repo="$1"
  local latest_url
  latest_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${repo}/releases/latest")"
  local tag="${latest_url##*/}"
  if [ -z "${tag}" ] || [ "${tag}" = "latest" ]; then
    echo "Failed to resolve latest release tag for ${repo}" >&2
    exit 1
  fi
  printf '%s\n' "${tag}"
}

resolve_tag_commit_sha() {
  local repo="$1"
  local tag="$2"
  local remote="https://github.com/${repo}.git"

  # refs/tags/<tag>^{} resolves annotated tags to the underlying commit.
  local peeled
  peeled="$(git ls-remote --refs --tags "${remote}" "refs/tags/${tag}^{}" | awk 'NR==1 {print $1}')"
  if [ -n "${peeled}" ]; then
    printf '%s\n' "${peeled}"
    return
  fi

  # Lightweight tags point directly at the commit.
  local direct
  direct="$(git ls-remote --refs --tags "${remote}" "refs/tags/${tag}" | awk 'NR==1 {print $1}')"
  if [ -z "${direct}" ]; then
    echo "Could not resolve tag ${tag} in ${repo}" >&2
    exit 1
  fi
  printf '%s\n' "${direct}"
}

extract_repo_and_version() {
  local repo="$DEFAULT_REPO"
  local version="latest"
  local args=("$@")
  local i=0
  while [ $i -lt ${#args[@]} ]; do
    case "${args[$i]}" in
      --repo)
        i=$((i + 1))
        if [ $i -ge ${#args[@]} ]; then
          echo "missing value for --repo" >&2
          exit 1
        fi
        repo="${args[$i]}"
        ;;
      --repo=*)
        repo="${args[$i]#--repo=}"
        ;;
      --version)
        i=$((i + 1))
        if [ $i -ge ${#args[@]} ]; then
          echo "missing value for --version" >&2
          exit 1
        fi
        version="${args[$i]}"
        ;;
      --version=*)
        version="${args[$i]#--version=}"
        ;;
    esac
    i=$((i + 1))
  done
  printf '%s\n%s\n' "${repo}" "${version}"
}

main() {
  require_cmd curl
  require_cmd git
  require_cmd awk
  require_cmd mktemp
  require_cmd chmod

  local extracted
  extracted="$(extract_repo_and_version "$@")"
  local repo="${extracted%%$'\n'*}"
  local requested_version="${extracted#*$'\n'}"
  validate_repo "${repo}"
  local resolved_version="${requested_version}"
  if [ "${requested_version}" = "latest" ]; then
    resolved_version="$(resolve_latest_tag "${repo}")"
  fi
  validate_tag "${resolved_version}"

  local installer_sha
  installer_sha="$(resolve_tag_commit_sha "${repo}" "${resolved_version}")"
  local installer_url="https://raw.githubusercontent.com/${repo}/${installer_sha}/scripts/install.sh"
  local installer_path
  installer_path="$(mktemp "${TMPDIR:-/tmp}/ai-mr-comment-install.XXXXXX.sh")"
  trap 'rm -f "${installer_path}"' EXIT

  echo "Downloading pinned installer from ${repo}@${installer_sha}"
  download_file "${installer_url}" "${installer_path}"
  chmod +x "${installer_path}"

  local forward_args=()
  local saw_version="false"
  local args=("$@")
  local i=0
  while [ $i -lt ${#args[@]} ]; do
    case "${args[$i]}" in
      --version)
        saw_version="true"
        i=$((i + 1))
        if [ $i -ge ${#args[@]} ]; then
          echo "missing value for --version" >&2
          exit 1
        fi
        if [ "${args[$i]}" = "latest" ]; then
          forward_args+=("--version" "${resolved_version}")
        else
          forward_args+=("--version" "${args[$i]}")
        fi
        ;;
      --version=*)
        saw_version="true"
        local v="${args[$i]#--version=}"
        if [ "${v}" = "latest" ]; then
          forward_args+=("--version=${resolved_version}")
        else
          forward_args+=("${args[$i]}")
        fi
        ;;
      *)
        forward_args+=("${args[$i]}")
        ;;
    esac
    i=$((i + 1))
  done
  if [ "${saw_version}" != "true" ]; then
    forward_args+=("--version" "${resolved_version}")
  fi

  "${installer_path}" "${forward_args[@]}"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
