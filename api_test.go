package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"google.golang.org/api/option"
)

// --- callOpenAI tests ---

func TestCallOpenAI_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]string{"role": "assistant", "content": "openai response"}, "finish_reason": "stop"},
			},
		})
	}))
	defer ts.Close()

	client := openai.NewClient(openaiopt.WithBaseURL(ts.URL+"/v1/"), openaiopt.WithAPIKey("test"))
	cfg := &Config{OpenAIModel: "gpt-4o-mini"}

	result, err := callOpenAI(context.Background(), &client, cfg, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "openai response" {
		t.Errorf("expected 'openai response', got %q", result)
	}
}

func TestCallOpenAI_NoChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{},
		})
	}))
	defer ts.Close()

	client := openai.NewClient(openaiopt.WithBaseURL(ts.URL+"/v1/"), openaiopt.WithAPIKey("test"))
	cfg := &Config{OpenAIModel: "gpt-4o-mini"}

	_, err := callOpenAI(context.Background(), &client, cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error for no choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected 'no choices' error, got %q", err.Error())
	}
}

func TestCallOpenAI_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"server error","type":"server_error"}}`))
	}))
	defer ts.Close()

	client := openai.NewClient(openaiopt.WithBaseURL(ts.URL+"/v1/"), openaiopt.WithAPIKey("test"))
	cfg := &Config{OpenAIModel: "gpt-4o-mini"}

	_, err := callOpenAI(context.Background(), &client, cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error from API")
	}
}

// --- callAnthropic tests ---

func TestCallAnthropic_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test",
			"content":     []map[string]string{{"type": "text", "text": "anthropic response"}},
			"stop_reason": "end_turn",
		})
	}))
	defer ts.Close()

	client := anthropic.NewClient(anthropicopt.WithBaseURL(ts.URL), anthropicopt.WithAPIKey("test"))
	cfg := &Config{AnthropicModel: "claude-3-5-sonnet-20240620"}

	result, err := callAnthropic(context.Background(), &client, cfg, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "anthropic response" {
		t.Errorf("expected 'anthropic response', got %q", result)
	}
}

func TestCallAnthropic_NoContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test",
			"content":     []map[string]string{},
			"stop_reason": "end_turn",
		})
	}))
	defer ts.Close()

	client := anthropic.NewClient(anthropicopt.WithBaseURL(ts.URL), anthropicopt.WithAPIKey("test"))
	cfg := &Config{AnthropicModel: "claude-3-5-sonnet-20240620"}

	_, err := callAnthropic(context.Background(), &client, cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error for no content")
	}
	if !strings.Contains(err.Error(), "no content") {
		t.Errorf("expected 'no content' error, got %q", err.Error())
	}
}

// --- callGemini tests ---

func TestCallGemini_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "streamGenerateContent") || strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{"content": map[string]any{
						"parts": []map[string]string{{"text": "gemini response"}},
						"role":  "model",
					}},
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	geminiClientOptions = []option.ClientOption{
		option.WithEndpoint(ts.URL),
		option.WithHTTPClient(ts.Client()),
	}
	defer func() { geminiClientOptions = nil }()

	cfg := &Config{GeminiAPIKey: "test", GeminiModel: "gemini-2.5-flash"}

	result, err := callGemini(context.Background(), cfg, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "gemini response" {
		t.Errorf("expected 'gemini response', got %q", result)
	}
}

func TestCallAnthropic_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"server error"}}`))
	}))
	defer ts.Close()

	client := anthropic.NewClient(anthropicopt.WithBaseURL(ts.URL), anthropicopt.WithAPIKey("test"))
	cfg := &Config{AnthropicModel: "claude-3-5-sonnet-20240620"}

	_, err := callAnthropic(context.Background(), &client, cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error from API")
	}
}

func TestCallAnthropic_NonTextBlock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test",
			"content":     []map[string]string{{"type": "image", "text": ""}},
			"stop_reason": "end_turn",
		})
	}))
	defer ts.Close()

	client := anthropic.NewClient(anthropicopt.WithBaseURL(ts.URL), anthropicopt.WithAPIKey("test"))
	cfg := &Config{AnthropicModel: "claude-3-5-sonnet-20240620"}

	_, err := callAnthropic(context.Background(), &client, cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error for non-text block")
	}
	if !strings.Contains(err.Error(), "not text") {
		t.Errorf("expected 'not text' error, got %q", err.Error())
	}
}

