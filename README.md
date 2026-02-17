# MR Comment Generator (Go)

A command-line tool written in Go that generates professional GitLab Merge Request (MR) comments based on git diffs using AI (OpenAI, Anthropic, Gemini, or Ollama).

## Features

- Reads git diffs from current repo or from a file
- Supports OpenAI, Anthropic (Claude), Google Gemini, and Ollama APIs
- Customizable API endpoints and models
- Configuration file support (~/.ai-mr-comment)
- Environment variable configuration
- Outputs to console or to a file
- Proper error handling with context
- Diff truncation and token estimation
- Native binary with no runtime dependencies

## Installation

### Prerequisites

- Go
- Git
- OpenAI API key, Anthropic API key, or Google Gemini API key

### Building from source

```bash
# Clone the repository
git clone https://github.com/yourusername/ai-mr-comment.git
cd ai-mr-comment

# Build
make build

# The binary will be available at ./dist/ai-mr-comment
# Build and run
make run

# Show estimated tokens
make run-debug
```

### Installing

Download the latest binary for your OS from the [Releases](https://github.com/pbsladek/ai-mr-comment/releases) page.

```bash
# macOS (Intel)
curl -L https://github.com/pbsladek/ai-mr-comment/releases/latest/download/ai-mr-comment-darwin-amd64 -o /usr/local/bin/ai-mr-comment
chmod +x /usr/local/bin/ai-mr-comment

# macOS (Apple Silicon / M1/M2)
curl -L https://github.com/your-org/ai-mr-comment/releases/latest/download/ai-mr-comment-darwin-arm64 -o /usr/local/bin/ai-mr-comment
chmod +x /usr/local/bin/ai-mr-comment

# Linux (x86_64)
curl -L https://github.com/your-org/ai-mr-comment/releases/latest/download/ai-mr-comment-linux-amd64 -o /usr/local/bin/ai-mr-comment
chmod +x /usr/local/bin/ai-mr-comment

# Windows (x86_64)
# Download and add it to your PATH.
https://github.com/your-org/ai-mr-comment/releases/latest/download/ai-mr-comment-windows-amd64.exe
```

## Configuration File

```toml
provider = "openai"

openai_api_key = "xxxx"                    
openai_model = "gpt-4o-mini"
openai_endpoint = "https://api.openai.com/v1/chat/completions"

anthropic_api_key = "xxxx"
anthropic_model = "claude-3-7-sonnet-20250219"
anthropic_endpoint = "https://api.anthropic.com/v1/messages"

gemini_api_key = "xxxx"
gemini_model = "gemini-1.5-flash"

ollama_model = "ollama"
ollama_endpoint = "http://localhost:11434/api/generate"
```

```toml
# Choose which provider to use: "openai", "anthropic", "gemini", or "ollama"
provider = "openai"

# === OpenAI Settings ===
# Your OpenAI API key
openai_api_key = "xxxx"            
# The OpenAI model to use (e.g., gpt-4, gpt-4o-mini)         
openai_model = "gpt-4o-mini"
 # Custom endpoint (optional, default is OpenAI's)
openai_endpoint = "https://api.openai.com/v1/chat/completions"

# === Anthropic Settings ===
# Your Anthropic Claude API key
anthropic_api_key = "xxxx"
# The Claude model to use
anthropic_model = "claude-3-7-sonnet-20250219"
# Custom endpoint (optional, default is Anthropic's)
anthropic_endpoint = "https://api.anthropic.com/v1/messages"

# === Gemini Settings ===
# Your Google Gemini API key
gemini_api_key = "xxxx"
# The Gemini model to use
gemini_model = "gemini-1.5-flash"

# === Ollama Settings ===
# The Ollama model to use
ollama_model = "ollama"
# Custom endpoint (optional, default is Ollama's)
ollama_endpoint = "http://localhost:11434/api/generate"
```

## Usage

```bash
# Generate comment using OpenAI (default)
ai-mr-comment --api-key YOUR_OPENAI_API_KEY

# Generate comment using Anthropic
ai-mr-comment --provider anthropic --api-key  YOUR_ANTHROPIC_API_KEY

# Generate comment using Gemini
ai-mr-comment --provider gemini --api-key YOUR_GEMINI_API_KEY

# Generate comment for a specific commit
ai-mr-comment --commit a1b2c3d

# Generate comment for a range of commits
ai-mr-comment --commit "HEAD~3..HEAD"

# Read diff from file
ai-mr-comment --file path/to/diff.txt

# Write output to file
ai-mr-comment --output ai-mr-comment.md

# Use a different model
ai-mr-comment --provider anthropic --model claude-3-haiku-20240307
```

### Options

- `-c, --commit <COMMIT>`: Specific commit to generate comment for (default: HEAD)
- `-f, --file <FILE>`: Read diff from file instead of git command
- `-o, --output <FILE>`: Write output to file instead of stdout
- `-k, --api-key <API_KEY>`: API key (can also use OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY env var)
- `-p, --provider <PROVIDER>`: API provider to use (openai, anthropic, gemini, ollama)
- `-e, --endpoint <ENDPOINT>`: API endpoint (defaults based on provider)
- `-m, --model <MODEL>`: Model to use (defaults based on provider)
- `-h, --help`: Print help
- `-V, --version`: Print version
- `--debug`: Debug mode - estimate token usage and exit

## Configuration

The tool will look for configuration in the following order:

1. Command line arguments
2. Environment variables (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `GEMINI_API_KEY`)
3. Configuration file

### Default Values

#### OpenAI

- Endpoint: `https://api.openai.com/v1/chat/completions`
- Model: `gpt-4o-mini`

#### Anthropic

- Endpoint: `https://api.anthropic.com/v1/messages`
- Model: `claude-3-7-sonnet-20250219`

#### Gemini

- Model: `gemini-1.5-flash`

#### Ollama

- Endpoint: `http://localhost:11434/api/generate`
- Model: `llama3`

## Example Output

```markdown
Implement user authentication system

This MR adds a complete user authentication system including login, registration, password reset, and account management.

## Key Changes

- Added user model with secure password hashing
- Implemented JWT-based authentication
- Created login and registration API endpoints
- Added password reset functionality
- Included comprehensive test coverage

## Why These Changes

These changes provide a secure authentication foundation for the application, allowing users to create accounts and access protected features.

## Review Checklist

- [ ] Verify all authentication routes are properly protected
- [ ] Check password hashing implementation
- [ ] Review JWT token expiration and refresh logic
- [ ] Confirm test coverage for edge cases
- [ ] Validate error handling for invalid credentials

## Notes

The implementation follows OWASP security guidelines and includes rate limiting to prevent brute force attacks.
```

## Development

### Project Structure

- `sr/`: Contains all the code for the CLI tool
- `go.mod`: Go package configuration and dependencies

### Dependencies

- see `go.mod`

## License

MIT

## Acknowledgements

This project is Go rewrite of [mr-comment](https://github.com/RobertKozak/mr-comment) originally created by [Robert Kozak](https://github.com/RobertKozak).
