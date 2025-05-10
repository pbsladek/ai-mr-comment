# MR Comment Generator (Rust)

A command-line tool written in Rust that generates professional GitLab Merge Request (MR) comments based on git diffs using AI (OpenAI or Claude).

## Features

- Reads git diffs from current repo or from a file
- Supports both OpenAI and Claude (Anthropic) APIs
- Customizable API endpoints and models
- Configuration file support (~/.mr-comment)
- Environment variable configuration
- Outputs to console or to a file
- Proper error handling with context
- Diff truncation and token estimation
- Native binary with no runtime dependencies (thanks to Rust)

## Installation

### Prerequisites

- Rust and Cargo (install via [rustup](https://rustup.rs/))
- Git
- OpenAI API key or Claude API key

### Building from source

```bash
# Clone the repository
git clone https://github.com/yourusername/ai-mr-comment.git
cd ai-mr-comment

# Build in release mode
go build -o ai-mr-comment

# The binary will be available at root
```

### Installing with Go

```bash

```

## Usage

```bash
# Generate comment using Claude (default)
ai-mr-comment --api-key YOUR_CLAUDE_API_KEY

# Generate comment using OpenAI
ai-mr-comment --provider openai --api-key YOUR_OPENAI_API_KEY

# Generate comment for a specific commit
ai-mr-comment --commit a1b2c3d

# Generate comment for a range of commits
ai-mr-comment --commit "HEAD~3..HEAD"

# Read diff from file
ai-mr-comment --file path/to/diff.txt

# Write output to file
ai-mr-comment --output mr-comment.md

# Use a different model
ai-mr-comment --provider claude --model claude-3-haiku-20240307
```

### Options

- `-c, --commit <COMMIT>`: Specific commit to generate comment for (default: HEAD)
- `-f, --file <FILE>`: Read diff from file instead of git command
- `-o, --output <FILE>`: Write output to file instead of stdout
- `-k, --api-key <API_KEY>`: API key (can also use OPENAI_API_KEY or ANTHROPIC_API_KEY env var)
- `-p, --provider <PROVIDER>`: API provider to use (openai or claude)
- `-e, --endpoint <ENDPOINT>`: API endpoint (defaults based on provider)
- `-m, --model <MODEL>`: Model to use (defaults based on provider)
- `-h, --help`: Print help
- `-V, --version`: Print version
- `--debug`: Debug mode - estimate token usage and exit

## Configuration

The tool will look for configuration in the following order:

1. Command line arguments
2. Environment variables (`OPENAI_API_KEY` for OpenAI or `ANTHROPIC_API_KEY` for Claude)
3. Environment variables (`OPENAI_API_KEY` for OpenAI or `ANTHROPIC_API_KEY` for Claude)

### Default Values

#### Claude

- Endpoint: `https://api.anthropic.com/v1/messages`
- Model: `claude-3-7-sonnet-20250219`

#### OpenAI

- Endpoint: `https://api.openai.com/v1/chat/completions`
- Model: `gpt-4-turbo`

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

- `main.go`: Contains all the code for the CLI tool
- `go.mod`: Go package configuration and dependencies

### Dependencies



## License

MIT