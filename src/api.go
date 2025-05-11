package main

import (
	"context"
	_ "embed"
	"errors"
	"math"

	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
)

type OpenAIChatClient interface {
	New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
}

type AnthropicMessagesClient interface {
	New(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
}

type realAnthropicClient struct {
	client *anthropic.Client
}

func (r *realAnthropicClient) New(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return r.client.Messages.New(ctx, params)
}

type realOpenAIClient struct {
	client *openai.Client
}

func (r *realOpenAIClient) New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return r.client.Chat.Completions.New(ctx, params)
}

func callOpenAIChatCompletions(client OpenAIChatClient, cfg *Config, prompt, diff string) (string, error) {
	resp, err := client.New(context.TODO(), openai.ChatCompletionNewParams{
		Model: cfg.OpenAIModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(prompt),
			openai.UserMessage(diff),
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

func callAnthropicMessages(client AnthropicMessagesClient, cfg *Config, prompt, diff string) (string, error) {
	resp, err := client.New(context.TODO(), anthropic.MessageNewParams{
		Model:     cfg.AnthropicModel,
		MaxTokens: 4000,
		System: []anthropic.TextBlockParam{
			{Text: prompt},
		},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{
						OfRequestTextBlock: &anthropic.TextBlockParam{
							Text: diff,
						},
					},
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

func chatCompletions(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
	if provider == OpenAI {
		// defaults to os.LookupEnv("OPENAI_API_KEY")
		client := openai.NewClient(
			openaiopt.WithAPIKey(cfg.OpenAIKey),
		)
		return callOpenAIChatCompletions(&realOpenAIClient{client: &client}, cfg, prompt, diff)
	} else if provider == Anthropic {
		// defaults to os.LookupEnv("ANTHROPIC_API_KEY")
		client := anthropic.NewClient(
			anthropicopt.WithAPIKey(cfg.AnthropicKey),
		)
		return callAnthropicMessages(&realAnthropicClient{client: &client}, cfg, prompt, diff)
	}
	return "", errors.New("unsupported provider")
}

func estimateTokens(text string) int {
	// Anthropic counts ~4 chars per token, OpenAI ~3.5 - we'll use conservative estimate
	return int(math.Ceil(float64(len(text)) / 3.5))
}
