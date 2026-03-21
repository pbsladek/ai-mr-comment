#!/usr/bin/env bash
set -euo pipefail

tag="${1:?tag is required}"
commit_sha="${2:?commit sha is required}"
source_url="${3:?source url is required}"
source_sha256="${4:?source sha256 is required}"
docker_image="${5:-}"
docker_tags_csv="${6:-}"
docker_digest="${7:-}"
output_path="${8:?output path is required}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return
  fi
  echo "Missing checksum tool: install sha256sum or shasum" >&2
  exit 1
}

require_cmd jq
require_cmd awk
require_cmd date

if [[ ! "${commit_sha}" =~ ^[0-9a-fA-F]{40}$ ]]; then
  echo "Invalid commit sha: ${commit_sha}" >&2
  exit 1
fi

if [[ ! "${source_sha256}" =~ ^[0-9a-fA-F]{64}$ ]]; then
  echo "Invalid source sha256: ${source_sha256}" >&2
  exit 1
fi

checksums_path="dist/checksums.txt"
if [ ! -f "${checksums_path}" ]; then
  echo "Missing checksums file: ${checksums_path}" >&2
  exit 1
fi

artifacts_from_checksums="$(
  jq -R -s '
    split("\n")
    | map(select(length > 0))
    | map(capture("^(?<sha>[0-9A-Fa-f]{64})\\s+\\*?(?<name>.+)$"))
    | map({name: .name, sha256: (.sha | ascii_downcase)})
    | sort_by(.name)
  ' "${checksums_path}"
)"

supplemental_candidates=(
  "dist/checksums.txt"
  "dist/checksums.txt.sig"
  "dist/checksums.txt.pem"
  "dist/provenance-binaries.intoto.jsonl"
  "dist/provenance-docker.intoto.jsonl"
  "dist/provenance-docker-fips.intoto.jsonl"
)

supplemental_artifacts='[]'
for path in "${supplemental_candidates[@]}"; do
  if [ ! -f "${path}" ]; then
    continue
  fi
  name="$(basename "${path}")"
  sha="$(sha256_file "${path}")"
  supplemental_artifacts="$(
    jq -cn \
      --argjson current "${supplemental_artifacts}" \
      --arg name "${name}" \
      --arg path "${path}" \
      --arg sha "${sha}" \
      '
      ($current + [{name: $name, path: $path, sha256: ($sha | ascii_downcase)}])
      | sort_by(.name)
      '
  )"
done

docker_tags_json="$(
  jq -n --arg tags "${docker_tags_csv}" '
    if ($tags | length) == 0 then
      []
    else
      ($tags | split(",") | map(gsub("^\\s+|\\s+$"; "") | select(length > 0)))
    end
  '
)"

docker_published=false
if [ -n "${docker_digest}" ]; then
  docker_published=true
fi

checksums_sha256="$(sha256_file "${checksums_path}")"
generated_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
mkdir -p "$(dirname "${output_path}")"

jq -n \
  --arg repository "pbsladek/ai-mr-comment" \
  --arg tag "${tag}" \
  --arg commit_sha "${commit_sha}" \
  --arg generated_at "${generated_at}" \
  --arg source_url "${source_url}" \
  --arg source_sha256 "${source_sha256}" \
  --arg checksums_file "${checksums_path}" \
  --arg checksums_sha256 "${checksums_sha256}" \
  --arg docker_image "${docker_image}" \
  --arg docker_digest "${docker_digest}" \
  --argjson docker_published "${docker_published}" \
  --argjson docker_tags "${docker_tags_json}" \
  --argjson artifacts_from_checksums "${artifacts_from_checksums}" \
  --argjson supplemental_artifacts "${supplemental_artifacts}" \
  '
  {
    schema_version: 1,
    repository: $repository,
    tag: $tag,
    commit_sha: ($commit_sha | ascii_downcase),
    generated_at_utc: $generated_at,
    source: {
      url: $source_url,
      sha256: ($source_sha256 | ascii_downcase)
    },
    artifacts: {
      checksums_file: $checksums_file,
      checksums_sha256: ($checksums_sha256 | ascii_downcase),
      from_checksums: $artifacts_from_checksums,
      supplemental: $supplemental_artifacts
    },
    docker: {
      published: $docker_published,
      image: $docker_image,
      tags: $docker_tags,
      digest: $docker_digest
    }
  }
  ' > "${output_path}"
