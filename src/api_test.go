package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	openai "github.com/openai/openai-go"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

type mockOpenAIClient struct {
	mock.Mock
}

func (m *mockOpenAIClient) New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*openai.ChatCompletion), args.Error(1)
}

type mockAnthropicClient struct {
	mock.Mock
}

func (m *mockAnthropicClient) New(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*anthropic.Message), args.Error(1)
}

func TestCallOpenAIChatCompletions_WithTestify(t *testing.T) {
	client := new(mockOpenAIClient)
	client.On("New", mock.Anything, mock.Anything).Return(&openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatCompletionMessage{Content: "test-reply"}},
		},
	}, nil)

	cfg := &Config{OpenAIModel: "gpt-4"}
	result, err := callOpenAIChatCompletions(client, cfg, "sys", "diff")

	require.NoError(t, err)
	require.Equal(t, "test-reply", result)
	client.AssertExpectations(t)
}

func TestCallAnthropicMessages_WithTestify(t *testing.T) {
	client := new(mockAnthropicClient)
	client.On("New", mock.Anything, mock.Anything).Return(&anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			{
				Type: "text",
				Text: "claude-output",
			},
		},
	}, nil)

	cfg := &Config{AnthropicModel: "claude-3"}

	result, err := callAnthropicMessages(client, cfg, "prompt", "diff")

	require.NoError(t, err)
	require.Equal(t, "claude-output", result)
	client.AssertExpectations(t)
}

func TestEstimateTokens(t *testing.T) {
	text := "This is a short sentence."
	tokens := estimateTokens(text)
	if tokens <= 0 {
		t.Errorf("Expected positive token count, got %d", tokens)
	}
}

func TestEstimateDebugOutput(t *testing.T) {
	text := strings.Repeat("x", 3500) // ~1000 tokens
	tokens := estimateTokens(text)
	if tokens < 900 || tokens > 1100 {
		t.Errorf("Unexpected token estimate: %d", tokens)
	}
}
