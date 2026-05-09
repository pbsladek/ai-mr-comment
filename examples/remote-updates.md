# Remote Update Examples

## Update a Remote PR/MR Title and Description

Generate a title and description from the diff and write them back to the remote
pull request or merge request.

```sh
ai-mr-comment \
  --pr https://github.com/owner/repo/pull/42 \
  --update-title \
  --update-description
```

Preview the remote writes first:

```sh
ai-mr-comment \
  --pr https://github.com/owner/repo/pull/42 \
  --update-title \
  --update-description \
  --dry-run
```

## Publish a One-Shot PR/MR Update

Use `publish` when you want the tool to handle title, description, managed
summary comment, labels, and reviewers.

```sh
ai-mr-comment publish \
  --pr https://github.com/owner/repo/pull/42 \
  --auto-labels \
  --label needs-review \
  --reviewer octocat
```

For GitLab, reviewers are numeric user IDs:

```sh
ai-mr-comment publish \
  --pr https://gitlab.com/group/project/-/merge_requests/5 \
  --auto-labels \
  --reviewer 12345
```

