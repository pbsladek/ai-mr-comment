package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/generative-ai-go/genai"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"github.com/pbsladek/ai-mr-comment/internal/providers"
	"google.golang.org/api/option"
)

// geminiClientOptions allows tests to inject a custom endpoint and HTTP client
// for the Gemini SDK without modifying call sites.
var geminiClientOptions []option.ClientOption

var geminiOptionsActive bool

const defaultOllamaHTTPTimeout = providers.DefaultOllamaHTTPTimeout

func syncGeminiClientOptions() {
	if len(geminiClientOptions) > 0 {
		providers.SetGeminiClientOptions(geminiClientOptions)
		geminiOptionsActive = true
		return
	}
	if geminiOptionsActive {
		providers.SetGeminiClientOptions(nil)
		geminiOptionsActive = false
	}
}

func getOllamaHTTPTimeout() time.Duration {
	return providers.GetOllamaHTTPTimeout()
}

func getGeminiClient(ctx context.Context, apiKey string) (*genai.Client, error) {
	syncGeminiClientOptions()
	return providers.GetGeminiClient(ctx, apiKey)
}

func callOpenAI(ctx context.Context, client *openai.Client, cfg *Config, systemPrompt, diffContent string) (string, error) {
	return providers.CallOpenAI(ctx, client, cfg, systemPrompt, diffContent)
}

func callAnthropic(ctx context.Context, client *anthropic.Client, cfg *Config, systemPrompt, diffContent string) (string, error) {
	return providers.CallAnthropic(ctx, client, cfg, systemPrompt, diffContent)
}

func callOllama(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	return providers.CallOllama(ctx, cfg, systemPrompt, diffContent)
}

func callGemini(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	syncGeminiClientOptions()
	return providers.CallGemini(ctx, cfg, systemPrompt, diffContent)
}

func cliPrompt(systemPrompt, diffContent string) string {
	return providers.CLIPrompt(systemPrompt, diffContent)
}

func cliExecError(ctx context.Context, name string, err error, stderr string) error {
	return providers.CLIExecError(ctx, name, err, stderr)
}

func execCLI(ctx context.Context, binary string, args []string) (string, error) {
	return providers.ExecCLI(ctx, binary, args)
}

func streamExecCLI(ctx context.Context, binary string, args []string, w io.Writer) (string, error) {
	return providers.StreamExecCLI(ctx, binary, args, w)
}

func findClaudeBinary(cfg *Config) (string, error) {
	return providers.FindClaudeBinary(cfg)
}

func claudeCLIArgs(cfg *Config, prompt string) []string {
	return providers.ClaudeCLIArgs(cfg, prompt)
}

func callClaudeCLI(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	return providers.CallClaudeCLI(ctx, cfg, systemPrompt, diffContent)
}

func streamClaudeCLI(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	return providers.StreamClaudeCLI(ctx, cfg, systemPrompt, diffContent, w)
}

func findGeminiCLIBinary(cfg *Config) (string, error) {
	return providers.FindGeminiCLIBinary(cfg)
}

func geminiCLIArgs(cfg *Config, prompt string) []string {
	return providers.GeminiCLIArgs(cfg, prompt)
}

func callGeminiCLI(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	return providers.CallGeminiCLI(ctx, cfg, systemPrompt, diffContent)
}

func streamGeminiCLI(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	return providers.StreamGeminiCLI(ctx, cfg, systemPrompt, diffContent, w)
}

func findCodexBinary(cfg *Config) (string, error) {
	return providers.FindCodexBinary(cfg)
}

func codexCLIArgs(cfg *Config, prompt string) []string {
	return providers.CodexCLIArgs(cfg, prompt)
}

func callCodexCLI(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	return providers.CallCodexCLI(ctx, cfg, systemPrompt, diffContent)
}

func streamCodexCLI(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	return providers.StreamCodexCLI(ctx, cfg, systemPrompt, diffContent, w)
}

func validateAPIKey(provider ApiProvider, cfg *Config) error {
	return providers.ValidateAPIKey(provider, cfg)
}

