#!/usr/bin/env bash
set -euo pipefail

bash -n scripts/install.sh
bash -n scripts/bootstrap-install.sh
bash -n scripts/generate-installer-manifest.sh
bash -n evals/providers/run-ai-mr-comment.sh
bash scripts/install_test.sh
bash scripts/manifest_test.sh

pwsh -NoProfile -Command '$parseTokens = $null; $parseErrors = $null; [void][System.Management.Automation.Language.Parser]::ParseFile("scripts/install.ps1",[ref]$parseTokens,[ref]$parseErrors); if ($null -ne $parseErrors -and $parseErrors.Count -gt 0) { $parseErrors; exit 1 }'
pwsh -NoProfile -Command '$parseTokens = $null; $parseErrors = $null; [void][System.Management.Automation.Language.Parser]::ParseFile("scripts/bootstrap-install.ps1",[ref]$parseTokens,[ref]$parseErrors); if ($null -ne $parseErrors -and $parseErrors.Count -gt 0) { $parseErrors; exit 1 }'
pwsh -NoProfile -File scripts/install_test.ps1
