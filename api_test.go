package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
