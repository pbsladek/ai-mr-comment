# Security Policy

## Supported Versions

Only the latest release is actively supported with security fixes.

| Version | Supported |
| ------- | --------- |
| latest  | yes       |
| older   | no        |

## Reporting a Vulnerability

Please do not report security vulnerabilities through public GitHub issues.

Instead, open a [GitHub Security Advisory](https://github.com/pbsladek/ai-mr-comment/security/advisories/new) or email the maintainer directly.

## Security Measures

This repository uses the following automated security tooling:

- **CodeQL** — static analysis for code vulnerabilities
- **Gosec** — Go-specific security linting
- **Dependabot** — automated dependency updates
- **Dependency Review** — blocks PRs introducing high-severity CVEs
- **SBOM** — software bill of materials published with every release
- **Cosign** — keyless signing of release artifacts via Sigstore
- **OpenSSF Scorecard** — continuous supply chain security scoring

## Verifying Release Artifacts

Release archives are verified via a signed `checksums.txt` file.
`checksums.txt` is signed with [cosign](https://github.com/sigstore/cosign) using keyless Sigstore signing.

To verify release checksums and then an archive:

```sh
VERSION=v0.6.0
ASSET="ai-mr-comment_Linux_x86_64.tar.gz"
BASE_URL="https://github.com/pbsladek/ai-mr-comment/releases/download/${VERSION}"
curl -fsSLO "${BASE_URL}/checksums.txt"
curl -fsSLO "${BASE_URL}/checksums.txt.sig"
curl -fsSLO "${BASE_URL}/checksums.txt.pem"
curl -fsSLO "${BASE_URL}/${ASSET}"

cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity "https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt

grep "  ${ASSET}$" checksums.txt | sha256sum -c -
```

### Verifying installer-manifest.json

Each release also publishes a signed `installer-manifest.json` containing pinned
bootstrap installer script URLs and SHA256 hashes.

```sh
VERSION=v0.6.0
BASE_URL="https://github.com/pbsladek/ai-mr-comment/releases/download/${VERSION}"
curl -fsSLO "${BASE_URL}/installer-manifest.json"
curl -fsSLO "${BASE_URL}/installer-manifest.json.sig"
curl -fsSLO "${BASE_URL}/installer-manifest.json.pem"
cosign verify-blob installer-manifest.json \
  --certificate installer-manifest.json.pem \
  --signature installer-manifest.json.sig \
  --certificate-identity "https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
```
