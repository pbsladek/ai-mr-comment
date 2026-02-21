#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=install.sh
source "${SCRIPT_DIR}/install.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

echo "hello" > "${tmp}/artifact.bin"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp}/artifact.bin" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "${tmp}/artifact.bin" | awk '{print $1}')"
fi
printf "%s  artifact.bin\n" "${actual}" > "${tmp}/checksums.txt"

# verify_checksum success path
verify_checksum "${tmp}/checksums.txt" "${tmp}/artifact.bin" "artifact.bin"

# verify_checksum mismatch path
printf "deadbeef  artifact.bin\n" > "${tmp}/checksums-bad.txt"
if bash -c "source '${SCRIPT_DIR}/install.sh'; verify_checksum '${tmp}/checksums-bad.txt' '${tmp}/artifact.bin' 'artifact.bin'" 2>/dev/null; then
  echo "expected verify_checksum mismatch to fail" >&2
  exit 1
fi

# cosign_verify_checksums should use exact identity for pinned versions.
tmpbin="${tmp}/bin"
mkdir -p "${tmpbin}"
cat > "${tmpbin}/cosign" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$COSIGN_ARGS_FILE"
exit 0
EOF
chmod +x "${tmpbin}/cosign"

COSIGN_ARGS_FILE="${tmp}/cosign-pinned.args" PATH="${tmpbin}:${PATH}" bash -c \
  "source '${SCRIPT_DIR}/install.sh'; cosign_verify_checksums 'pbsladek/ai-mr-comment' 'v0.6.0' '${tmp}/checksums.txt' '${tmp}/checksums.txt.pem' '${tmp}/checksums.txt.sig'"
if ! grep -q -- "--certificate-identity https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/v0.6.0" "${tmp}/cosign-pinned.args"; then
  echo "expected exact certificate identity for pinned version" >&2
  exit 1
fi
if grep -q -- "--certificate-identity-regexp" "${tmp}/cosign-pinned.args"; then
  echo "did not expect regex identity for pinned version" >&2
  exit 1
fi

# cosign_verify_checksums should use regex identity for latest.
COSIGN_ARGS_FILE="${tmp}/cosign-latest.args" PATH="${tmpbin}:${PATH}" bash -c \
  "source '${SCRIPT_DIR}/install.sh'; cosign_verify_checksums 'pbsladek/ai-mr-comment' 'latest' '${tmp}/checksums.txt' '${tmp}/checksums.txt.pem' '${tmp}/checksums.txt.sig'"
if ! grep -q -- "--certificate-identity-regexp https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/.*" "${tmp}/cosign-latest.args"; then
  echo "expected regex certificate identity for latest version" >&2
  exit 1
fi

# cosign verification failure should fail-closed.
cat > "${tmpbin}/cosign" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "${tmpbin}/cosign"
if PATH="${tmpbin}:${PATH}" bash -c "source '${SCRIPT_DIR}/install.sh'; cosign_verify_checksums 'pbsladek/ai-mr-comment' 'v0.6.0' '${tmp}/checksums.txt' '${tmp}/checksums.txt.pem' '${tmp}/checksums.txt.sig'" 2>/dev/null; then
  echo "expected cosign verification failure to fail-closed" >&2
  exit 1
fi

echo "install.sh security tests passed"
