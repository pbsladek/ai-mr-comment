# CI and Docker Examples

## Gate CI on Critical Review Findings

Use `--exit-code` in CI. The command exits with code `2` when the AI returns a
failing verdict.

```sh
ai-mr-comment \
  --exit-code \
  --format json \
  --pr "$PR_URL"
```

Post the review comment and gate the job in one step:

```sh
ai-mr-comment \
  --exit-code \
  --post \
  --pr "$PR_URL"
```

## Run from Docker in a Local Repository

Mount the repository and run the container in that working directory.

```sh
docker run --rm \
  -e OPENAI_API_KEY \
  -v "$PWD:/repo" \
  -w /repo \
  pwbsladek/ai-mr-comment:latest \
  --provider openai
```

Review a remote PR from Docker:

```sh
docker run --rm \
  -e OPENAI_API_KEY \
  -e GITHUB_TOKEN \
  pwbsladek/ai-mr-comment:latest \
  --provider openai \
  --pr https://github.com/owner/repo/pull/42
```

