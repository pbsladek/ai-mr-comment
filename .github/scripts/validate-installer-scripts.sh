#!/usr/bin/env bash
set -euo pipefail

bash -n scripts/install.sh
bash -n scripts/bootstrap-install.sh
bash -n scripts/generate-installer-manifest.sh
bash -n evals/providers/run-ai-mr-comment.sh
bash scripts/install_test.sh
bash scripts/manifest_test.sh
