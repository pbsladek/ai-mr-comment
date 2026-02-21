#!/usr/bin/env bash
set -euo pipefail

APP_NAME="ai-mr-comment"
REPO="pbsladek/ai-mr-comment"
VERSION="latest"
INSTALL_DIR="/usr/local/lib/${APP_NAME}"
BIN_DIR="/usr/local/bin"
USE_SUDO="never"
VERIFY_SIGNATURE="true"
RUN_VERSION_CHECK="false"

usage() {
  cat <<'EOF'
Install ai-mr-comment from GitHub Releases (macOS/Linux).

Usage:
  install.sh [options]

Options:
  --repo <owner/repo>     GitHub repository (default: pbsladek/ai-mr-comment)
  --version <tag|latest>  Release tag like v1.2.3 or "latest" (default: latest)
  --install-dir <path>    Binary install directory (default: /usr/local/lib/ai-mr-comment)
  --bin-dir <path>        Symlink directory (default: /usr/local/bin)
  --sudo <always|never>   Privilege strategy for writes (default: never)
  --verify-signature <true|false>
                          Verify signed checksums.txt using cosign (default: true).
                          WARNING: false is unsafe; use only in trusted offline/internal mirrors.
  --run-version-check     Run installed binary with --version after install
  -h, --help              Show this help
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

download_file() {
  local url="$1"
  local out="$2"
  curl -fL --retry 3 --retry-delay 1 --proto '=https' --tlsv1.3 -o "${out}" "${url}"
}

verify_checksum() {
  local checksums_file="$1"
  local target_file="$2"
  local asset_name="$3"

  local expected
  expected="$(awk -v name="$asset_name" '$2 == name { print $1; exit }' "$checksums_file")"
  if [ -z "$expected" ]; then
    echo "Could not find checksum entry for $asset_name in checksums.txt" >&2
    exit 1
  fi

  local actual
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$target_file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$target_file" | awk '{print $1}')"
  else
    echo "Missing checksum tool: install sha256sum or shasum" >&2
    exit 1
  fi

  if [ "$expected" != "$actual" ]; then
    echo "Checksum verification failed for $asset_name" >&2
    echo "Expected: $expected" >&2
    echo "Actual:   $actual" >&2
    exit 1
  fi
}

cosign_verify_checksums() {
  local repo="$1"
  local version="$2"
  local checksums_path="$3"
  local checksums_cert_path="$4"
  local checksums_sig_path="$5"

  if [ "${version}" = "latest" ]; then
    local identity_regex="https://github.com/${repo}/.github/workflows/release.yml@refs/tags/.*"
    cosign verify-blob "${checksums_path}" \
      --certificate "${checksums_cert_path}" \
      --signature "${checksums_sig_path}" \
      --certificate-identity-regexp "${identity_regex}" \
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" >/dev/null
    return
  fi

  local exact_identity="https://github.com/${repo}/.github/workflows/release.yml@refs/tags/${version}"
  cosign verify-blob "${checksums_path}" \
    --certificate "${checksums_cert_path}" \
    --signature "${checksums_sig_path}" \
    --certificate-identity "${exact_identity}" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" >/dev/null
}

run_write() {
  if [ "${USE_SUDO}" = "always" ]; then
    sudo "$@"
    return
  fi
  "$@"
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "Linux" ;;
    Darwin) echo "Darwin" ;;
    *)
      echo "Unsupported OS for install.sh. Use install.ps1 on Windows." >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "x86_64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "Unsupported CPU architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --repo)
        REPO="${2:?missing value for --repo}"
        shift 2
        ;;
      --version)
        VERSION="${2:?missing value for --version}"
        shift 2
        ;;
      --install-dir)
        INSTALL_DIR="${2:?missing value for --install-dir}"
        shift 2
        ;;
      --bin-dir)
        BIN_DIR="${2:?missing value for --bin-dir}"
        shift 2
        ;;
      --sudo)
        USE_SUDO="${2:?missing value for --sudo}"
        if [ "${USE_SUDO}" != "always" ] && [ "${USE_SUDO}" != "never" ]; then
          echo "Invalid --sudo value: ${USE_SUDO} (expected always|never)" >&2
          exit 1
        fi
        shift 2
        ;;
      --verify-signature)
        VERIFY_SIGNATURE="${2:?missing value for --verify-signature}"
        if [ "${VERIFY_SIGNATURE}" != "true" ] && [ "${VERIFY_SIGNATURE}" != "false" ]; then
          echo "Invalid --verify-signature value: ${VERIFY_SIGNATURE} (expected true|false)" >&2
          exit 1
        fi
        shift 2
        ;;
      --run-version-check)
        RUN_VERSION_CHECK="true"
        shift 1
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

