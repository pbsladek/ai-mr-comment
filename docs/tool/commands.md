# Commands

## Root Command

The root command generates review text, titles, descriptions, JSON output, or
automation verdicts.

Input examples:

```sh
ai-mr-comment
ai-mr-comment --staged
ai-mr-comment --commit "main..HEAD"
ai-mr-comment --file changes.diff
git diff | ai-mr-comment --file=-
ai-mr-comment --pr https://github.com/owner/repo/pull/42
ai-mr-comment --pr https://gitlab.com/group/project/-/merge_requests/5
```

Common flags:

| Flag | Purpose |
| --- | --- |
| `--provider <name>` | Override the configured provider |
| `--model <name>` | Override the configured model |
| `--profile <name>` | Use a named config profile |
| `--template <name>` | Use a built-in or configured prompt template |
| `--system-prompt <text>` | Override the review system prompt |
| `--format plain,json,markdown` | Select output format |
| `--output <path>` | Write output to a file |
| `--clipboard` | Copy output to the clipboard |
| `--stream=jsonl` | Stream progress/events as JSON lines |
| `--title` | Generate a title and description |
| `--title-only` | Generate only a title |
| `--summary-only` | Generate only a compact summary |
| `--changed-files` | Print changed file paths only |
| `--print-prompt` | Print the resolved prompt and exit |
| `--print-request` | Print the provider request JSON and exit |

Automation flags:

| Flag | Purpose |
| --- | --- |
| `--exit-code` | Exit with code 2 when the AI verdict is `FAIL` |
| `--verdict-only` | Print only the parsed verdict with `--exit-code` |
| `--post` | Post the generated comment to the remote PR/MR |
| `--update-title` | Update the remote PR/MR title |
| `--update-description` | Update the remote PR/MR body or description |
| `--dry-run` | Show intended remote actions without applying them |

## Publish

`publish` is the one-shot remote PR/MR workflow. It generates a title and
description, syncs managed remote metadata, preserves manual description text by
updating only the managed section, and creates or updates a managed summary
comment.

```sh
ai-mr-comment publish --pr https://github.com/owner/repo/pull/42
```

If `--pr` is omitted, `publish` uses the current branch and `origin` remote to
find or create the matching PR/MR when possible.

Useful flags:

| Flag | Purpose |
| --- | --- |
| `--pr <url>` | Target an existing GitHub PR or GitLab MR |
| `--no-update-title` | Do not update the remote title |
| `--no-update-description` | Do not update the remote body/description |
| `--replace-description` | Replace the full remote description instead of a managed section |
| `--post-summary` | Create or update the managed summary comment |
| `--auto-labels` | Generate labels from the diff |
| `--label <name>` | Add an explicit label |
| `--reviewer <id>` | Request a reviewer |
| `--draft-if-risky` | Prefix the title with `Draft:` when risk is detected |
| `--dry-run` | Show planned remote changes |
| `--format json` | Print machine-readable output |

GitHub reviewer values are usernames. GitLab reviewer values are numeric user
IDs.

## Quick Commit

`quick-commit` stages all changes, generates a commit message, commits, and
pushes in one command.

```sh
ai-mr-comment quick-commit
ai-mr-comment quick-commit --dry-run
ai-mr-comment quick-commit --long
ai-mr-comment quick-commit --body-lines=32
ai-mr-comment quick-commit --no-push
ai-mr-comment quick-commit --edit
ai-mr-comment quick-commit --type fix --scope api
ai-mr-comment quick-commit --message-template detailed
ai-mr-comment quick-commit --tracked-only
ai-mr-comment quick-commit --signoff
ai-mr-comment quick-commit --provider anthropic --model claude-opus-4-6
ai-mr-comment quick-commit --profile local
ai-mr-comment quick-commit --format json
```

Useful flags:

| Flag | Purpose |
| --- | --- |
| `--dry-run` | Generate the message without committing |
| `--no-push` | Commit locally but do not push |
| `--edit` | Open the generated commit message in `$GIT_EDITOR`, `$VISUAL`, or `$EDITOR` before committing |
| `--type <type>` | Force the conventional commit type |
| `--scope <scope>` | Force the conventional commit scope |
| `--message-template <name>` | Apply a template style: `short`, `detailed`, `release`, or `ticket` |
| `--include-untracked` | Explicitly stage tracked and untracked changes, matching the default behavior |
| `--tracked-only` | Stage only tracked modified/deleted files with `git add -u` |
| `--signoff` | Append a `Signed-off-by:` trailer from `git user.name` and `git user.email` |
| `--multi-line` | Generate subject plus body |
| `--long` | Generate a longer multi-line body |
| `--body-lines <n>` | Request a target body length |
| `--emoji` / `--emoji-commit` | Include a type-matched gitmoji |
| `--no-conventional` | Do not enforce conventional commit format |
| `--breaking` | Generate a breaking-change commit subject |
| `--jira` | Prefix with a Jira ticket inferred from the branch |
| `--technical` | Favor precise technical details |

Style flags:

| Flag | Purpose |
| --- | --- |
| `--chaos` | Generate an intentionally unusual but still accurate commit |
| `--haiku` | Generate the commit description as a haiku |
| `--roast` | Generate a blunt, critical commit style |
| `--fortune` | Add a short fortune-style trailer |
| `--monday` | Generate a casual low-energy commit style |
| `--sassy` | Generate a sassy but accurate commit |
| `--intern` | Generate an enthusiastic junior-dev style commit |
| `--shakespeare` | Generate an Early Modern English style commit |
| `--manager` | Generate a corporate status-report style commit |
| `--yoda` | Generate an inverted-syntax style commit |
| `--excuse` | Generate an accurate commit with built-in justification |

More quick-commit usage examples are available in
`examples/quick-commit.md`.

## Changelog

The `changelog` subcommand generates release notes or changelog text from a
commit range, file, or standard input.

```sh
ai-mr-comment changelog --commit "v1.2.0..HEAD"
ai-mr-comment changelog --file changes.diff --provider anthropic
git diff v1.2.0..HEAD | ai-mr-comment changelog --file=-
```

Use the `release-notes` preset for user-facing summaries:

```sh
ai-mr-comment changelog --preset release-notes --commit "v1.2.0..HEAD"
```
