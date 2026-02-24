#!/usr/bin/env bash
set -euo pipefail

input_tag="${1:-}"
workflow_run_sha="${2:-}"
output_file="${3:?output file is required}"

# Determine which tag to use
if [ -n "${input_tag}" ]; then
  tag="${input_tag}"
elif [ -n "${workflow_run_sha}" ]; then
  # For workflow_run events, find tag on the commit from main branch
  tag="$(git tag --points-at "${workflow_run_sha}" --list 'v*' | sort -V | tail -n 1 || true)"
else
  # Fallback: find tag on current HEAD
  tag="$(git tag --points-at HEAD --list 'v*' | sort -V | tail -n 1 || true)"
fi

if [ -z "${tag}" ]; then
  echo "No release tag found; skipping release job."
  {
    echo "skip=true"
    echo "tag="
    echo "tag_commit="
    echo "tag_commit_short="
  } >> "${output_file}"
  exit 0
fi

# Validate that the tag exists and points to a commit on main branch
if ! git rev-parse --verify "refs/tags/${tag}" >/dev/null 2>&1; then
  echo "Error: Tag ${tag} does not exist" >&2
  exit 1
fi

tag_commit="$(git rev-list -n 1 "refs/tags/${tag}")"

# Verify the tag commit is an ancestor of main branch
if ! git merge-base --is-ancestor "${tag_commit}" HEAD; then
  echo "Error: Tag ${tag} (${tag_commit}) is not an ancestor of main branch" >&2
  exit 1
fi

tag_commit_short="${tag_commit:0:7}"

echo "Resolved and validated tag: ${tag} at commit ${tag_commit} (short: ${tag_commit_short})"

{
  echo "skip=false"
  echo "tag=${tag}"
  echo "tag_commit=${tag_commit}"
  echo "tag_commit_short=${tag_commit_short}"
} >> "${output_file}"
