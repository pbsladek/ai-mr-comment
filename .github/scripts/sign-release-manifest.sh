#!/bin/bash
set -euo pipefail
cosign sign-blob \
  --yes \
  --bundle "dist/release-manifest.json.bundle" \
  "dist/release-manifest.json"
