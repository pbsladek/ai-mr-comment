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

func callOpenAIChatCompletions(client *openai.Client, cfg *Config, prompt, diff string) (string, error) {
	resp, err := client.Chat.Completions.New(context.TODO(), openai.ChatCompletionNewParams{
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

func callAnthropicMessages(client *anthropic.Client, cfg *Config, prompt, diff string) (string, error) {
	resp, err := client.Messages.New(context.TODO(), anthropic.MessageNewParams{
		Model:     cfg.ClaudeModel,
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

	block := resp.Content[0].AsAny()
	textBlock, ok := block.(anthropic.TextBlock)
	if !ok {
		return "", errors.New("first content block is not text")
	}

	return textBlock.Text, nil
}

func chatCompletions(cfg *Config, provider ApiProvider, prompt, diff string) (string, error) {
	if provider == OpenAI {
		// defaults to os.LookupEnv("OPENAI_API_KEY")
		client := openai.NewClient(
			openaiopt.WithAPIKey(cfg.OpenAIKey),
		)
		return callOpenAIChatCompletions(&client, cfg, prompt, diff)
	} else if provider == Claude {
		// defaults to os.LookupEnv("ANTHROPIC_API_KEY")
		client := anthropic.NewClient(
			anthropicopt.WithAPIKey(cfg.ClaudeKey),
		)
		return callAnthropicMessages(&client, cfg, prompt, diff)
	}
	return "", errors.New("unsupported provider")
}

func estimateTokens(text string) int {
	// Claude counts ~4 chars per token, OpenAI ~3.5 - we'll use conservative estimate
	return int(math.Ceil(float64(len(text)) / 3.5))
}
