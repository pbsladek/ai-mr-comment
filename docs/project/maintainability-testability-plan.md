# Maintainability and Testability Plan

This file tracks the phased work to improve maintainability, test coverage, testability, and release validation. Update the status checkboxes as work lands.

## Status Legend

- `[ ]` Not started
- `[~]` In progress
- `[x]` Complete
- `[!]` Blocked or needs a decision

## Baseline

- Internal package coverage from the review pass:
  - `internal/app`: 76.4%
  - `internal/commit`: 53.6%
  - `internal/config`: 72.4%
  - `internal/estimate`: 100.0%
  - `internal/gitdiff`: 95.5%
  - `internal/prompts`: 94.7%
  - `internal/providers`: 13.6%
  - `internal/remote`: 26.5%
  - Total internal coverage: 61.7%
- Known completed hardening before this tracker:
  - `[x]` GitHub workflow `uses:` references are pinned to commit SHA.
  - `[x]` A repo-level test fails if external workflow actions are not SHA-pinned.
  - `[x]` Temp git repo test helpers disable inherited commit/tag signing.

## Phase 0: Release Safety

Goal: prevent broken or partially validated artifacts from being tagged, published, or trusted.

- `[x]` Make tagging/release depend on successful required validation, not only semantic-release on `main`.
- `[x]` Ensure release cannot proceed unless the standard test workflow has passed for the release commit.
- `[x]` Ensure release cannot proceed unless deterministic e2e smoke has passed for the release commit.
- `[x]` Decide whether provider e2e checks are release-blocking or advisory, since they depend on external secrets/services.
- `[x]` Scan the exact multiarch Docker image path that will be published, or scan both `linux/amd64` and `linux/arm64` before publishing the manifest.
- `[x]` Add FIPS Docker image tags and digest to the immutable release manifest.
- `[x]` Verify normal and FIPS Docker image references in release verification.
- `[x]` Verify SBOM assets in release verification.
- `[x]` Pin release tool inputs beyond action SHAs:
  - `[x]` Exact Node version or checked-in lockfile path.
  - `[x]` Locked semantic-release install path instead of floating `npx --yes semantic-release`.
  - `[x]` Explicit GoReleaser version.

Acceptance:

- `[x]` A release tag cannot publish artifacts from an unvalidated commit.
- `[x]` Multiarch Docker publish is covered by matching vulnerability scan coverage.
- `[x]` Release manifest includes normal and FIPS Docker references.
- `[x]` Release verification fails when SBOMs or Docker digests are missing.

## Phase 1: Test Determinism

Goal: make `go test ./...` independent of the developer checkout, branch, remotes, staged files, signing config, and cwd.

- `[x]` Inventory tests that depend on the current checkout, local branch, local remotes, or staged/dirty state.
- `[x]` Move checkout-dependent git tests to temp repos using shared helpers.
- `[x]` Keep any real-checkout tests behind explicit integration/build tags.
- `[x]` Replace remaining relative fixture paths with `testdataPath` or equivalent absolute helpers.
- `[x]` Reduce package-wide cwd assumptions in `internal/app` tests.
- `[x]` Add shared helpers for:
  - `[x]` command execution with env/home isolation,
  - `[x]` fake git repositories,
  - `[x]` fake remote GitHub/GitLab servers,
  - `[x]` fake provider responses.

Acceptance:

- `[x]` `go test ./...` passes from any cwd.
- `[x]` Unit tests do not skip or fail based on local git branch/remotes/staged files.
- `[x]` Current-checkout tests are clearly tagged as integration tests.

## Phase 2: Side-Effect Seams

Goal: make command behavior unit-testable without real git, network, clipboard, filesystem, or time side effects.

- `[x]` Introduce a small git operations interface or function bundle.
- `[x]` Introduce a remote operations interface or function bundle.
- `[x]` Introduce clipboard and file output seams.
- `[x]` Introduce env/home lookup seams where command behavior depends on process state.
- `[x]` Introduce a time/clock seam for elapsed-time and timeout behavior.
- `[x]` Thread seams through:
  - `[x]` `newRootCmd`,
  - `[x]` `newQuickCommitCmd`,
  - `[x]` `newPublishCmd`,
  - `[x]` changelog command paths.
- `[x]` Add orchestration tests for:
  - `[x]` quick-commit stages before diff,
  - `[x]` commit happens before push,
  - `[x]` post happens after push,
  - `[x]` no post happens on push failure,
  - `[x]` GitHub vs GitLab dispatch,
  - `[x]` remote diff failure,
  - `[x]` review generation failure,
  - `[x]` comment post failure.

Acceptance:

- `[x]` High-risk command side effects can be tested with fakes instead of real git/network state.
- `[x]` quick-commit post flow has deterministic success and failure tests.

## Phase 3: Command Orchestration Refactor

Goal: shrink `root.RunE` into a coordinator while preserving backwards-compatible CLI behavior.

