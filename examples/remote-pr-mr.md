# Remote PR/MR Review Examples

## Review a GitHub Pull Request Without Checking It Out

Use `--pr` to fetch the diff and metadata directly from GitHub.

```sh
export GITHUB_TOKEN="..."
export OPENAI_API_KEY="..."

ai-mr-comment \
  --provider openai \
  --pr https://github.com/owner/repo/pull/42
```

This is useful for support rotations, release reviews, and CI jobs that should
not need a local branch checkout.

## Review a GitLab Merge Request Without Checking It Out

Use the same `--pr` flag for GitLab merge requests.

```sh
export GITLAB_TOKEN="..."
export OPENAI_API_KEY="..."

ai-mr-comment \
  --provider openai \
  --pr https://gitlab.com/group/project/-/merge_requests/5
```

Self-hosted GitLab URLs work the same way when the token has access to that
instance.