main() {
  parse_args "$@"
  require_cmd curl
  require_cmd tar
  require_cmd mktemp
  require_cmd find
  require_cmd awk

  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="${APP_NAME}_${os}_${arch}.tar.gz"
  checksums_asset="checksums.txt"
  checksums_sig_asset="checksums.txt.sig"
  checksums_cert_asset="checksums.txt.pem"

  if [ "${VERSION}" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
    checksums_url="https://github.com/${REPO}/releases/latest/download/${checksums_asset}"
    checksums_sig_url="https://github.com/${REPO}/releases/latest/download/${checksums_sig_asset}"
    checksums_cert_url="https://github.com/${REPO}/releases/latest/download/${checksums_cert_asset}"
  else
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
    checksums_url="https://github.com/${REPO}/releases/download/${VERSION}/${checksums_asset}"
    checksums_sig_url="https://github.com/${REPO}/releases/download/${VERSION}/${checksums_sig_asset}"
    checksums_cert_url="https://github.com/${REPO}/releases/download/${VERSION}/${checksums_cert_asset}"
  fi

  tmp_root="$(mktemp -d)"
  archive_path="${tmp_root}/${asset}"
  checksums_path="${tmp_root}/${checksums_asset}"
  checksums_sig_path="${tmp_root}/${checksums_sig_asset}"
  checksums_cert_path="${tmp_root}/${checksums_cert_asset}"
  extract_dir="${tmp_root}/extract"
  mkdir -p "${extract_dir}"
  trap 'rm -rf "${tmp_root}"' EXIT

  if [ "${VERIFY_SIGNATURE}" = "true" ]; then
    require_cmd cosign
    echo "Downloading ${checksums_sig_url}"
    download_file "${checksums_sig_url}" "${checksums_sig_path}"
    echo "Downloading ${checksums_cert_url}"
    download_file "${checksums_cert_url}" "${checksums_cert_path}"
  fi

  echo "Downloading ${checksums_url}"
  download_file "${checksums_url}" "${checksums_path}"

  if [ "${VERIFY_SIGNATURE}" = "true" ]; then
    echo "Verifying signed checksums.txt"
    cosign_verify_checksums "${REPO}" "${VERSION}" "${checksums_path}" "${checksums_cert_path}" "${checksums_sig_path}"
  fi

  echo "Downloading ${url}"
  download_file "${url}" "${archive_path}"

  echo "Verifying checksum"
  verify_checksum "${checksums_path}" "${archive_path}" "${asset}"

  echo "Extracting ${asset}"
  tar -xzf "${archive_path}" -C "${extract_dir}"

  binary_path="$(find "${extract_dir}" -type f -name "${APP_NAME}" | head -n 1)"
  if [ -z "${binary_path}" ]; then
    echo "Failed to find ${APP_NAME} in archive ${asset}" >&2
    exit 1
  fi

  target_path="${INSTALL_DIR}/${APP_NAME}"
  link_path="${BIN_DIR}/${APP_NAME}"

  echo "Installing binary to ${target_path}"
  run_write mkdir -p "${INSTALL_DIR}"
  run_write install -m 0755 "${binary_path}" "${target_path}"

  echo "Creating symlink ${link_path} -> ${target_path}"
  run_write mkdir -p "${BIN_DIR}"
  run_write ln -sfn "${target_path}" "${link_path}"

  echo "Installed ${APP_NAME} successfully."
  if [ "${RUN_VERSION_CHECK}" = "true" ]; then
    "${link_path}" --version || true
  fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
