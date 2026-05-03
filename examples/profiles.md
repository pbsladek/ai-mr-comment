# Profile Examples

## Use Named Profiles for Repeatable Local and CI Workflows

Define profiles in `~/.ai-mr-comment.toml`:

```toml
[profile.local]
provider = "ollama"
model = "llama3.1"
format = "plain"

[profile.ci]
provider = "openai"
model = "gpt-4o-mini"
template = "technical"
format = "json"
exit_code = true
```

Run with the same settings anywhere:

```sh
ai-mr-comment --profile local
ai-mr-comment --profile ci --pr "$PR_URL"
ai-mr-comment quick-commit --profile local --dry-run
```