- `[x]` Extract a `RootOptions` struct from root flag values.
- `[x]` Add a pure `validateRootOptions` function.
- `[x]` Remove duplicated root validation for post/update metadata flag combinations.
- `[x]` Split root execution into pipeline functions:
  - `[x]` load and override config,
  - `[x]` resolve diff input,
  - `[x]` resolve prompt/template,
  - `[x]` generate provider output,
  - `[x]` render output,
  - `[x]` apply side effects.
- `[x]` Move JSON/text output construction into reusable renderer functions.
- `[x]` Table-test root validation and renderer behavior.

Acceptance:

- `[x]` `root.RunE` is smaller and less duplicated, with diff resolution, prompt resolution, provider generation, and rendering extracted.
- `[x]` validation is table-tested and not duplicated.
- `[x]` output payload construction is covered without invoking providers.

## Phase 4: Registry and Boundary Cleanup

Goal: make providers, templates, and remotes easier to extend without scattered edits.

- `[x]` Create a provider registry with metadata for:
  - `[x]` provider name,
  - `[x]` default model,
  - `[x]` completion values,
  - `[x]` validation requirements,
  - `[x]` call dispatch,
  - `[x]` streaming support.
- `[x]` Create a template registry with metadata for:
  - `[x]` template name,
  - `[x]` prompt type,
  - `[x]` completion values,
  - `[x]` MR-only vs commit-only compatibility.
- `[x]` Move prompt/template ownership fully into `internal/prompts`.
- `[x]` Replace scattered provider/template lists in command wiring with registry calls.
- `[x]` Gradually remove the broad `internal/app/git.go` facade:
  - `[x]` use `internal/remote` directly for PR/MR URL and remote operations,
  - `[x]` use `internal/gitdiff` directly for diff logic,
  - `[x]` move local shell git operations into a dedicated internal package.
- `[x]` Consolidate PR/MR operations behind a shared remote target abstraction used by root, publish, and quick-commit.

Acceptance:

- `[x]` Adding a provider or template requires one registry update plus tests.
- `[x]` command validation uses registry metadata.
- `[x]` package ownership matches the `cmd/internal` layout; prompt ownership, local shell-git operations, and shared remote target operations are in dedicated internal packages.

## Phase 5: Coverage Expansion

Goal: raise confidence in low-coverage, high-risk packages.

- `[x]` Raise `internal/providers` coverage from 13.6% to at least 60%.
- `[x]` Raise `internal/remote` coverage from 26.5% to at least 70%.
- `[x]` Raise `internal/commit` coverage from 53.6% to at least 80%.
- `[x]` Raise total internal coverage from 61.7% to at least 75%.
- `[x]` Add direct `internal/remote` tests for:
  - `[x]` GitHub metadata fetch/update,
  - `[x]` GitHub comment create/update,
  - `[x]` GitHub labels/reviewers,
  - `[x]` GitHub auth error wrapping,
  - `[x]` GitLab metadata fetch/update,
  - `[x]` GitLab note create/update,
  - `[x]` GitLab labels/reviewers,
  - `[x]` GitLab auth error wrapping,
  - `[x]` GitLab pagination and malformed diff formatting.
- `[x]` Add direct `internal/providers` tests for:
  - `[x]` OpenAI error enrichment,
  - `[x]` Anthropic error enrichment,
  - `[x]` CLI execution failures,
  - `[x]` streaming paths,
  - `[x]` Ollama API errors,
  - `[x]` global state cleanup.
- `[x]` Add direct `internal/commit` tests for:
  - `[x]` message templates,
  - `[x]` prompt guidance,
  - `[x]` editor splitting/defaulting,
  - `[x]` breaking-change enforcement edge cases.
- `[x]` Add a fixture manifest or table that asserts every `testdata/*.diff` fixture is exercised by at least one parser or CLI test.

Acceptance:

- `[x]` Coverage targets above are met or explicitly revised with rationale.
- `[x]` High-risk remote/provider behavior is covered without live external services.
- `[x]` Diff fixtures are all intentionally exercised.

## Progress Log

- Initial tracker created with all phases open.
- 2026-05-08: Implemented release safety gates, locked semantic-release tooling, multiarch Docker scan coverage, FIPS manifest/verification, command dependency seams, deterministic quick-commit/git tests, root option validation/rendering extraction, provider/template registries, direct remote/provider/commit coverage, and fixture manifest coverage.
- 2026-05-08: Final measured coverage after the boundary cleanup: `internal/app` 80.5%, `internal/providers` 61.7%, `internal/remote` 75.0%, `internal/commit` 83.7%, `internal/localgit` 64.4%, `internal/prompts` 66.7%, total internal 77.4%.
- 2026-05-08: Completed the remaining cleanup: root prompt and provider generation now live in pipeline helpers, provider metadata includes call/stream dispatch, built-in templates moved to `internal/prompts/templates`, shell git execution moved to `internal/localgit`, and GitHub/GitLab operations share the `internal/remote.Target` abstraction across root, publish, and quick-commit.
