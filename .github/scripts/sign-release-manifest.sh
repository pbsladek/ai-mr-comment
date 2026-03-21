#!/bin/bash
set -euo pipefail
cosign sign-blob \
  --yes \
  --output-signature "dist/release-manifest.json.sig" \
  --output-certificate "dist/release-manifest.json.pem" \
  "dist/release-manifest.json"
