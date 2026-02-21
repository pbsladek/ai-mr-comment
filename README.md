# MR Comment Generator (Go)

[![Go Test](https://github.com/pbsladek/ai-mr-comment/actions/workflows/test.yml/badge.svg)](https://github.com/pbsladek/ai-mr-comment/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/pbsladek/ai-mr-comment)](https://github.com/pbsladek/ai-mr-comment/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/pbsladek/ai-mr-comment)](https://goreportcard.com/report/github.com/pbsladek/ai-mr-comment)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A command-line tool written in Go that generates professional GitLab Merge Request (MR) or GitHub Pull Request (PR) comments based on git diffs using AI (OpenAI, Anthropic, Gemini, or Ollama).

## Features

- Reads git diffs from current repo or from a file
- Auto-detects branch diff against `origin/main` or `origin/master` when no flags are given
- Staged-only diff (`--staged`) for reviewing changes before committing
- Exclude files from the diff by glob pattern (`--exclude`)
- Smart chunking (`--smart-chunk`) for large diffs: summarizes each file, then synthesizes a final comment
- Optional MR/PR title generation (`--title`) alongside the comment, printed as a distinct section
- **Generate comments directly from a GitHub PR or GitLab MR URL** (`--pr`) — no local checkout needed
- Supports public **github.com**, **GitHub Enterprise**, public **gitlab.com**, and **self-hosted GitLab** instances
- Supports OpenAI, Anthropic (Claude), Google Gemini, and Ollama APIs
- Customizable API endpoints and models via `--model` flag or config
- Multiple prompt styles — `default`, `conventional`, `technical`, `user-focused`, `emoji`, `sassy`, `monday`, `jira`, `commit`, `commit-emoji`
- **Jira-aware template** (`--template=jira`) — extracts ticket key from branch name and places it first so Jira auto-links
- **Commit message generation** (`--commit-msg`) — outputs a single conventional-style line ready for `git commit -m`
- **`quick-commit` subcommand** — stages all changes, generates a commit message, commits, and pushes in one step
- **CI/CD gate** (`--exit-code`) — exits with code 2 when the AI flags critical issues, enabling pipeline enforcement
- **Auto-post comments** (`--post`) — publishes the generated comment directly to the GitHub PR or GitLab MR via API
- Configuration file support (`~/.ai-mr-comment.toml`)
- Environment variable configuration
- Outputs to console, a file (`--output`), or the system clipboard (`--clipboard=title|description|commit-msg|all`)
- `--output` writes JSON when `--format=json` is set — ideal for saving structured review artifacts
- Structured JSON output for scripting and CI (`--format json`)
- Verbose debug logging to stderr (`--verbose`) — API timing, diff stats, config details
- Live streaming output to the terminal — tokens appear as they are generated
- Bootstrap a config file with `init-config` (never edit TOML by hand again)
- Shell completions for bash, zsh, fish, and PowerShell (`completion` subcommand)
- **Shell aliases** (`gen-aliases`) — prints `amc` and `amc-*` convenience aliases ready to source into your shell profile
- **Changelog generation** (`changelog`) — produces a user-facing Keep a Changelog entry from a commit range, grouped by Added / Fixed / Breaking Changes etc.
- **Custom system prompt** (`--system-prompt`) — supply an ad-hoc prompt inline or from a file (`@path`), overriding the active template for a single run
- Precise token counting for Gemini and heuristic estimation for others
- Estimated cost calculation in debug mode
- Native binary with no runtime dependencies

## Installation

### Prerequisites

- Go (1.26+)
- Git
- API Key for your preferred provider (OpenAI, Anthropic, or Google Gemini)

### Building from source

```bash
# Clone the repository
git clone https://github.com/pbsladek/ai-mr-comment.git
cd ai-mr-comment

# Build
make build

# The binary will be available at ./dist/ai-mr-comment
# Build and run on current diff
make test-run
```

### Installing

Download the latest binary for your OS from the [Releases](https://github.com/pbsladek/ai-mr-comment/releases) page.

### One-line installers

Recommended secure path:
- Do not use `curl | bash`
- Do not use `/main/` for installer scripts
- Resolve bootstrap script SHA from a signed release manifest, then execute locally

Bootstrap prerequisites: `curl`, `git`, `jq`, and `cosign` on macOS/Linux; PowerShell + `cosign` on Windows.

macOS / Linux (pinned release, no pipe, signed manifest):

```bash
VERSION=v0.6.0
BASE_URL="https://github.com/pbsladek/ai-mr-comment/releases/download/${VERSION}"
curl -fsSLO "${BASE_URL}/installer-manifest.json"
curl -fsSLO "${BASE_URL}/installer-manifest.json.sig"
curl -fsSLO "${BASE_URL}/installer-manifest.json.pem"
cosign verify-blob installer-manifest.json \
  --certificate installer-manifest.json.pem \
  --signature installer-manifest.json.sig \
  --certificate-identity "https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" >/dev/null
BOOTSTRAP_SHA="$(jq -r '.bootstrap.commit_sha' installer-manifest.json)"
BOOTSTRAP_SHA256="$(jq -r '.bootstrap.bash.sha256' installer-manifest.json)"
curl -fsSLo install-bootstrap.sh "https://raw.githubusercontent.com/pbsladek/ai-mr-comment/${BOOTSTRAP_SHA}/scripts/bootstrap-install.sh"
if command -v sha256sum >/dev/null 2>&1; then
  echo "${BOOTSTRAP_SHA256}  install-bootstrap.sh" | sha256sum -c -
else
  echo "${BOOTSTRAP_SHA256}  install-bootstrap.sh" | shasum -a 256 -c -
fi
chmod +x install-bootstrap.sh
./install-bootstrap.sh --version "${VERSION}"
```

macOS / Linux (user-local, no sudo):

```bash
./install-bootstrap.sh --version "${VERSION}" --install-dir "$HOME/.local/lib/ai-mr-comment" --bin-dir "$HOME/.local/bin" --sudo never
```

Windows PowerShell (pinned release, no pipe, signed manifest):

```powershell
$Version = "v0.6.0"
$BaseUrl = "https://github.com/pbsladek/ai-mr-comment/releases/download/$Version"
iwr "$BaseUrl/installer-manifest.json" -OutFile installer-manifest.json -SslProtocol Tls13
iwr "$BaseUrl/installer-manifest.json.sig" -OutFile installer-manifest.json.sig -SslProtocol Tls13
iwr "$BaseUrl/installer-manifest.json.pem" -OutFile installer-manifest.json.pem -SslProtocol Tls13
cosign verify-blob installer-manifest.json --certificate installer-manifest.json.pem --signature installer-manifest.json.sig --certificate-identity "https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/$Version" --certificate-oidc-issuer "https://token.actions.githubusercontent.com" | Out-Null
$Manifest = Get-Content installer-manifest.json -Raw | ConvertFrom-Json
$BootstrapSha = $Manifest.bootstrap.commit_sha
$BootstrapSha256 = $Manifest.bootstrap.powershell.sha256
iwr "https://raw.githubusercontent.com/pbsladek/ai-mr-comment/$BootstrapSha/scripts/bootstrap-install.ps1" -OutFile install-bootstrap.ps1 -SslProtocol Tls13
$Actual = (Get-FileHash -Path install-bootstrap.ps1 -Algorithm SHA256).Hash.ToLowerInvariant()
if ($Actual -ne $BootstrapSha256.ToLowerInvariant()) { throw "bootstrap script checksum mismatch" }
.\install-bootstrap.ps1 -Version $Version
```

Default install mode is least privilege (`--sudo never`).

Both installers:
- Use a signed `installer-manifest.json` to resolve pinned bootstrap script SHA
- Verify the signed `checksums.txt` with `cosign` (default)
- Verify artifact SHA256 against `checksums.txt`
- Do not execute the installed binary unless explicitly requested

Install a specific release tag:

```bash
./install.sh --version v1.2.3
```

```powershell
.\install.ps1 -Version v1.2.3
```

Use a different repository (for forks or internal mirrors):

```bash
./install.sh --repo <owner>/<repo> --version <tag>
```

```powershell
.\install.ps1 -Repo <owner>/<repo>
```

If `cosign` is unavailable, you can disable signature verification (not recommended):

```bash
./install.sh --version v1.2.3 --verify-signature false
```

```powershell
.\install.ps1 -Version v1.2.3 -VerifySignature:$false
```

Disabling signature verification is unsafe. Use it only for trusted offline/internal mirrors.

### Docker

No Go toolchain required. The image includes git so all diff and commit commands work.

```bash
# Build the image
make docker-build

# Run against the current repo diff (mounts PWD, passes API keys from env)
make docker-run ARGS="--provider openai"

# Run quick-commit
make docker-quick-commit ARGS="--dry-run"

# Or use docker directly
docker run --rm -it \
  -v "$(pwd):/repo" -w /repo \
  -e OPENAI_API_KEY \
  ai-mr-comment --provider openai
```

**Mounting your config file:**
```bash
docker run --rm -it \
  -v "$(pwd):/repo" -w /repo \
  -v "$HOME/.ai-mr-comment.toml:/home/aiuser/.ai-mr-comment.toml:ro" \
  -e OPENAI_API_KEY \
  ai-mr-comment
```

**Fetching a PR/MR by URL (no repo mount needed):**
```bash
docker run --rm \
  -e OPENAI_API_KEY \
  -e GITHUB_TOKEN \
  ai-mr-comment --pr https://github.com/owner/repo/pull/42
```

> **Note:** `--clipboard` is not available inside a container. Use `--output` or `--format json` instead to capture the output.

## Configuration File

The tool looks for `.ai-mr-comment.toml` in your home directory or the current directory.

### Generating the config file

Run `init-config` to write a fully-commented template to `~/.ai-mr-comment.toml`:

```bash
ai-mr-comment init-config

# Write to a custom path instead
ai-mr-comment init-config --output ./ai-mr-comment.toml
```

The command refuses to overwrite an existing file. Remove the old file first if you want to regenerate it.

```toml
# Choose which provider to use: "openai", "anthropic", "gemini", or "ollama"
provider = "gemini"

# === Gemini Settings ===
gemini_api_key = "xxxx"
gemini_model = "gemini-2.5-flash"

# === OpenAI Settings ===
openai_api_key = "xxxx"
openai_model = "gpt-4o-mini"
openai_endpoint = "https://api.openai.com/v1/"

# === Anthropic Settings ===
anthropic_api_key = "xxxx"
anthropic_model = "claude-sonnet-4-5"
anthropic_endpoint = "https://api.anthropic.com"

# === Ollama Settings ===
ollama_model = "llama3"
ollama_endpoint = "http://localhost:11434/api/generate"

# === GitHub / GitHub Enterprise ===
github_token = "xxxx"       # or set GITHUB_TOKEN env var (required for private repos)
# github_base_url = ""      # set for GitHub Enterprise, e.g. https://github.mycompany.com

# === GitLab / Self-Hosted GitLab ===
gitlab_token = "xxxx"       # or set GITLAB_TOKEN env var (required for private projects)
# gitlab_base_url = ""      # set for self-hosted GitLab, e.g. https://gitlab.mycompany.com

# === Template Settings ===
# Options: default, conventional, technical, user-focused, emoji, sassy, monday,
#          jira, commit, commit-emoji
template = "default"
```

## Usage

```bash
# Generate comment for the full branch diff (auto-detects merge base with origin/main)
ai-mr-comment

# Generate comment for staged changes only
ai-mr-comment --staged

# Exclude generated or vendored files
ai-mr-comment --exclude "vendor/**" --exclude "*.sum"

# Use smart chunking for large diffs (summarizes per-file, then combines)
ai-mr-comment --smart-chunk

# Use a specific provider and template
ai-mr-comment --provider anthropic --template technical

# Override the model for a single run
ai-mr-comment --model gpt-4o

# Generate comment for a specific commit range
ai-mr-comment --commit "HEAD~3..HEAD"

# Generate a comment from a GitHub PR URL (no local checkout needed)
ai-mr-comment --pr https://github.com/owner/repo/pull/42

# Generate a comment from a GitLab MR URL
ai-mr-comment --pr https://gitlab.com/group/project/-/merge_requests/5

# Generate a comment from a GitHub Enterprise or self-hosted GitLab URL
# (set github_base_url / gitlab_base_url in config or env)
GITHUB_BASE_URL=https://github.mycompany.com \
  ai-mr-comment --pr https://github.mycompany.com/owner/repo/pull/42

GITLAB_BASE_URL=https://gitlab.mycompany.com \
  ai-mr-comment --pr https://gitlab.mycompany.com/group/project/-/merge_requests/5

# Generate a title and comment together (shown as separate sections)
ai-mr-comment --title

# Output structured JSON for CI/scripting
# Always includes title, description, provider, and model fields
ai-mr-comment --format json

# Generate only a git commit message (conventional commits style)
ai-mr-comment --commit-msg --staged

# Generate a commit message as JSON
ai-mr-comment --commit-msg --format json | jq -r '.commit_message'

# Copy the description to clipboard
ai-mr-comment --clipboard=description

# Copy the title to clipboard
ai-mr-comment --title --clipboard=title

# Copy a commit message to clipboard
ai-mr-comment --commit-msg --clipboard=commit-msg

# Gate CI on AI review — exits 2 if critical issues are detected
ai-mr-comment --exit-code --pr https://github.com/owner/repo/pull/42

# Generate and immediately post the comment back to the PR/MR
ai-mr-comment --pr https://github.com/owner/repo/pull/42 --post

# Save JSON review to a file (for artifact upload in CI)
ai-mr-comment --format json --output /tmp/review.json --pr https://github.com/owner/repo/pull/42

# Enable verbose debug logging to stderr (API timing, diff stats, config)
ai-mr-comment --verbose

# Show token and cost estimation without calling the API
ai-mr-comment --debug

# Override the system prompt for a single run (inline)
ai-mr-comment --system-prompt="Focus only on security vulnerabilities."

# Override the system prompt from a file
ai-mr-comment --system-prompt=@~/prompts/security-review.txt

# Generate a user-facing changelog entry from the last 10 commits
ai-mr-comment changelog --commit="HEAD~10..HEAD"

# Generate a changelog entry for a release range, output as JSON
ai-mr-comment changelog --commit="v1.2.0..HEAD" --format=json

# Print shell aliases (amc, amc-qc, amc-cl, …) and add to profile
ai-mr-comment gen-aliases >> ~/.zshrc

# Or evaluate on every shell start to always stay up to date
# eval "$(ai-mr-comment gen-aliases)"

# Generate shell completion script
ai-mr-comment completion bash >> ~/.bash_completion
ai-mr-comment completion zsh > ~/.zsh/completions/_ai-mr-comment

# Bootstrap a config file (writes ~/.ai-mr-comment.toml)
ai-mr-comment init-config
```

### Options

- `--pr <URL>`: GitHub PR or GitLab MR URL — fetches diff and metadata remotely, no local checkout needed. Works with `github.com`, GitHub Enterprise, `gitlab.com`, and self-hosted GitLab. Mutually exclusive with `--staged`, `--commit`, and `--file`.
- `--commit <COMMIT>`: Specific commit or range
- `--staged`: Diff staged changes only (`git diff --cached`); mutually exclusive with `--commit`
- `--exclude <PATTERN>`: Exclude files matching glob pattern (e.g. `vendor/**`, `*.sum`). Can be repeated.
- `--smart-chunk`: Split large diffs by file, summarize each, then synthesize a final comment
- `--title`: Generate a concise MR/PR title alongside the comment; printed as a distinct `── Title ──` section in text mode. When `--format=json` is used, title is always generated automatically (no need for `--title`). Mutually exclusive with `--commit-msg`.
- `--commit-msg`: Generate a single-line git commit message instead of a full MR/PR description. Output is clean text or `{"commit_message":"..."}` in JSON mode. Mutually exclusive with `--title`.
- `--exit-code`: Exit with code 2 when the AI detects critical issues (bugs, security vulnerabilities, data loss risks). Exit 0 = pass, exit 2 = AI-flagged fail, exit 1 = tool error. Mutually exclusive with `--commit-msg`.
- `--post`: Post the generated comment back to the GitHub PR or GitLab MR via API (requires `--pr`). Uses the same token as diff fetching.
- `--file <FILE>`: Read diff from file instead of git
- `--output <FILE>`: Write output to file instead of stdout. Writes JSON when `--format=json` is set; writes the commit message when `--commit-msg` is set.
- `--clipboard <WHAT>`: Copy to system clipboard — `title`, `description` (or `comment`), `commit-msg`, or `all` (title + description separated by a blank line)
- `--format <FORMAT>`: Output format — `text` (default) or `json`
- `--provider <PROVIDER>`: Provider (openai, anthropic, gemini, ollama)
- `--model <NAME>`: Override the model for this run (e.g. `gpt-4o`, `claude-opus-4-6`, `gemini-2.5-flash`)
- `-t, --template <NAME>`: Template style — `default`, `conventional`, `technical`, `user-focused`, `emoji`, `sassy`, `monday`, `jira`, `commit`, `commit-emoji`
- `--system-prompt <TEXT|@FILE>`: Override the system prompt for this run. Pass the prompt inline (`--system-prompt="Focus on security"`) or read it from a file with an `@` prefix (`--system-prompt=@review.txt`). Mutually exclusive with `--template`.
- `--debug`: Show precise token usage and cost estimation without calling the generation API
- `--verbose`: Print detailed debug lines to stderr — config file path, diff stats, template source, streaming decision, and per-API-call timing and response size
- `-h, --help`: Print help

### Subcommands

- `quick-commit [flags]`: Stage all changes, generate an AI commit message, commit, and push in one step. See [Quick Commit](#quick-commit) below.
- `changelog [flags]`: Generate a user-facing changelog entry from a commit range or diff file. See [Changelog](#changelog) below.
- `gen-aliases [--shell bash|zsh] [--output FILE]`: Print `amc` and `amc-*` shell aliases to stdout. See [Shell Aliases](#shell-aliases) below.
- `models [--provider <NAME>]`: List known model names for a provider.
- `init-config [--output <PATH>]`: Write a default config file to `~/.ai-mr-comment.toml` (or the given path). Refuses to overwrite an existing file.
- `completion [bash|zsh|fish|powershell]`: Print a shell completion script to stdout.

## Streaming Output

When running interactively (stdout is a TTY), the tool streams tokens directly to the terminal as the model generates them, so you see the comment appear word by word rather than waiting for the full response.

Streaming is automatically disabled and the output is fully buffered when:

- `--format json` is set (the full response is needed before encoding)
- `--smart-chunk` is set (multi-stage summarise + synthesise calls)
- `--output <file>` is set (writing to a file)
- stdout is not a TTY (piped output, CI, redirected to file)

If a streaming call fails mid-flight, the tool transparently falls back to a standard buffered request and outputs the full comment normally.

## GitHub & GitLab Integration (`--pr`)

Point `--pr` at any GitHub pull request or GitLab merge request URL to generate a comment without needing the repository checked out locally.

```bash
# GitHub (public)
ai-mr-comment --pr https://github.com/owner/repo/pull/42

# GitLab (public)
ai-mr-comment --pr https://gitlab.com/group/project/-/merge_requests/5
```

### Authentication

Public repos and projects work without a token (subject to API rate limits). For private repos set the token via environment variable or config file:

| Platform | Env var | Config key |
|---|---|---|
| GitHub / GitHub Enterprise | `GITHUB_TOKEN` | `github_token` |
| GitLab / Self-Hosted GitLab | `GITLAB_TOKEN` | `gitlab_token` |

### Self-Hosted Instances

For GitHub Enterprise or self-hosted GitLab, pass the instance base URL. The SDKs automatically append the correct API path (`/api/v3/` for GitHub, `/api/v4/` for GitLab).

**Environment variables:**
```bash
# GitHub Enterprise
GITHUB_BASE_URL=https://github.mycompany.com \
  ai-mr-comment --pr https://github.mycompany.com/owner/repo/pull/42

# Self-hosted GitLab
GITLAB_BASE_URL=https://gitlab.mycompany.com \
  ai-mr-comment --pr https://gitlab.mycompany.com/group/project/-/merge_requests/5
```

**Config file (`.ai-mr-comment.toml`):**
```toml
github_base_url = "https://github.mycompany.com"
gitlab_base_url = "https://gitlab.mycompany.com"
```

## Token & Cost Estimation

When running with the `--debug` flag, the tool provides a detailed breakdown of the expected usage:

- **Gemini**: Uses official SDK token counting (100% accurate).
- **OpenAI/Anthropic/Ollama**: Uses a conservative character-based heuristic (~3.5 chars per token).
- **Cost**: Calculates estimated input cost in USD based on current model pricing (Ollama is free).

## Templates

Select a template with `-t` / `--template`. All templates receive the branch name as context (useful for ticket key extraction).

| Name | Description |
|---|---|
| `default` | Professional description with Key Changes, Why, Checklist, Notes |
| `conventional` | Conventional Commits body with optional BREAKING CHANGE and Refs |
| `technical` | Deep technical focus — Implementation Details, Rationale, Testing Strategy |
| `user-focused` | Non-technical perspective — What's New, Why Important, Impact |
| `emoji` | Fun emoji-rich format |
| `sassy` | Dry-wit tone, technically accurate |
| `monday` | Casual "pre-coffee" tone |
| `jira` | Puts the Jira ticket key from the branch name first so Jira auto-links |
| `commit` | Single-line conventional commit message (useful with `--commit-msg`) |
| `commit-emoji` | Single-line gitmoji-style commit message (useful with `--commit-msg`) |

### Jira Integration

The `jira` template extracts the ticket key from your branch name (e.g. `feat/ABC-123-add-login` → `ABC-123`) and places it at the very start of the description so Jira's branch/commit linking picks it up automatically.

```bash
ai-mr-comment --template jira --staged
# Output starts with: "ABC-123 Add login endpoint\n\n## Key Changes..."
```

This works because the current branch name is automatically prepended to the diff context for all local git operations (not `--file` or `--pr`).

## Commit Messages

Use `--commit-msg` to generate a single conventional-style commit message instead of a full PR description.

```bash
# Print the message (text mode)
ai-mr-comment --commit-msg --staged

# JSON output — single key, pipeline-friendly
ai-mr-comment --commit-msg --format json
# {"commit_message":"feat(auth): add JWT refresh token support"}

# Pipe directly into git commit
git commit -m "$(ai-mr-comment --commit-msg --staged)"

# Copy to clipboard
ai-mr-comment --commit-msg --staged --clipboard=commit-msg

# Gitmoji style
ai-mr-comment --commit-msg --template commit-emoji --staged
```

`--commit-msg` and `--title` are mutually exclusive. In JSON mode, the response contains only `commit_message` (no `description` or `title` fields).

## Quick Commit

`quick-commit` is a one-command shortcut for the full stage → AI message → commit → push workflow.

```bash
# Full workflow
ai-mr-comment quick-commit

# Preview the generated message without touching git
ai-mr-comment quick-commit --dry-run

# Commit but skip the push
ai-mr-comment quick-commit --no-push

# Use a specific provider or model
ai-mr-comment quick-commit --provider anthropic --model claude-sonnet-4-5

# JSON output — only commit_message is printed, all status lines suppressed
ai-mr-comment quick-commit --format json
# {"commit_message":"feat: add login endpoint"}
```

Steps performed:
1. `git add .` — stages all changes
2. Reads the staged diff; prepends branch name for ticket key context
3. Calls AI with the conventional commits prompt
4. `git commit -m "<message>"`
5. `git push --set-upstream origin <branch>` — works for new branches too

| Flag | Description |
|---|---|
| `--dry-run` | Generate and print the message, skip all git operations |
| `--no-push` | Commit but skip the push |
| `--format json` | Output `{"commit_message":"..."}` only; suppress status lines |
| `--provider` | Override the AI provider |
| `--model` | Override the model |

## CI/CD Usage

Three flags are designed specifically for pipeline integration:

### `--exit-code` — Gate merges on AI review

Instruct the AI to output a `VERDICT: PASS` or `VERDICT: FAIL` line before its review. If FAIL, the process exits with code 2, failing your pipeline step.

```bash
# Fail the pipeline if the AI detects critical issues
ai-mr-comment --exit-code --pr "$PR_URL"
echo $?  # 0 = PASS, 2 = FAIL, 1 = tool error

# JSON includes the verdict field for downstream processing
ai-mr-comment --exit-code --format json --pr "$PR_URL" | jq .verdict
```

**GitHub Actions example:**
```yaml
- name: AI Code Review
  run: ai-mr-comment --exit-code --pr "${{ github.event.pull_request.html_url }}"
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  # Step fails (exit 2) if AI detects critical issues
```

### `--post` — Auto-post comments to PRs/MRs

After generating the comment, post it directly to the PR or MR via the GitHub/GitLab API. Uses the same token as diff fetching — no extra setup needed.

```bash
# Generate and post in one step
ai-mr-comment --pr "$PR_URL" --post

# Combine with exit-code: review, post, and gate in one command
ai-mr-comment --exit-code --post --pr "$PR_URL"
```

**GitHub Actions example:**
```yaml
- name: AI Review & Comment
  run: ai-mr-comment --post --pr "${{ github.event.pull_request.html_url }}"
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### `--output` with `--format json` — Save review artifacts

Write the full JSON review to a file. Useful for artifact upload, audit trails, or passing data between pipeline jobs.

```bash
# Write JSON to file
ai-mr-comment --format json --output review.json --pr "$PR_URL"

# Upload as artifact (GitHub Actions)
# - uses: actions/upload-artifact@v4
#   with: { name: ai-review, path: review.json }

# Read in a later job
cat review.json | jq -r '.description'
cat review.json | jq -r '.verdict'  # when --exit-code was used
```

When `--commit-msg` is set, `--output` writes the commit message (with a trailing newline) rather than JSON.

## Example Output

**Text mode (`--title`):**

```
── Title ────────────────────────────────

feat: Add user authentication system


── Description ──────────────────────────

## Key Changes

- Added user model with bcrypt password hashing
- Implemented JWT authentication middleware
- Created login and registration API endpoints
- Added comprehensive unit tests for auth logic

## Why These Changes

Provides a secure foundation for user identity, allowing protected access to API resources.

```

**JSON mode (`--format json`):**

```json
{
  "title": "feat: Add user authentication system",
  "description": "## Key Changes\n\n- Added user model...",
  "comment": "## Key Changes\n\n- Added user model...",
  "provider": "openai",
  "model": "gpt-4o-mini"
}
```

`description` and `comment` carry the same value; `comment` is kept for backwards compatibility. When `--exit-code` is set, a `"verdict": "PASS"` or `"verdict": "FAIL"` field is also included.

**Commit message mode (`--commit-msg --format json`):**

```json
{"commit_message":"feat(auth): add JWT refresh token support"}
```

## Custom System Prompt

Use `--system-prompt` to replace the active template prompt for a single run — no template file needed.

```bash
# Inline prompt
ai-mr-comment --system-prompt="Focus only on security vulnerabilities."

# Read from a file (@ prefix)
ai-mr-comment --system-prompt=@~/prompts/security-review.txt

# Works on the changelog subcommand too
ai-mr-comment changelog --commit="v1.2.0..HEAD" \
  --system-prompt="List only breaking changes in one sentence each."
```

`--system-prompt` and `--template` are mutually exclusive — use one or the other per run.

When `--exit-code` is also set, the verdict preamble is still prepended on top of the custom prompt (same behaviour as with templates).

## Changelog

`changelog` generates a user-facing changelog entry from any diff source. Output follows the [Keep a Changelog](https://keepachangelog.com/) format, grouped under the headings that apply: Added, Changed, Deprecated, Removed, Fixed, Security, Breaking Changes.

```bash
# Last 10 commits (Makefile default)
make changelog

# Specific release range
ai-mr-comment changelog --commit="v1.2.0..HEAD"

# From a diff file
ai-mr-comment changelog --file=my.diff --provider=anthropic

# JSON output for CI pipelines
ai-mr-comment changelog --commit="v1.2.0..HEAD" --format=json

# Write directly to a file
ai-mr-comment changelog --commit="v1.2.0..HEAD" --output=CHANGELOG.md

# Override the prompt for a custom format
ai-mr-comment changelog --commit="v1.2.0..HEAD" \
  --system-prompt="List only breaking changes."
```

| Flag | Description |
|---|---|
| `--commit` | Commit or commit range (e.g. `v1.2.0..HEAD`) |
| `--file` | Read diff from a file instead of git |
| `--format` | `text` (default) or `json` (`{"changelog":"...","provider":"...","model":"..."}`) |
| `--output` | Write output to a file instead of stdout |
| `--provider` | AI provider override |
| `--model` | Model override |
| `--system-prompt` | Override the changelog system prompt |

## Shell Aliases

`gen-aliases` prints a block of ready-to-source shell alias definitions.

```bash
# Append once to your profile
ai-mr-comment gen-aliases >> ~/.zshrc

# Or evaluate on every shell start (always up-to-date)
eval "$(ai-mr-comment gen-aliases)"

# Write directly to a file
ai-mr-comment gen-aliases --output=~/.bashrc

# Via make
make gen-aliases
```

Aliases defined:

| Alias | Expands to |
|---|---|
| `amc` | `ai-mr-comment` |
| `amc-review` | `ai-mr-comment` |
| `amc-staged` | `ai-mr-comment --staged` |
| `amc-commit` | `ai-mr-comment --commit-msg` |
| `amc-title` | `ai-mr-comment --title` |
| `amc-json` | `ai-mr-comment --format=json` |
| `amc-debug` | `ai-mr-comment --debug` |
| `amc-qc` | `ai-mr-comment quick-commit` |
| `amc-qc-dry` | `ai-mr-comment quick-commit --dry-run` |
| `amc-cl` | `ai-mr-comment changelog` |
| `amc-models` | `ai-mr-comment models` |
| `amc-init` | `ai-mr-comment init-config` |

| Flag | Description |
|---|---|
| `--shell` | `bash` (default) or `zsh` — both use identical alias syntax |
| `--output` | Also write aliases to this file |

## Development

### Project Structure

- `./`: Main Go source files (`main.go`, `api.go`, etc.)
- `templates/`: Markdown prompt templates
- `testdata/`: Sample git diffs for testing
- `dist/`: Compiled binaries (after build)

### Testing

```bash
# Run unit tests
make test

# Run integration tests (requires GEMINI_API_KEY)
make test-integration

# Run Ollama-only integration tests (CPU-friendly small model)
OLLAMA_ENDPOINT=http://127.0.0.1:11434/api/generate \
OLLAMA_MODEL=llama3.2:1b \
make test-integration-ollama

# Run fuzz tests (30s per target)
make test-fuzz

# Run response-quality evals on curated diff fixtures (promptfoo)
make eval-quality

# Run linter
make lint
```

CI coverage on PRs includes:
- Ollama `Qwen2.5 7b` full integration suite
- Ollama `8b` full integration suite
- Promptfoo quality evals on both `Qwen2.5 7b` and `8b` lanes (`make eval-quality`)

### Response Quality Evals

`make eval-quality` runs end-to-end evals against fixture diffs in `evals/` and scores the generated review for recall, severity coverage, actionability, and false positives.

Prerequisites:
- Node.js + `npx` (for `promptfoo`)
- A reachable model provider (defaults to local Ollama)
- Built binary (handled automatically by `make eval-quality`)

Useful overrides:
- `AMC_EVAL_PROVIDER` (default `ollama`)
- `AMC_EVAL_MODEL` (default `llama3.2:1b`)
- `AMC_EVAL_TEMPLATE` (default `technical`)
- `AMC_EVAL_FLAGS` (additional CLI flags)

```bash
# Example: run evals against local Ollama 1B model
AMC_EVAL_PROVIDER=ollama AMC_EVAL_MODEL=llama3.2:1b make eval-quality

# Open the latest promptfoo report UI
make eval-quality-view
```

### Shell Completions

```bash
# Install bash completions
make install-completion-bash

# Install zsh completions
make install-completion-zsh

# Or generate manually for any shell
ai-mr-comment completion [bash|zsh|fish|powershell]
```

## Contributing

### Commit Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/) to drive automatic semantic versioning. Every commit merged to `main` must follow the format:

```
<type>[optional scope]: <description>
```

| Type | Description | Version bump |
|---|---|---|
| `fix:` | Bug fix | patch (`0.0.x`) |
| `feat:` | New feature | minor (`0.x.0`) |
| `feat!:` or `BREAKING CHANGE:` footer | Breaking change | major (`x.0.0`) |
| `chore:`, `docs:`, `ci:`, `test:`, `refactor:`, `style:`, `perf:` | Non-functional | none |

**Examples:**
```
fix: handle empty diff gracefully
feat: add clipboard output support
feat!: redesign config file format
chore: update dependencies
```

### Release Process

Merging to `main` automatically creates a version tag (e.g. `v0.2.0`) based on the commits since the last release — no GitHub Release is created yet.

When ready to publish a release:
1. Go to **GitHub → Releases → Draft a new release**
2. Pick the pre-created tag from the dropdown
3. Click **Publish release** — GoReleaser builds and attaches signed binaries automatically
4. Release workflow also publishes signed installer metadata (`installer-manifest.json`, `.sig`, `.pem`)

## License

MIT

## Acknowledgements

This project is a Go rewrite of [mr-comment](https://github.com/RobertKozak/mr-comment) originally created by [Robert Kozak](https://github.com/RobertKozak).