func TestCallGemini_NoContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{},
		})
	}))
	defer ts.Close()

	geminiClientOptions = []option.ClientOption{
		option.WithEndpoint(ts.URL),
		option.WithHTTPClient(ts.Client()),
	}
	defer func() { geminiClientOptions = nil }()

	cfg := &Config{GeminiAPIKey: "test", GeminiModel: "gemini-2.5-flash"}

	_, err := callGemini(context.Background(), cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error for no content")
	}
	if !strings.Contains(err.Error(), "no content") {
		t.Errorf("expected 'no content' error, got %q", err.Error())
	}
}

func TestCallGemini_NilCandidateContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{},
			},
		})
	}))
	defer ts.Close()

	geminiClientOptions = []option.ClientOption{
		option.WithEndpoint(ts.URL),
		option.WithHTTPClient(ts.Client()),
	}
	defer func() { geminiClientOptions = nil }()

	cfg := &Config{GeminiAPIKey: "test", GeminiModel: "gemini-2.5-flash"}

	_, err := callGemini(context.Background(), cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error for nil candidate content")
	}
	if !strings.Contains(err.Error(), "no content") {
		t.Errorf("expected 'no content' error, got %q", err.Error())
	}
}

// --- callOllama tests ---

func TestCallOllama_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "test comment"})
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	result, err := callOllama(context.Background(), cfg, "system prompt", "diff content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "test comment" {
		t.Errorf("expected 'test comment', got %q", result)
	}
}

func TestCallOllama_ErrorWithJSONBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad model"})
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	_, err := callOllama(context.Background(), cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Errorf("expected error to contain 'bad model', got %q", err.Error())
	}
}

func TestCallOllama_ErrorWithPlainBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	_, err := callOllama(context.Background(), cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Errorf("expected error to contain 'internal server error', got %q", err.Error())
	}
}

func TestCallOllama_ErrorWithEmptyBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	_, err := callOllama(context.Background(), cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to contain status code, got %q", err.Error())
	}
}

func TestCallOllama_BadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	_, err := callOllama(context.Background(), cfg, "prompt", "diff")
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// --- chatCompletions tests ---

func TestChatCompletions_UnsupportedProvider(t *testing.T) {
	cfg := &Config{Provider: "invalid"}
	_, err := chatCompletions(context.Background(), cfg, "invalid", "prompt", "diff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected 'unsupported provider' error, got %q", err.Error())
	}
}

func TestChatCompletions_Ollama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "ollama comment"})
	}))
	defer ts.Close()

	cfg := &Config{
		Provider:       Ollama,
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	result, err := chatCompletions(context.Background(), cfg, Ollama, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ollama comment" {
		t.Errorf("expected 'ollama comment', got %q", result)
	}
}

func TestChatCompletions_OpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]string{"role": "assistant", "content": "openai via chat"}, "finish_reason": "stop"},
			},
		})
	}))
	defer ts.Close()

	cfg := &Config{
		Provider:       OpenAI,
		OpenAIAPIKey:   "test",
		OpenAIModel:    "gpt-4o-mini",
		OpenAIEndpoint: ts.URL + "/v1/",
	}

	result, err := chatCompletions(context.Background(), cfg, OpenAI, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "openai via chat" {
		t.Errorf("expected 'openai via chat', got %q", result)
	}
}

func TestChatCompletions_Anthropic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test",
			"content":     []map[string]string{{"type": "text", "text": "anthropic via chat"}},
			"stop_reason": "end_turn",
		})
	}))
	defer ts.Close()

	cfg := &Config{
		Provider:          Anthropic,
		AnthropicAPIKey:   "test",
		AnthropicModel:    "claude-3-5-sonnet-20240620",
		AnthropicEndpoint: ts.URL,
	}

	result, err := chatCompletions(context.Background(), cfg, Anthropic, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "anthropic via chat" {
		t.Errorf("expected 'anthropic via chat', got %q", result)
	}
}

func TestChatCompletions_Gemini(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{
					"parts": []map[string]string{{"text": "gemini via chat"}},
					"role":  "model",
				}},
			},
		})
	}))
	defer ts.Close()

	geminiClientOptions = []option.ClientOption{
		option.WithEndpoint(ts.URL),
		option.WithHTTPClient(ts.Client()),
	}
	defer func() { geminiClientOptions = nil }()

	cfg := &Config{
		Provider:     Gemini,
		GeminiAPIKey: "test",
		GeminiModel:  "gemini-2.5-flash",
	}

	result, err := chatCompletions(context.Background(), cfg, Gemini, "prompt", "diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "gemini via chat" {
		t.Errorf("expected 'gemini via chat', got %q", result)
	}
}

// --- streaming tests ---

