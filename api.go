package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/generative-ai-go/genai"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// geminiClientOptions allows tests to inject a custom endpoint and HTTP client
// for the Gemini SDK without modifying call sites.
var geminiClientOptions []option.ClientOption

// geminiClient caches a single Gemini SDK client per process. The client is
// safe for concurrent use; creating one per call would waste TLS handshakes.
var (
	geminiCachedClient    *genai.Client
	geminiCachedClientKey string
	geminiClientMu        sync.Mutex
)

// getGeminiClient returns a cached *genai.Client for apiKey, creating it on
// first use. If the API key changes (rare in practice) the cache is refreshed.
func getGeminiClient(ctx context.Context, apiKey string) (*genai.Client, error) {
	geminiClientMu.Lock()
	defer geminiClientMu.Unlock()

	if geminiCachedClient != nil && geminiCachedClientKey == apiKey && len(geminiClientOptions) == 0 {
		return geminiCachedClient, nil
	}

	// Build a fresh client (key changed, first call, or test options injected).
	opts := []option.ClientOption{option.WithAPIKey(apiKey)}
	opts = append(opts, geminiClientOptions...)
	client, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	// Close the old client if we're replacing it.
	if geminiCachedClient != nil {
		_ = geminiCachedClient.Close()
	}
	geminiCachedClient = client
	geminiCachedClientKey = apiKey
	return client, nil
}

// callOpenAI sends a chat completion request to the OpenAI API and returns the
// generated message content.
func callOpenAI(ctx context.Context, client *openai.Client, cfg *Config, systemPrompt, diffContent string) (string, error) {
	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: cfg.OpenAIModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(diffContent),
		},
		Temperature: param.NewOpt(0.7),
		MaxTokens:   param.NewOpt(int64(4000)),
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no choices returned")
	}
	return resp.Choices[0].Message.Content, nil
}

// callAnthropic sends a message request to the Anthropic API and returns the
// first text block from the response.
func callAnthropic(ctx context.Context, client *anthropic.Client, cfg *Config, systemPrompt, diffContent string) (string, error) {
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
		return "", err
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

// callOllama sends a generation request to the Ollama local API and returns
// the response text.
func callOllama(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
			return "", fmt.Errorf("ollama API error: %s - %s", resp.Status, errResp.Error)
		}
		if len(body) > 0 {
			return "", fmt.Errorf("ollama API error: %s - %s", resp.Status, string(body))
		}
		return "", fmt.Errorf("ollama API error: %s", resp.Status)
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Response, nil
}

// callGemini sends a content generation request to the Google Gemini API and
// returns the concatenated text from all response parts.
func callGemini(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	client, err := getGeminiClient(ctx, cfg.GeminiAPIKey)
	if err != nil {
		return "", err
	}

	model := client.GenerativeModel(cfg.GeminiModel)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}

	resp, err := model.GenerateContent(ctx, genai.Text(diffContent))
	if err != nil {
		return "", err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
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

// chatCompletions dispatches a prompt and diff to the appropriate provider and
// returns the generated comment.
func chatCompletions(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
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
			anthropicopt.WithBaseURL(cfg.AnthropicEndpoint),
		)
		return callAnthropic(ctx, &client, cfg, systemPrompt, diffContent)
	case Ollama:
		debugLog(cfg, "api: calling ollama model=%s endpoint=%s mode=buffered", cfg.OllamaModel, cfg.OllamaEndpoint)
		return callOllama(ctx, cfg, systemPrompt, diffContent)
	case Gemini:
		debugLog(cfg, "api: calling gemini model=%s mode=buffered", cfg.GeminiModel)
		return callGemini(ctx, cfg, systemPrompt, diffContent)
	default:
		return "", errors.New("unsupported provider")
	}
}

// streamToWriter streams tokens from the AI provider to w as they arrive and
// returns the full accumulated response. It is used when stdout is a TTY and
// text output is selected. Callers should fall back to chatCompletions on error.
func streamToWriter(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string, w io.Writer) (string, error) {
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
			anthropicopt.WithBaseURL(cfg.AnthropicEndpoint),
		)
		return streamAnthropic(ctx, &client, cfg, systemPrompt, diffContent, w)
	case Ollama:
		debugLog(cfg, "api: calling ollama model=%s endpoint=%s mode=stream", cfg.OllamaModel, cfg.OllamaEndpoint)
		return streamOllama(ctx, cfg, systemPrompt, diffContent, w)
	case Gemini:
		debugLog(cfg, "api: calling gemini model=%s mode=stream", cfg.GeminiModel)
		return streamGemini(ctx, cfg, systemPrompt, diffContent, w)
	default:
		return "", errors.New("unsupported provider")
	}
}

// streamOpenAI streams a chat completion from OpenAI, writing each token to w.
func streamOpenAI(ctx context.Context, client *openai.Client, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: cfg.OpenAIModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(diffContent),
		},
		Temperature: param.NewOpt(0.7),
		MaxTokens:   param.NewOpt(int64(4000)),
	})
	defer func() { _ = stream.Close() }()

	var sb strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			token := chunk.Choices[0].Delta.Content
			_, _ = fmt.Fprint(w, token)
			sb.WriteString(token)
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// streamAnthropic streams a message from Anthropic, writing each text token to w.
func streamAnthropic(ctx context.Context, client *anthropic.Client, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
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
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			// Only text_delta events carry text; other delta types (input_json_delta,
			// thinking_delta, etc.) are silently skipped.
			if delta.Delta.Type == "text_delta" {
				token := delta.Delta.AsTextDelta().Text
				_, _ = fmt.Fprint(w, token)
				sb.WriteString(token)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// streamGemini streams a content generation response from Gemini, writing each
// text part to w.
func streamGemini(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
	client, err := getGeminiClient(ctx, cfg.GeminiAPIKey)
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
			return "", err
		}
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
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

// streamOllama streams a generation response from the Ollama local API,
// writing each response token to w.
func streamOllama(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) (string, error) {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
			return "", fmt.Errorf("ollama API error: %s - %s", resp.Status, errResp.Error)
		}
		if len(body) > 0 {
			return "", fmt.Errorf("ollama API error: %s - %s", resp.Status, string(body))
		}
		return "", fmt.Errorf("ollama API error: %s", resp.Status)
	}

	var sb strings.Builder
	var chunk struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		chunk.Response = ""
		chunk.Done = false
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
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
