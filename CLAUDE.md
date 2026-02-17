# AI MR Comment Generator

A command-line tool written in Go that generates professional GitLab Merge Request (MR) comments based on git diffs using AI (OpenAI, Anthropic, Gemini, or Ollama).

## Build & Run

- **Build**: `make build` (creates binary in `./dist/ai-mr-comment`)
- **Run**: `make run` or `./dist/ai-mr-comment`
- **Debug**: `make run-debug` (shows token estimation)
- **Test**: `go test ./src/...`
- **Install dependencies**: `go mod download`

## Code Structure

- `src/`: Source code for the CLI tool.
  - `main.go`: Entry point, command definition, and core logic.
  - `main_test.go`: Tests for main logic.
- `dist/`: Built binaries.
- `Makefile`: Build scripts.

## Configuration

- Config file: `~/.ai-mr-comment` (TOML format).
- Environment variables: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`.
- Providers: `openai`, `anthropic`, `gemini`, `ollama`.

## Key Features

- Reads git diffs from repo or file.
- Supports multiple AI providers.
- Estimates token usage.
- Outputs to console or file.