func TestStreamOpenAI_Success(t *testing.T) {
	// OpenAI streaming uses SSE (text/event-stream).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{"Hello", " world", "!"}
		for _, c := range chunks {
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", c)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	client := openai.NewClient(
		openaiopt.WithAPIKey("test"),
		openaiopt.WithBaseURL(ts.URL+"/"),
	)
	cfg := &Config{OpenAIModel: "gpt-4o-mini"}

	var buf strings.Builder
	result, err := streamOpenAI(context.Background(), &client, cfg, "sys", "diff", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", result)
	}
	if buf.String() != "Hello world!" {
		t.Errorf("expected writer to receive 'Hello world!', got %q", buf.String())
	}
}

func TestStreamOllama_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "{\"response\":\"chunk1\",\"done\":false}\n")
		_, _ = fmt.Fprint(w, "{\"response\":\" chunk2\",\"done\":true}\n")
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	var buf strings.Builder
	result, err := streamOllama(context.Background(), cfg, "sys", "diff", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "chunk1 chunk2" {
		t.Errorf("expected 'chunk1 chunk2', got %q", result)
	}
	if buf.String() != "chunk1 chunk2" {
		t.Errorf("expected writer to receive 'chunk1 chunk2', got %q", buf.String())
	}
}

func TestStreamOllama_LargeChunkLine(t *testing.T) {
	largeToken := strings.Repeat("a", 70*1024)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "{\"response\":%q,\"done\":true}\n", largeToken)
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	var buf strings.Builder
	result, err := streamOllama(context.Background(), cfg, "sys", "diff", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != largeToken {
		t.Errorf("expected large token response, got len=%d", len(result))
	}
	if buf.Len() != len(largeToken) {
		t.Errorf("expected writer len=%d, got %d", len(largeToken), buf.Len())
	}
}

func TestStreamOllama_InvalidChunkFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "{\"response\":\"ok\",\"done\":false}\n")
		_, _ = fmt.Fprint(w, "{invalid json}\n")
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	_, err := streamOllama(context.Background(), cfg, "sys", "diff", io.Discard)
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decoding ollama stream chunk") {
		t.Fatalf("expected decode error context, got: %v", err)
	}
}

func TestStreamOllama_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	cfg := &Config{
		OllamaModel:    "llama3",
		OllamaEndpoint: ts.URL,
	}

	_, err := streamOllama(context.Background(), cfg, "sys", "diff", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ollama API error") {
		t.Errorf("expected ollama API error, got %v", err)
	}
}

func TestStreamAnthropic_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3-5-sonnet-20240620\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" stream\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer ts.Close()

	client := anthropic.NewClient(
		anthropicopt.WithAPIKey("test"),
		anthropicopt.WithBaseURL(ts.URL),
	)
	cfg := &Config{AnthropicModel: "claude-3-5-sonnet-20240620"}

	var buf strings.Builder
	result, err := streamAnthropic(context.Background(), &client, cfg, "sys", "diff", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello stream" {
		t.Errorf("expected 'Hello stream', got %q", result)
	}
	if buf.String() != "Hello stream" {
		t.Errorf("expected writer to receive 'Hello stream', got %q", buf.String())
	}
}

func TestStreamToWriter_UnsupportedProvider(t *testing.T) {
	cfg := &Config{}
	_, err := streamToWriter(context.Background(), cfg, "unknown", "sys", "diff", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected unsupported provider error, got %v", err)
	}
}

func TestGetOllamaHTTPTimeout_Default(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", "")
	got := getOllamaHTTPTimeout()
	if got != defaultOllamaHTTPTimeout {
		t.Fatalf("expected default timeout %v, got %v", defaultOllamaHTTPTimeout, got)
	}
}

func TestGetOllamaHTTPTimeout_Override(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", "300000")
	got := getOllamaHTTPTimeout()
	if got != 5*time.Minute {
		t.Fatalf("expected 5m timeout, got %v", got)
	}
}

func TestGetOllamaHTTPTimeout_InvalidFallback(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", "not-a-number")
	got := getOllamaHTTPTimeout()
	if got != defaultOllamaHTTPTimeout {
		t.Fatalf("expected fallback timeout %v, got %v", defaultOllamaHTTPTimeout, got)
	}
}

func TestGetOllamaHTTPTimeout_NonPositiveFallback(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", "0")
	got := getOllamaHTTPTimeout()
	if got != defaultOllamaHTTPTimeout {
		t.Fatalf("expected fallback timeout %v, got %v", defaultOllamaHTTPTimeout, got)
	}
}

func TestGetOllamaHTTPTimeout_WhitespaceTrimmed(t *testing.T) {
	t.Setenv("AI_MR_COMMENT_OLLAMA_TIMEOUT_MS", " 120000 ")
	got := getOllamaHTTPTimeout()
	if got != 2*time.Minute {
		t.Fatalf("expected 2m timeout, got %v", got)
	}
}
