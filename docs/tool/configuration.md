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
openai_model = "gpt-5.5"
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
