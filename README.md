# MR Comment Generator (Go)

[![Go Test](https://github.com/pbsladek/ai-mr-comment/actions/workflows/test.yml/badge.svg)](https://github.com/pbsladek/ai-mr-comment/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/pbsladek/ai-mr-comment)](https://github.com/pbsladek/ai-mr-comment/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/pbsladek/ai-mr-comment)](https://goreportcard.com/report/github.com/pbsladek/ai-mr-comment)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A command-line tool written in Go that generates professional GitLab Merge Request (MR) or GitHub Pull Request (PR) comments based on git diffs using AI (OpenAI, Anthropic, Gemini, or Ollama).

## Features

- Reads git diffs from current repo or from a file
- Supports OpenAI, Anthropic (Claude), Google Gemini, and Ollama APIs
- Customizable API endpoints and models
- Multiple prompt styles (Conventional, Technical, User-Focused)
- Configuration file support (`~/.ai-mr-comment.toml`)
- Environment variable configuration
- Outputs to console or to a file
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

```toml
# Choose which provider to use: "openai", "anthropic", "gemini", or "ollama"
provider = "gemini"

# === Gemini Settings ===
gemini_api_key = "xxxx"
gemini_model = "gemini-2.5-flash"

# === OpenAI Settings ===
openai_api_key = "xxxx"            
openai_model = "gpt-4o-mini"
openai_endpoint = "https://api.openai.com/v1/chat/completions"

# === Anthropic Settings ===
anthropic_api_key = "xxxx"
anthropic_model = "claude-3-5-sonnet-20240620"
anthropic_endpoint = "https://api.anthropic.com/v1/messages"

# === Ollama Settings ===
ollama_model = "llama3"
ollama_endpoint = "http://localhost:11434/api/generate"

# === Template Settings ===
# Options: default, conventional, technical, user-focused
template = "default"
```

## Usage

```bash
# Generate comment using default provider
ai-mr-comment

# Use a specific provider and template
ai-mr-comment --provider anthropic --template technical

# Generate comment for a specific commit range
ai-mr-comment --commit "HEAD~3..HEAD"

# Show token and cost estimation without calling the API
ai-mr-comment --debug
```

### Options

- `-c, --commit <COMMIT>`: Specific commit or range (default: HEAD)
- `-f, --file <FILE>`: Read diff from file instead of git
- `-o, --output <FILE>`: Write output to file instead of stdout
- `-k, --api-key <API_KEY>`: API key (overrides env/config)
- `-p, --provider <PROVIDER>`: Provider (openai, anthropic, gemini, ollama)
- `-t, --template <NAME>`: Template style (default, conventional, technical, user-focused)
- `--debug`: Debug mode - show precise token usage and cost estimation
- `-h, --help`: Print help
- `-V, --version`: Print version

## Token & Cost Estimation

When running with the `--debug` flag, the tool provides a detailed breakdown of the expected usage:

- **Gemini**: Uses official SDK token counting (100% accurate).
- **OpenAI/Anthropic/Ollama**: Uses a conservative character-based heuristic (~3.5 chars per token).
- **Cost**: Calculates estimated input cost in USD based on current model pricing (Ollama is free).

## Example Output

```markdown
MR Title: Implement user authentication system
MR Summary: This change adds a complete user authentication system including secure password hashing and JWT-based session management.

## Key Changes

- Added user model with bcrypt password hashing
- Implemented JWT authentication middleware
- Created login and registration API endpoints
- Added comprehensive unit tests for auth logic

## Why These Changes

Provides a secure foundation for user identity, allowing protected access to API resources.
```

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

# Run linter
make lint
```

## License

MIT

## Acknowledgements

This project is a Go rewrite of [mr-comment](https://github.com/RobertKozak/mr-comment) originally created by [Robert Kozak](https://github.com/RobertKozak).