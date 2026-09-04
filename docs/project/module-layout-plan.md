# Go Package Layout Plan

This tracks the incremental migration from a single root `package main` to a
more idiomatic Go CLI layout.

## Target Shape

```text
cmd/ai-mr-comment/        # executable entry point
internal/app/             # Cobra command orchestration
internal/config/          # config loading and profiles
internal/providers/       # model provider clients
internal/remote/          # GitHub/GitLab remote operations
internal/gitdiff/         # local git diff helpers
internal/prompts/         # prompt and template resolution
internal/estimate/        # token and cost estimation
internal/commit/          # quick-commit and commit-message helpers
```

## Migration Order

1. Move leaf packages with minimal dependencies.
2. Keep root wrappers while callers are migrated.
3. Move provider and remote packages after their interfaces are clearer.
4. Move Cobra command construction into `internal/app`.
5. Add `cmd/ai-mr-comment/main.go` as the final entry point.

## Current Progress

- Done: `internal/estimate` owns heuristic token counting and cost estimation.
- Done: `internal/prompts` owns system-prompt resolution and custom template lookup.
- Done: `internal/config` owns config types, defaults, profile loading, and env binding.
- Done: `internal/gitdiff` owns raw diff file reading, splitting, and truncation.
- Done: `internal/remote` owns hosted URL parsing, remote URL normalization, host detection, PR/MR create URLs, and GitHub/GitLab SDK operations.
- Done: `internal/providers` owns provider API calls, streaming, key validation, Gemini client caching, and local AI CLI helpers.
- Done: `internal/commit` owns quick-commit message normalization, conventional commit validation, guidance, signoff, emoji, breaking-change, and editor helpers.
- Done: `internal/app` owns Cobra command orchestration and embedded application templates.
- Done: `cmd/ai-mr-comment` owns the executable entry point and build metadata handoff.
- Done: split command groups into focused `internal/app/*_cmd.go` and root pipeline files.
