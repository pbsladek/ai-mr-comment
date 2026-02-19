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
- Customizable API endpoints and models
- Multiple prompt styles (Conventional, Technical, User-Focused)
- Configuration file support (`~/.ai-mr-comment.toml`)
- Environment variable configuration
- Outputs to console, a file (`--output`), or the system clipboard (`--clipboard=title|description|all`)
- Structured JSON output for scripting and CI (`--format json`) — always includes `title` and `description` fields
- Verbose debug logging to stderr (`--verbose`) — API timing, diff stats, config details
- Live streaming output to the terminal — tokens appear as they are generated
- Bootstrap a config file with `init-config` (never edit TOML by hand again)
- Shell completions for bash, zsh, fish, and PowerShell (`completion` subcommand)
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
# Options: default, conventional, technical, user-focused
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

# Output structured JSON (useful for CI/scripting)
# Always includes title, description, provider, and model fields
ai-mr-comment --format json

# Generate a title and comment together (shown as separate sections)
ai-mr-comment --title

# Generate title + comment as JSON (--format=json implies title generation)
ai-mr-comment --format json

# Copy the description to clipboard
ai-mr-comment --clipboard=description

# Copy the title to clipboard
ai-mr-comment --title --clipboard=title

# Copy title and description to clipboard
ai-mr-comment --title --clipboard=all

# Enable verbose debug logging to stderr (API timing, diff stats, config)
ai-mr-comment --verbose

# Show token and cost estimation without calling the API
ai-mr-comment --debug

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
- `--title`: Generate a concise MR/PR title alongside the comment; printed as a distinct `── Title ──` section in text mode. When `--format=json` is used, title is always generated automatically (no need for `--title`)
- `--file <FILE>`: Read diff from file instead of git
- `--output <FILE>`: Write output to file instead of stdout
- `--clipboard <WHAT>`: Copy to system clipboard — `title`, `description` (or `comment`), or `all` (title + description separated by a blank line)
- `--format <FORMAT>`: Output format — `text` (default) or `json`. JSON always includes `title`, `description`, `comment`, `provider`, and `model` fields
- `--provider <PROVIDER>`: Provider (openai, anthropic, gemini, ollama)
- `-t, --template <NAME>`: Template style (default, conventional, technical, user-focused)
- `--debug`: Show precise token usage and cost estimation without calling the generation API
- `--verbose`: Print detailed debug lines to stderr — config file path, diff stats, template source, streaming decision, and per-API-call timing and response size
- `-h, --help`: Print help

### Subcommands

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

`description` and `comment` carry the same value; `comment` is kept for backwards compatibility.

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

# Run fuzz tests (30s per target)
make test-fuzz

# Run linter
make lint
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

## License

MIT

## Acknowledgements

This project is a Go rewrite of [mr-comment](https://github.com/RobertKozak/mr-comment) originally created by [Robert Kozak](https://github.com/RobertKozak).