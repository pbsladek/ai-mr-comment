#!/bin/bash
set -euo pipefail
if [ -z "${DOCKERHUB_USERNAME}" ] || [ -z "${DOCKERHUB_TOKEN}" ]; then
  echo "enabled=false" >> "${GITHUB_OUTPUT}"
  echo "Docker publish skipped: DOCKERHUB_USERNAME or DOCKERHUB_TOKEN is missing."
else
  echo "enabled=true" >> "${GITHUB_OUTPUT}"
fi
