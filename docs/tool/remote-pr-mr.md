# GitHub and GitLab Integration

`--pr <url>` fetches a remote PR/MR diff and metadata directly from GitHub or
GitLab. It supports public GitHub/GitLab URLs and self-hosted GitHub Enterprise
or GitLab hosts.

```sh
ai-mr-comment --pr https://github.com/owner/repo/pull/42
ai-mr-comment --pr https://gitlab.com/group/project/-/merge_requests/5
```

`--pr` is mutually exclusive with local input flags such as `--staged`,
`--commit`, and `--file`.

Remote write actions require a token with the right API permissions:

- GitHub: a token that can read pull requests and write pull request comments or
  metadata.
- GitLab: a token that can read merge requests and write notes or metadata.

Review, update title, and update description in one command:

```sh
ai-mr-comment \
  --pr https://github.com/owner/repo/pull/42 \
  --update-title \
  --update-description
```

Review, post, and gate CI:

```sh
ai-mr-comment --exit-code --post --pr "$PR_URL"
```

Use `--dry-run` before remote writes when testing automation:

```sh
ai-mr-comment --pr "$PR_URL" --post --update-title --update-description --dry-run
```