// chatCompletions dispatches a prompt and diff to the appropriate provider and
// returns the generated comment.
func chatCompletions(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
	if err := validateAPIKey(provider, cfg); err != nil {
		return "", err
	}
	switch provider {
	case OpenAI:
		debugLog(cfg, "api: calling openai model=%s endpoint=%s mode=buffered", cfg.OpenAIModel, cfg.OpenAIEndpoint)
		client := openai.NewClient(
			openaiopt.WithAPIKey(cfg.OpenAIAPIKey),
			openaiopt.WithBaseURL(cfg.OpenAIEndpoint),
		)
		return callOpenAI(ctx, &client, cfg, systemPrompt, diffContent)
	case Anthropic:
		debugLog(cfg, "api: calling anthropic model=%s endpoint=%s mode=buffered", cfg.AnthropicModel, cfg.AnthropicEndpoint)
		client := anthropic.NewClient(
			anthropicopt.WithAPIKey(cfg.AnthropicAPIKey),
			anthropicopt.WithBaseURL(strings.TrimRight(cfg.AnthropicEndpoint, "/")+"/"),
		)
		return callAnthropic(ctx, &client, cfg, systemPrompt, diffContent)
	case Ollama:
		debugLog(cfg, "api: calling ollama model=%s endpoint=%s mode=buffered", cfg.OllamaModel, cfg.OllamaEndpoint)
		return callOllama(ctx, cfg, systemPrompt, diffContent)
	case Gemini:
		debugLog(cfg, "api: calling gemini model=%s mode=buffered", cfg.GeminiModel)
		return callGemini(ctx, cfg, systemPrompt, diffContent)
	case ClaudeCLI:
		debugLog(cfg, "api: calling claude-cli model=%s mode=buffered", cfg.ClaudeCLIModel)
		return callClaudeCLI(ctx, cfg, systemPrompt, diffContent)
	case GeminiCLI:
		debugLog(cfg, "api: calling gemini-cli model=%s mode=buffered", cfg.GeminiCLIModel)
		return callGeminiCLI(ctx, cfg, systemPrompt, diffContent)
	case CodexCLI:
		debugLog(cfg, "api: calling codex-cli model=%s mode=buffered", cfg.CodexCLIModel)
		return callCodexCLI(ctx, cfg, systemPrompt, diffContent)
	default:
		return "", errors.New("unsupported provider")
	}
}

// streamToWriter streams tokens from the AI provider to w as they arrive and
// returns the full accumulated response.
func streamToWriter(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string, w io.Writer) (string, error) {
	if err := validateAPIKey(provider, cfg); err != nil {
		return "", err
	}
	switch provider {
	case OpenAI:
		debugLog(cfg, "api: calling openai model=%s endpoint=%s mode=stream", cfg.OpenAIModel, cfg.OpenAIEndpoint)
		client := openai.NewClient(
			openaiopt.WithAPIKey(cfg.OpenAIAPIKey),
			openaiopt.WithBaseURL(cfg.OpenAIEndpoint),
		)
		return streamOpenAI(ctx, &client, cfg, systemPrompt, diffContent, w)
	case Anthropic:
		debugLog(cfg, "api: calling anthropic model=%s endpoint=%s mode=stream", cfg.AnthropicModel, cfg.AnthropicEndpoint)
		client := anthropic.NewClient(
			anthropicopt.WithAPIKey(cfg.AnthropicAPIKey),
			anthropicopt.WithBaseURL(strings.TrimRight(cfg.AnthropicEndpoint, "/")+"/"),
		)
		return streamAnthropic(ctx, &client, cfg, systemPrompt, diffContent, w)
	case Ollama:
		debugLog(cfg, "api: calling ollama model=%s endpoint=%s mode=stream", cfg.OllamaModel, cfg.OllamaEndpoint)
		return streamOllama(ctx, cfg, systemPrompt, diffContent, w)
	case Gemini:
		debugLog(cfg, "api: calling gemini model=%s mode=stream", cfg.GeminiModel)
		return streamGemini(ctx, cfg, systemPrompt, diffContent, w)
	case ClaudeCLI:
		debugLog(cfg, "api: calling claude-cli model=%s mode=stream", cfg.ClaudeCLIModel)
		return streamClaudeCLI(ctx, cfg, systemPrompt, diffContent, w)
	case GeminiCLI:
		debugLog(cfg, "api: calling gemini-cli model=%s mode=stream", cfg.GeminiCLIModel)
		return streamGeminiCLI(ctx, cfg, systemPrompt, diffContent, w)
	case CodexCLI:
		debugLog(cfg, "api: calling codex-cli model=%s mode=stream", cfg.CodexCLIModel)
		return streamCodexCLI(ctx, cfg, systemPrompt, diffContent, w)
	default:
		return "", errors.New("unsupported provider")
	}
}

func streamOpenAI(ctx context.Context, client *openai.Client, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	return providers.StreamOpenAI(ctx, client, cfg, systemPrompt, diffContent, w)
}

func streamAnthropic(ctx context.Context, client *anthropic.Client, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	return providers.StreamAnthropic(ctx, client, cfg, systemPrompt, diffContent, w)
}

func streamGemini(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	syncGeminiClientOptions()
	return providers.StreamGemini(ctx, cfg, systemPrompt, diffContent, w)
}

func streamOllama(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	return providers.StreamOllama(ctx, cfg, systemPrompt, diffContent, w)
}
