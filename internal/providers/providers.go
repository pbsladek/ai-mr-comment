package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/generative-ai-go/genai"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/pbsladek/ai-mr-comment/internal/config"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var geminiClientOptions []option.ClientOption

var (
	geminiCachedClient    *genai.Client
	geminiCachedClientKey string
	geminiClientMu        sync.Mutex
)

const DefaultOllamaHTTPTimeout = 2 * time.Minute

// SetGeminiClientOptions allows tests to inject Gemini SDK options.
func SetGeminiClientOptions(opts []option.ClientOption) {
	geminiClientMu.Lock()
	defer geminiClientMu.Unlock()

	geminiClientOptions = opts
	geminiCachedClient = nil
	geminiCachedClientKey = ""
}

func GetOllamaHTTPTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS"))
	if raw == "" {
		return DefaultOllamaHTTPTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return DefaultOllamaHTTPTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

var ollamaHTTPClient = &http.Client{Timeout: GetOllamaHTTPTimeout()}

// GetGeminiClient returns a cached *genai.Client for apiKey.
func GetGeminiClient(ctx context.Context, apiKey string) (*genai.Client, error) {
	geminiClientMu.Lock()
	defer geminiClientMu.Unlock()

	if geminiCachedClient != nil && geminiCachedClientKey == apiKey && len(geminiClientOptions) == 0 {
		return geminiCachedClient, nil
	}

	opts := []option.ClientOption{option.WithAPIKey(apiKey)}
	opts = append(opts, geminiClientOptions...)
	client, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	geminiCachedClient = client
	geminiCachedClientKey = apiKey
	return client, nil
}

// CallOpenAI sends a Responses API request to OpenAI.
func CallOpenAI(ctx context.Context, client *openai.Client, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	resp, err := client.Responses.New(ctx, responses.ResponseNewParams{
		Model:           cfg.OpenAIModel,
		Instructions:    param.NewOpt(systemPrompt),
		Input:           responses.ResponseNewParamsInputUnion{OfString: param.NewOpt(diffContent)},
		MaxOutputTokens: param.NewOpt(int64(4000)),
		Store:           param.NewOpt(false),
		Truncation:      responses.ResponseNewParamsTruncationAuto,
	})
	if err != nil {
		return "", enrichOpenAIError(err)
	}
	output := resp.OutputText()
	if output == "" {
		return "", errors.New("no output text returned")
	}
	return output, nil
}

func enrichNetworkError(err error) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return fmt.Errorf("%w\n\nCould not reach the API host (%s).\nCheck your internet connection or proxy settings", err, dnsErr.Name)
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) && netErr.Op == "dial" {
		return fmt.Errorf("%w\n\nCould not connect to the API.\nCheck your internet connection or proxy settings", err)
	}
	return err
}

func enrichAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w\n\nYour Anthropic API key is invalid or lacks permission.\nCheck ANTHROPIC_API_KEY or 'anthropic_api_key' in ~/.ai-mr-comment.toml", err)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w\n\nYou have hit the Anthropic rate limit. Wait a moment and try again", err)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		529:
		return fmt.Errorf("%w\n\nThe Anthropic API returned a server error. This is usually transient - try again in a moment", err)
	}
	return enrichNetworkError(err)
}

func enrichOpenAIError(err error) error {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return enrichNetworkError(err)
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w\n\nYour OpenAI API key is invalid or lacks permission.\nCheck OPENAI_API_KEY or 'openai_api_key' in ~/.ai-mr-comment.toml", err)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w\n\nYou have hit the OpenAI rate limit. Wait a moment and try again", err)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%w\n\nThe OpenAI API returned a server error. This is usually transient - try again in a moment", err)
	}
	return enrichNetworkError(err)
}

// CallAnthropic sends a message request to Anthropic.
func CallAnthropic(ctx context.Context, client *anthropic.Client, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(cfg.AnthropicModel),
		MaxTokens: 4000,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					anthropic.NewTextBlock(diffContent),
				},
			},
		},
	})
	if err != nil {
		return "", enrichAnthropicError(err)
	}
	if len(resp.Content) == 0 {
		return "", errors.New("no content returned")
	}
	block := resp.Content[0]
	if block.Type != "text" {
		return "", errors.New("first content block is not text")
	}
	return block.Text, nil
}

// CallOllama sends a generation request to the Ollama local API.
func CallOllama(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	reqBody := map[string]any{
		"model":  cfg.OllamaModel,
		"prompt": systemPrompt + "\n" + diffContent,
		"stream": false,
	}

	buf, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.OllamaEndpoint, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", ollamaAPIError(resp)
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Response, nil
}

func ollamaAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return fmt.Errorf("ollama API error: %s - %s", resp.Status, errResp.Error)
	}
	if len(body) > 0 {
		return fmt.Errorf("ollama API error: %s - %s", resp.Status, string(body))
	}
	return fmt.Errorf("ollama API error: %s", resp.Status)
}

