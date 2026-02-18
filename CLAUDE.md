# AI MR Comment Generator

A command-line tool written in Go that generates professional MR/PR comments based on git diffs using AI (OpenAI, Anthropic, Gemini, or Ollama).

## Build & Run

- **Build**: `make build` (creates binary in `./dist/ai-mr-comment`)
- **Run**: `make run` or `./dist/ai-mr-comment`
- **Test**: `go test ./...`
- **Lint**: `make lint`
- **Install dependencies**: `go mod tidy`

## Code Structure

- `main.go`: Entry point, command definition, and core logic.
- `api.go`: Logic for calling external AI provider APIs.
- `config.go`: Configuration loading (Viper).
- `git.go`: Git diff handling.
- `prompt.go`: System prompt generation.
- `token_estimator.go`: Token counting and cost estimation logic.
- `*.go`: Go source files are in the project root.
- `dist/`: Built binaries.
- `Makefile`: Build scripts.

## Configuration

- Config file: `~/.ai-mr-comment.toml` or `ai-mr-comment.toml` (TOML format).
- Environment variables: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`.
- Providers: `openai`, `anthropic`, `gemini`, `ollama`.

## Key Features

- Reads git diffs from repo or file.
- Supports multiple AI providers.
- Estimates token usage.
- Outputs to console or file.

## Testing Notes

- `go test ./...` runs all unit tests; `make test` runs with `-v`.
- `make test-integration` runs integration tests (requires `GEMINI_API_KEY`).
- Tests that call `git diff HEAD^` or `git diff HEAD~1..HEAD` skip automatically when run in shallow clone environments (e.g., CI with `fetch-depth: 1`).
- `errcheck` lint rule is enforced: always assign return values from `json.NewEncoder(w).Encode(...)` and `w.Write(...)` to `_` in test HTTP handlers.

## Lint Notes

- Linter: `golangci-lint` via `make lint`.
- All error return values must be checked or explicitly discarded with `_`.