# Installation

Install the CLI from a release archive, from source, or by using Docker.

```sh
go install github.com/pbsladek/ai-mr-comment/cmd/ai-mr-comment@latest
```

Release archives are published for Linux, macOS, and Windows on x86_64 and arm64.
Container images are published as multi-arch Linux images for amd64 and arm64.

## Docker

Use Docker when the CLI should run without installing Go tooling locally:

```sh
docker run --rm \
  -e OPENAI_API_KEY \
  -v "$PWD:/repo" \
  -w /repo \
  pwbsladek/ai-mr-comment:latest \
  --provider openai
```

For remote PR/MR review:

```sh
docker run --rm \
  -e OPENAI_API_KEY \
  -e GITHUB_TOKEN \
  pwbsladek/ai-mr-comment:latest \
  --pr https://github.com/owner/repo/pull/42
```