// CallGemini sends a content generation request to Gemini.
func CallGemini(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	client, err := GetGeminiClient(ctx, cfg.GeminiAPIKey)
	if err != nil {
		return "", err
	}

	model := client.GenerativeModel(cfg.GeminiModel)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}

	resp, err := model.GenerateContent(ctx, genai.Text(diffContent))
	if err != nil {
		return "", enrichNetworkError(err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("no content returned from Gemini")
	}

	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			sb.WriteString(string(txt))
		}
	}
	return sb.String(), nil
}

// CLIPrompt combines the system prompt and diff, stripping null bytes.
func CLIPrompt(systemPrompt, diffContent string) string {
	return strings.ReplaceAll(systemPrompt+"\n\n"+diffContent, "\x00", "")
}

func CLIExecError(ctx context.Context, name string, err error, stderr string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", name, ctx.Err())
	}
	if msg := strings.TrimSpace(stderr); msg != "" {
		return fmt.Errorf("%s: %s", name, msg)
	}
	return fmt.Errorf("%s: %w", name, err)
}

func ExecCLI(ctx context.Context, binary string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // G204: binary is resolved from explicit config/PATH for supported local AI CLIs.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		combined := stderr.String()
		if strings.TrimSpace(combined) == "" {
			combined = string(out)
		}
		return "", CLIExecError(ctx, filepath.Base(binary), err, combined)
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", fmt.Errorf("%s returned empty output", filepath.Base(binary))
	}
	return result, nil
}

func StreamExecCLI(ctx context.Context, binary string, args []string, w io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // G204: binary is resolved from explicit config/PATH for supported local AI CLIs.
	var sb strings.Builder
	var stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(w, &sb)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", CLIExecError(ctx, filepath.Base(binary), err, stderr.String())
	}
	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("%s returned empty output", filepath.Base(binary))
	}
	return result, nil
}

// FindClaudeBinary returns the path to the claude CLI binary.
func FindClaudeBinary(cfg *config.Config) (string, error) {
	if cfg.ClaudeCLIPath != "" {
		if _, err := os.Stat(cfg.ClaudeCLIPath); err != nil {
			return "", fmt.Errorf("claude CLI not found at configured path %q: %w", cfg.ClaudeCLIPath, err)
		}
		return cfg.ClaudeCLIPath, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		local := filepath.Join(home, ".claude", "local", "claude")
		if _, err := os.Stat(local); err == nil {
			return local, nil
		}
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", errors.New("claude CLI not found: install Claude Code or set 'claude_cli_path' in ~/.ai-mr-comment.toml")
	}
	return path, nil
}

func ClaudeCLIArgs(cfg *config.Config, prompt string) []string {
	args := []string{"--output-format", "text"}
	if cfg.ClaudeCLIModel != "" {
		args = append(args, "--model", cfg.ClaudeCLIModel)
	}
	return append(args, "-p", prompt)
}

func CallClaudeCLI(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	binary, err := FindClaudeBinary(cfg)
	if err != nil {
		return "", err
	}
	return ExecCLI(ctx, binary, ClaudeCLIArgs(cfg, CLIPrompt(systemPrompt, diffContent)))
}

func StreamClaudeCLI(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	binary, err := FindClaudeBinary(cfg)
	if err != nil {
		return "", err
	}
	return StreamExecCLI(ctx, binary, ClaudeCLIArgs(cfg, CLIPrompt(systemPrompt, diffContent)), w)
}

func FindGeminiCLIBinary(cfg *config.Config) (string, error) {
	if cfg.GeminiCLIPath != "" {
		if _, err := os.Stat(cfg.GeminiCLIPath); err != nil {
			return "", fmt.Errorf("gemini CLI not found at configured path %q: %w", cfg.GeminiCLIPath, err)
		}
		return cfg.GeminiCLIPath, nil
	}
	path, err := exec.LookPath("gemini")
	if err != nil {
		return "", errors.New("gemini CLI not found: install via 'npm install -g @google/gemini-cli' or set 'gemini_cli_path' in ~/.ai-mr-comment.toml")
	}
	return path, nil
}

func GeminiCLIArgs(cfg *config.Config, prompt string) []string {
	var args []string
	if cfg.GeminiCLIModel != "" {
		args = append(args, "--model", cfg.GeminiCLIModel)
	}
	return append(args, "-p", prompt)
}

func CallGeminiCLI(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	binary, err := FindGeminiCLIBinary(cfg)
	if err != nil {
		return "", err
	}
	return ExecCLI(ctx, binary, GeminiCLIArgs(cfg, CLIPrompt(systemPrompt, diffContent)))
}

func StreamGeminiCLI(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	binary, err := FindGeminiCLIBinary(cfg)
	if err != nil {
		return "", err
	}
	return StreamExecCLI(ctx, binary, GeminiCLIArgs(cfg, CLIPrompt(systemPrompt, diffContent)), w)
}

