#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 1 ]; then
  echo "usage: run-ai-mr-comment-commit.sh <diff-path>" >&2
  exit 1
fi

diff_path="$1"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

if [ ! -f "${diff_path}" ]; then
  alt_repo="${repo_root}/${diff_path}"
  alt_evals="${repo_root}/evals/${diff_path#./}"
  if [ -f "${alt_repo}" ]; then
    diff_path="${alt_repo}"
  elif [ -f "${alt_evals}" ]; then
    diff_path="${alt_evals}"
  else
    echo "diff file not found: ${diff_path}" >&2
    exit 1
  fi
fi

bin="${AMC_BIN:-${repo_root}/dist/ai-mr-comment}"
provider="${AMC_EVAL_PROVIDER:-ollama}"
model="${AMC_EVAL_MODEL:-llama3.2:1b}"
extra_flags="${AMC_EVAL_FLAGS:-}"

if [ -x "${bin}" ]; then
  cmd=("${bin}")
else
  cmd=("go" "run" "${repo_root}")
fi

if [ -n "${extra_flags}" ]; then
  # shellcheck disable=SC2206 # intentional splitting for AMC_EVAL_FLAGS
  extra=(${extra_flags})
  "${cmd[@]}" \
    --provider "${provider}" \
    --model "${model}" \
    --file "${diff_path}" \
    --commit-msg \
    --format json \
    "${extra[@]}"
else
  "${cmd[@]}" \
    --provider "${provider}" \
    --model "${model}" \
    --file "${diff_path}" \
    --commit-msg \
    --format json
fi
