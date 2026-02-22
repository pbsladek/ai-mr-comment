#!/usr/bin/env bash
set -euo pipefail

previous_tag="${1:-}"
current_tag="$(git describe --tags --abbrev=0 2>/dev/null || true)"
repository="${GITHUB_REPOSITORY:-}"
ref_name="${GITHUB_REF_NAME:-main}"

if [ -z "${repository}" ]; then
  echo "GITHUB_REPOSITORY is not set; cannot dispatch release workflow." >&2
  exit 1
fi

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
start_ts="$(date -u +%s)"

gh api repos/"${repository}"/actions/workflows/release.yml/dispatches \
  -f ref="${ref_name}" \
  -f inputs[tag]="${current_tag}"

run_url=""
for _ in {1..12}; do
  run_url="$(
    gh api repos/"${repository}"/actions/workflows/release.yml/runs \
      -f event=workflow_dispatch \
      -f branch="${ref_name}" \
      -f per_page=20 \
      --jq ".workflow_runs[] | select((.created_at | fromdateiso8601) >= ${start_ts}) | .html_url" \
      | head -n 1 || true
  )"
  if [ -n "${run_url}" ]; then
    break
  fi
  sleep 5
done

if [ -z "${run_url}" ]; then
  echo "Dispatch accepted, but no new Release workflow run was detected on branch ${ref_name}." >&2
  echo "Check repository Actions settings and workflow permissions for workflow_dispatch." >&2
  exit 1
fi

echo "Release workflow queued: ${run_url}"
