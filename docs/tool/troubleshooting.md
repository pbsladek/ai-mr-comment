# Troubleshooting

## Provider Authentication

```sh
ai-mr-comment check --provider openai
ai-mr-comment doctor --provider openai
```

API-backed providers require their normal API key environment variables. Local
CLI providers require the provider executable to be installed and authenticated
before `ai-mr-comment` calls it.

## No Diff Found

- Confirm the branch has changes relative to the expected base.
- Use `--staged` for staged-only reviews.
- Use `--commit "base..HEAD"` when the automatic merge base is not the desired
  comparison.
- Use `--file=-` when piping a diff through standard input.

## Remote PR/MR Failures

- Confirm the URL is a GitHub pull request or GitLab merge request URL.
- Confirm the token has read access for review and write access for `--post`,
  `--update-title`, `--update-description`, or `publish`.
- Use `--dry-run` to inspect planned writes.
- For self-hosted GitHub or GitLab, configure the appropriate host and token
  environment for that installation.

## CI Failures

- Exit code `2` means the tool completed and the AI verdict was `FAIL`.
- Exit code `3` means no diff/input was available.
- Exit code `4` means the command flags were invalid for the requested mode.

