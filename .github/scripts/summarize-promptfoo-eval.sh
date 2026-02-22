#!/usr/bin/env bash
set -euo pipefail

lane="${1:?lane is required}"
quality_outcome="${2:?quality eval outcome is required}"
result_file="${3:-evals/promptfoo-results.json}"
summary_file="/tmp/promptfoo-summary.txt"

if [ -f "${result_file}" ]; then
  jq -r '
    def eval_rows:
      if (.results | type) == "array" then .results
      elif (.results | type) == "object" and (.results.results | type) == "array" then .results.results
      else []
      end;
    eval_rows as $r |
    "Promptfoo summary: total=\($r|length) pass=\($r|map(objects | select(.success == true or (.gradingResult.pass // false) == true))|length) fail=\($r|map(objects | select((.success == false or (.gradingResult.pass // true) == false) and (.error == null) and (.failureReason != 2)) )|length) error=\($r|map(objects | select(.error != null or .failureReason == 2))|length)"
  ' "${result_file}" | tee "${summary_file}"

  {
    echo "### Promptfoo Eval Summary (${lane})"
    cat "${summary_file}"
    echo ""
    echo "Top failures/errors:"
    jq -r '
      def eval_rows:
        if (.results | type) == "array" then .results
        elif (.results | type) == "object" and (.results.results | type) == "array" then .results.results
        else []
        end;
      eval_rows[]
      | select(type == "object")
      | select(.error != null or .success == false or ((.gradingResult.pass // true) == false))
      | "- " + (.description // .testCase.description // "unnamed test") + " :: " + (((.error // .gradingResult.reason // (if .failureReason == 2 then "error" else "failed" end)) | tostring))
    ' "${result_file}" | head -n 15
  } >> "${GITHUB_STEP_SUMMARY}"
else
  echo "promptfoo results file not found at ${result_file}" | tee "${summary_file}"
  {
    echo "### Promptfoo Eval Summary (${lane})"
    cat "${summary_file}"
  } >> "${GITHUB_STEP_SUMMARY}"
fi

if [ "${quality_outcome}" != "success" ]; then
  echo "promptfoo eval failed; see summary above and ${result_file} in workspace" >&2
  exit 1
fi
