#!/bin/bash
# Usage: compute-docker-tags.sh <TAG> <IMAGE> [SUFFIX]
# Writes a "tags=..." line to GITHUB_OUTPUT.
# SUFFIX is optional (e.g. "-fips") and is appended to each tag name.
set -euo pipefail
TAG="$1"
IMAGE="$2"
SUFFIX="${3:-}"
if [[ "${TAG}" == *-* ]]; then
  echo "tags=${IMAGE}:${TAG}${SUFFIX}" >> "${GITHUB_OUTPUT}"
else
  echo "tags=${IMAGE}:${TAG}${SUFFIX},${IMAGE}:latest${SUFFIX}" >> "${GITHUB_OUTPUT}"
fi
