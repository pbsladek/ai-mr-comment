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

All release archives are signed with [cosign](https://github.com/sigstore/cosign) using keyless signing via Sigstore.

To verify a release artifact:

```sh
cosign verify-blob \
  --certificate ai-mr-comment_Linux_x86_64.tar.gz.pem \
  --signature ai-mr-comment_Linux_x86_64.tar.gz.sig \
  --certificate-identity-regexp="https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ai-mr-comment_Linux_x86_64.tar.gz
```
