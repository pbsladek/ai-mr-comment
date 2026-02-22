`ai-mr-comment` is a CLI that generates MR/PR descriptions, review comments, titles, and commit messages from git diffs using AI.

Supported providers:
- OpenAI
- Anthropic
- Gemini
- Ollama

This image is built with Docker Hardened Images, pinned by digest, and runs as a non-root user.

## Quick start

```bash
docker run --rm \
  -e OPENAI_API_KEY=... \
  -v "$PWD:/work" -w /work \
  pwbsladek/ai-mr-comment:latest --help
```

## Examples

### OpenAI
```bash
docker run --rm \
  -e OPENAI_API_KEY=... \
  -v "$PWD:/work" -w /work \
  pwbsladek/ai-mr-comment:latest \
  --provider openai --model gpt-4o --staged
```

### Anthropic
```bash
docker run --rm \
  -e ANTHROPIC_API_KEY=... \
  -v "$PWD:/work" -w /work \
  pwbsladek/ai-mr-comment:latest \
  --provider anthropic --model claude-opus-4-6 --staged
```

### Gemini
```bash
docker run --rm \
  -e GEMINI_API_KEY=... \
  -v "$PWD:/work" -w /work \
  pwbsladek/ai-mr-comment:latest \
  --provider gemini --model gemini-2.5-flash --staged
```

### Ollama
```bash
docker run --rm \
  -e OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  -v "$PWD:/work" -w /work \
  pwbsladek/ai-mr-comment:latest \
  --provider ollama --model llama3.1 --staged
```

### PR URL input
```bash
docker run --rm \
  -e OPENAI_API_KEY=... \
  pwbsladek/ai-mr-comment:latest \
  --provider openai \
  --pr https://github.com/owner/repo/pull/123
```
