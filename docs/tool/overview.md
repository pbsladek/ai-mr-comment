# ai-mr-comment Tool Documentation

This documentation lives under `docs/tool/` so it does not replace or interfere
with the GitHub Pages entrypoint at `docs/index.md`.

## What the Tool Does

`ai-mr-comment` reviews code changes and generates merge request or pull request
content using an AI provider. It can work from local git diffs, staged changes,
commit ranges, diff files, standard input, GitHub pull request URLs, and GitLab
merge request URLs.

Common use cases:

- Generate a review comment for a local branch.
- Generate a PR/MR title and description.
- Review a remote GitHub PR or GitLab MR without checking out the branch.
- Post a managed comment back to GitHub or GitLab.
- Update remote PR/MR title and description.
- Gate CI by returning a non-zero exit code for critical findings.
- Generate conventional commit messages and perform the commit/push workflow.
- Generate changelog or release-note content from a diff.

## Documentation Files

- [installation.md](installation.md): installation, Docker, and release artifacts.
- [configuration.md](configuration.md): config files, profiles, and providers.
- [commands.md](commands.md): root command, `publish`, `quick-commit`, and
  `changelog`.
- [remote-pr-mr.md](remote-pr-mr.md): GitHub and GitLab PR/MR workflows.
- [automation.md](automation.md): CI, JSON output, pipes, streams, and exit
  codes.
- [troubleshooting.md](troubleshooting.md): common failures and diagnostics.

