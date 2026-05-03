# Automation

## JSON and Pipe Integration

Use JSON output when another tool or agent needs to consume the result:

```sh
ai-mr-comment --format json --output review.json --pr "$PR_URL"
```

Pipe a diff through standard input:

```sh
git diff | ai-mr-comment --file=-
```

Inspect the exact provider request without calling the provider:

```sh
git diff | ai-mr-comment --file=- --print-request
```

Stream events for long-running automation:

```sh
ai-mr-comment --stream=jsonl --pr "$PR_URL"
```

## CI

Basic GitHub Actions pattern:

```yaml
name: ai-mr-comment

on:
  pull_request:

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
      - name: Review PR
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          ai-mr-comment --exit-code --post --pr "${{ github.event.pull_request.html_url }}"
```

Use `--dry-run` while introducing automation to a repository:

```sh
ai-mr-comment publish --pr "$PR_URL" --dry-run --format json
```

## Exit Codes

Exit codes are stable for automation:

| Code | Meaning |
| --- | --- |
| `0` | Success or passing verdict |
| `1` | Tool/runtime error |
| `2` | AI verdict fail when `--exit-code` is enabled |
| `3` | No diff or no input |
| `4` | Invalid usage |

