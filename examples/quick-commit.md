# Quick Commit Examples

`quick-commit` stages all changes, generates a commit message, commits, and
pushes. Use `--dry-run` first when trying a new style or provider.

## 1. Preview the Commit Message

```sh
ai-mr-comment quick-commit --dry-run
```

Use this before committing when you want to inspect the generated subject.

## 2. Generate a Long Multi-Line Commit Message

Use `--long` for a subject plus a more detailed body:

```sh
ai-mr-comment quick-commit --long --dry-run
```

Request a larger body when the diff needs more context:

```sh
ai-mr-comment quick-commit --body-lines=32 --dry-run
```

When the message looks right, omit `--dry-run`:

```sh
ai-mr-comment quick-commit --long
```

## 3. Commit Locally Without Pushing

Use this when you want the AI-generated commit but prefer to push manually.

```sh
ai-mr-comment quick-commit --no-push
```

Combine with a long body for feature work:

```sh
ai-mr-comment quick-commit --long --no-push
```

## 4. Use a Specific Provider and Model

Override the configured provider for one commit:

```sh
ai-mr-comment quick-commit \
  --provider anthropic \
  --model claude-opus-4-6 \
  --dry-run
```

Use a named profile when the provider and model are already configured:

```sh
ai-mr-comment quick-commit --profile local --dry-run
```

## 5. Force the Commit Type and Scope

Use `--type` when the change category should not be inferred from the diff.

```sh
ai-mr-comment quick-commit --type=fix --dry-run
```

Use `--scope` to force the component name:

```sh
ai-mr-comment quick-commit --scope=api --dry-run
```

Combine both when the subject needs to match a release or changelog convention:

```sh
ai-mr-comment quick-commit --type=docs --scope=examples --dry-run
```

## 6. Edit the Generated Message Before Committing

`--edit` opens the generated message in `$GIT_EDITOR`, `$VISUAL`, or `$EDITOR`
before the commit is created.

```sh
ai-mr-comment quick-commit --edit
```

Preview and edit without committing:

```sh
ai-mr-comment quick-commit --edit --dry-run
```

## 7. Apply a Message Template

Use `--message-template` when you want a repeatable commit-message shape without
changing the global prompt template.

```sh
ai-mr-comment quick-commit --message-template=short --dry-run
```

Use `detailed` for a conventional subject plus a structured markdown body:

```sh
ai-mr-comment quick-commit --message-template=detailed --dry-run
```

Use `release` when the commit body should be useful as release-note source
material:

```sh
ai-mr-comment quick-commit --message-template=release --dry-run
```

Use `ticket` when the branch name contains a work item key and the body should
include issue context and verification notes:

```sh
ai-mr-comment quick-commit --message-template=ticket --dry-run
```

## 8. Control What Gets Staged

The default behavior stages tracked and untracked changes with `git add .`.
Use `--include-untracked` when you want that behavior to be explicit:

```sh
ai-mr-comment quick-commit --include-untracked --dry-run
```

Use `--tracked-only` when new untracked files should stay out of the commit:

```sh
ai-mr-comment quick-commit --tracked-only --no-push
```

## 9. Add a DCO Signoff

Use `--signoff` for repositories that require Developer Certificate of Origin
trailers.

```sh
ai-mr-comment quick-commit --signoff --dry-run
```

The trailer is built from local git config:

```sh
git config user.name
git config user.email
```

## 10. Generate a Conventional Breaking-Change Commit

Use this when the diff intentionally breaks compatibility and the commit subject
should communicate that clearly.

```sh
ai-mr-comment quick-commit --breaking --dry-run
```

For a longer breaking-change explanation:

```sh
ai-mr-comment quick-commit --breaking --long --dry-run
```

## 11. Include a Jira Ticket from the Branch Name

When the branch contains a ticket key such as `PROJ-123-add-login-timeout`,
`--jira` prefixes the generated commit message with that key.

```sh
ai-mr-comment quick-commit --jira --dry-run
```

## 12. Add a Type-Matched Gitmoji

Use `--emoji` or `--emoji-commit` to append a gitmoji based on the detected
conventional commit type.

```sh
ai-mr-comment quick-commit --emoji --dry-run
ai-mr-comment quick-commit --emoji-commit --dry-run
```

## 13. Generate a Free-Form Commit Message

Use `--no-conventional` when a repository does not follow conventional commits.

```sh
ai-mr-comment quick-commit --no-conventional --dry-run
```

## 14. Ask for Maximum Technical Precision

Use this for low-level code changes where exact functions, structs, files, or
APIs should be named in the body.

```sh
ai-mr-comment quick-commit --technical --long --dry-run
```

## 15. Return Machine-Readable Output

Use JSON output when another tool needs to inspect the generated message.

```sh
ai-mr-comment quick-commit --format json --dry-run
```

## 16. Try Alternate Commit Styles

These are useful for repositories that allow more expressive commit messages or
for previewing different tones before selecting one.

```sh
ai-mr-comment quick-commit --haiku --dry-run
ai-mr-comment quick-commit --fortune --dry-run
ai-mr-comment quick-commit --monday --dry-run
ai-mr-comment quick-commit --sassy --dry-run
ai-mr-comment quick-commit --manager --dry-run
```

You can combine compatible style flags with `--long`:

```sh
ai-mr-comment quick-commit --fortune --long --dry-run
```

## 17. Full One-Shot Commit and Push

When the generated message does not need previewing, run the default workflow.

```sh
ai-mr-comment quick-commit
```