func FindCodexBinary(cfg *config.Config) (string, error) {
	if cfg.CodexCLIPath != "" {
		if _, err := os.Stat(cfg.CodexCLIPath); err != nil {
			return "", fmt.Errorf("codex CLI not found at configured path %q: %w", cfg.CodexCLIPath, err)
		}
		return cfg.CodexCLIPath, nil
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", errors.New("codex CLI not found: install via 'npm install -g @openai/codex' or set 'codex_cli_path' in ~/.ai-mr-comment.toml")
	}
	return path, nil
}

func CodexCLIArgs(cfg *config.Config, prompt string) []string {
	args := []string{"exec"}
	if cfg.CodexCLIModel != "" {
		args = append(args, "-m", cfg.CodexCLIModel)
	}
	return append(args, prompt)
}

func CallCodexCLI(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string) (string, error) {
	binary, err := FindCodexBinary(cfg)
	if err != nil {
		return "", err
	}
	return ExecCLI(ctx, binary, CodexCLIArgs(cfg, CLIPrompt(systemPrompt, diffContent)))
}

func StreamCodexCLI(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	binary, err := FindCodexBinary(cfg)
	if err != nil {
		return "", err
	}
	return StreamExecCLI(ctx, binary, CodexCLIArgs(cfg, CLIPrompt(systemPrompt, diffContent)), w)
}

// ValidateAPIKey returns an error if the required API key for provider is missing.
func ValidateAPIKey(provider config.Provider, cfg *config.Config) error {
	switch provider {
	case config.OpenAI:
		if cfg.OpenAIAPIKey == "" {
			return errors.New("no OpenAI API key found\n\nSet the OPENAI_API_KEY environment variable or add 'openai_api_key' to ~/.ai-mr-comment.toml")
		}
	case config.Anthropic:
		if cfg.AnthropicAPIKey == "" {
			return errors.New("no Anthropic API key found\n\nSet the ANTHROPIC_API_KEY environment variable or add 'anthropic_api_key' to ~/.ai-mr-comment.toml")
		}
	case config.Gemini:
		if cfg.GeminiAPIKey == "" {
			return errors.New("no Gemini API key found\n\nSet the GEMINI_API_KEY environment variable or add 'gemini_api_key' to ~/.ai-mr-comment.toml")
		}
	}
	return nil
}

func StreamOpenAI(ctx context.Context, client *openai.Client, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	stream := client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model:           cfg.OpenAIModel,
		Instructions:    param.NewOpt(systemPrompt),
		Input:           responses.ResponseNewParamsInputUnion{OfString: param.NewOpt(diffContent)},
		MaxOutputTokens: param.NewOpt(int64(4000)),
		Store:           param.NewOpt(false),
		Truncation:      responses.ResponseNewParamsTruncationAuto,
	})
	defer func() { _ = stream.Close() }()

	var sb strings.Builder
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			token := event.Delta.OfString
			if token == "" {
				token = event.Text
			}
			_, _ = fmt.Fprint(w, token)
			sb.WriteString(token)
		case "error":
			return "", fmt.Errorf("OpenAI stream error: %s", event.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return "", enrichOpenAIError(err)
	}
	return sb.String(), nil
}

func StreamAnthropic(ctx context.Context, client *anthropic.Client, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(cfg.AnthropicModel),
		MaxTokens: 4000,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					anthropic.NewTextBlock(diffContent),
				},
			},
		},
	})
	defer func() { _ = stream.Close() }()

	var sb strings.Builder
	for stream.Next() {
		event := stream.Current()
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok && delta.Delta.Type == "text_delta" {
			token := delta.Delta.AsTextDelta().Text
			_, _ = fmt.Fprint(w, token)
			sb.WriteString(token)
		}
	}
	if err := stream.Err(); err != nil {
		return "", enrichAnthropicError(err)
	}
	return sb.String(), nil
}

func StreamGemini(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	client, err := GetGeminiClient(ctx, cfg.GeminiAPIKey)
	if err != nil {
		return "", err
	}

	model := client.GenerativeModel(cfg.GeminiModel)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}

	iter := model.GenerateContentStream(ctx, genai.Text(diffContent))

	var sb strings.Builder
	for {
		resp, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return "", enrichNetworkError(err)
		}
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			continue
		}
		for _, part := range resp.Candidates[0].Content.Parts {
			if txt, ok := part.(genai.Text); ok {
				token := string(txt)
				_, _ = fmt.Fprint(w, token)
				sb.WriteString(token)
			}
		}
	}
	return sb.String(), nil
}

func StreamOllama(ctx context.Context, cfg *config.Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	reqBody := map[string]any{
		"model":  cfg.OllamaModel,
		"prompt": systemPrompt + "\n" + diffContent,
		"stream": true,
	}

	buf, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.OllamaEndpoint, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", ollamaAPIError(resp)
	}

	var sb strings.Builder
	var chunk struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		chunk.Response = ""
		chunk.Done = false
		if err := json.Unmarshal(line, &chunk); err != nil {
			return "", fmt.Errorf("decoding ollama stream chunk: %w", err)
		}
		_, _ = fmt.Fprint(w, chunk.Response)
		sb.WriteString(chunk.Response)
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}
