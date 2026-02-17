package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/generative-ai-go/genai"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"google.golang.org/api/option"
)

var geminiClientOptions []option.ClientOption

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

func callGemini(ctx context.Context, cfg *Config, systemPrompt, diffContent string) (string, error) {
	opts := []option.ClientOption{option.WithAPIKey(cfg.GeminiAPIKey)}
	opts = append(opts, geminiClientOptions...)

	client, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()

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

	var result string
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			result += string(txt)
		}
	}
	return result, nil
}

func chatCompletions(ctx context.Context, cfg *Config, provider ApiProvider, systemPrompt, diffContent string) (string, error) {
	switch provider {
	case OpenAI:
		client := openai.NewClient(
			openaiopt.WithAPIKey(cfg.OpenAIAPIKey),
		)
		return callOpenAI(ctx, &client, cfg, systemPrompt, diffContent)
	case Anthropic:
		client := anthropic.NewClient(
			anthropicopt.WithAPIKey(cfg.AnthropicAPIKey),
		)
		return callAnthropic(ctx, &client, cfg, systemPrompt, diffContent)
	case Ollama:
		return callOllama(ctx, cfg, systemPrompt, diffContent)
	case Gemini:
		return callGemini(ctx, cfg, systemPrompt, diffContent)
	default:
		return "", errors.New("unsupported provider")
	}
}

func estimateTokens(text string) int {
	// Anthropic counts ~4 chars per token, OpenAI ~3.5 - we'll use conservative estimate
	return int(math.Ceil(float64(len(text)) / 3.5))
}
