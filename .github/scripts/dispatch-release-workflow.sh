#!/usr/bin/env bash
set -euo pipefail

previous_tag="${1:-}"
current_tag="$(git describe --tags --abbrev=0 2>/dev/null || true)"

if [ -z "${current_tag}" ]; then
  echo "No tags found after semantic-release; skipping release workflow dispatch."
  exit 0
fi

if [ "${current_tag}" = "${previous_tag}" ]; then
  echo "No new tag created; skipping release workflow dispatch."
  exit 0
fi

if ! [[ "${current_tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)*$ ]]; then
  echo "Latest tag ${current_tag} is not a supported release tag format; skipping dispatch."
  exit 0
fi

echo "Dispatching release workflow for tag ${current_tag}"
gh api repos/"${GITHUB_REPOSITORY}"/actions/workflows/release.yml/dispatches \
  -f ref="${GITHUB_REF_NAME}" \
  -f inputs[tag]="${current_tag}"
