# Configuration

Configuration is read from command-line flags, environment variables, and the
optional config file at `~/.ai-mr-comment.toml`. Command-line flags take
precedence for a single run.

Minimal OpenAI setup:

```sh
export OPENAI_API_KEY="..."
ai-mr-comment --provider openai
```

Example config:

```toml
provider = "openai"
openai_model = "gpt-6.1-sol"
template = "default"

[profile.fast]
provider = "ollama"
ollama_model = "llama3.1"

[profile.ci]
provider = "openai"
openai_model = "gpt-5.4-mini"
template = "technical"
```

Use a profile with any command:

```sh
ai-mr-comment --profile ci --pr "$PR_URL"
ai-mr-comment quick-commit --profile fast --dry-run
```

## Providers

Supported providers:

- `openai`
- `anthropic`
- `gemini`
- `ollama`
- `claude-cli`
- `gemini-cli`
- `codex-cli`

API-backed providers require their normal API key environment variables. Local
CLI providers require the provider executable to be installed and authenticated
before `ai-mr-comment` calls it.

Useful provider commands:

```sh
ai-mr-comment check --provider openai
ai-mr-comment check --all
ai-mr-comment models --provider anthropic
ai-mr-comment doctor --provider openai
```

## Presets and Templates

Presets apply groups of defaults:

```sh
ai-mr-comment --preset ci --pr "$PR_URL"
ai-mr-comment --preset local-fast --file=-
ai-mr-comment --preset security
ai-mr-comment changelog --preset release-notes --commit "v1.2.0..HEAD"
```

Templates tune the review voice and focus:

```sh
ai-mr-comment --template technical
ai-mr-comment --template security
```

For complete control, provide a custom system prompt:

```sh
ai-mr-comment --system-prompt "Review only for concurrency bugs and data races."
```


## Request limits

These settings also work inside a named profile. Explicit command-line flags take
precedence over environment variables, profile values, and the base file.

| Setting | CLI flag | Default | Purpose |
| --- | --- | --- | --- |
| `chunk_concurrency` | `--chunk-concurrency` | `4` | Maximum simultaneous file-summary calls with `--smart-chunk`. |
| `max_output_tokens` | `--max-output-tokens` | `4000` | Generated tokens per API request, including reasoning tokens where the provider counts them. |
| `max_input_bytes` | `--max-input-bytes` | `16777216` | Maximum diff input size in bytes, before line truncation. |

All limits must be positive integers. `max_output_tokens` must fit in a signed
32-bit integer; providers may impose a smaller model-specific limit. File and
stdin reads stop after the input limit plus one byte, then report an error if the
limit was exceeded. Git and remote diffs are checked after retrieval.

```toml
chunk_concurrency = 4
max_output_tokens = 8192
max_input_bytes = 16777216

[profile.large-review]
chunk_concurrency = 2
max_output_tokens = 16384
```

The equivalent environment variables are `AI_MR_COMMENT_CHUNK_CONCURRENCY`,
`AI_MR_COMMENT_MAX_OUTPUT_TOKENS`, and `AI_MR_COMMENT_MAX_INPUT_BYTES`.

The output limit is sent to OpenAI, Anthropic, Gemini, and Ollama. Local CLI
providers use the installed CLI's own output controls. The chunk limit applies
to file summaries; title and description calls can still run concurrently.
Use `doctor --format json` to inspect the effective limits.

## Model selection

Defaults for new configurations are `gpt-6.1-sol`, `claude-sonnet-5-5`, and
`gemini-3.8-flash`. Their CLI counterparts use the same Claude and Gemini
defaults; Codex CLI continues to choose its own default when no model is set.
Existing files and profiles retain their configured model IDs.

`models` and shell completion include GPT-6 Astra, GPT-6.1 Sol/Terra, GPT-6 Luna,
Claude Fable 5.1, Claude Opus/Sonnet/Haiku 5.5, and current Gemini models.
The built-in catalog supplies suggestions. Any provider-supported model ID can
be passed through `--model` or a provider's model setting, including a new ID
that isn't listed yet. Availability depends on your account and installed CLI.

Model IDs and standard text input prices were checked on November 7, 2026 using
[OpenAI's model guide](https://developers.openai.com/api/docs/guides/latest-model)
and [pricing](https://developers.openai.com/api/docs/pricing),
[Claude's catalog](https://platform.claude.com/docs/en/models/overview) and
[pricing](https://platform.claude.com/docs/en/about-claude/pricing), and
[Google's catalog](https://ai.google.dev/gemini-api/docs/models) and
[pricing](https://ai.google.dev/gemini-api/docs/pricing).

Cost estimates cover uncached input tokens only. Unknown model prices display
`unknown`; Ollama always displays zero API input cost. Claude Haiku 5.5's input
tier changes above 100,000 tokens. The estimate also accounts for Google's
published introductory-price end dates for Gemini 4 Argon and Gemini 3.8 Flash.
Output tokens, caching, service tiers, regional pricing, and local compute costs
are outside this estimate.

## Automated dependency updates

Dependabot checks Go modules, GitHub Actions, npm release tooling, and Docker
images every Monday. Docker updates use the `dhi` registry configuration in
`.github/dependabot.yml`. Add `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` as
repository or organization **Dependabot secrets** with access to `dhi.io`.
Use the same secret names as the release workflow; Actions secrets alone do not
supply Dependabot's private-registry credentials. See
[GitHub's registry configuration guide](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/manage-your-dependency-security/configure-access-to-private-registries).
