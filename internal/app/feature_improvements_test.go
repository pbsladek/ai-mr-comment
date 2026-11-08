package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/pbsladek/ai-mr-comment/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestInputByteLimitBoundaries(t *testing.T) {
	for _, input := range []string{"abcd", "éé"} {
		for _, limit := range []int64{3, 4, 5} {
			for _, file := range []bool{false, true} {
				t.Run(fmt.Sprintf("%q/%d/file=%v", input, limit, file), func(t *testing.T) {
					cmd := &cobra.Command{}
					cmd.SetIn(strings.NewReader(input))
					path := "-"
					if file {
						path = filepath.Join(t.TempDir(), "input.diff")
						if err := os.WriteFile(path, []byte(input), 0600); err != nil {
							t.Fatal(err)
						}
					}
					got, err := readCommandInputWithLimit(cmd, path, limit)
					if int64(len(input)) > limit {
						if err == nil || !strings.Contains(err.Error(), "max-input-bytes") {
							t.Fatalf("expected input limit error, got %q, %v", got, err)
						}
					} else if err != nil || got != input {
						t.Fatalf("input changed at boundary: %q, %v", got, err)
					}
				})
			}
		}
	}
}

func TestGenerationResourceFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	var called bool
	chat := func(_ context.Context, cfg *Config, _ ApiProvider, _, _ string) (string, error) {
		called = true
		if cfg.MaxOutputTokens != 8192 || cfg.ChunkConcurrency != 2 {
			t.Errorf("resource overrides not applied: %+v", cfg)
		}
		return "review", nil
	}
	cmd := newRootCmd(chat)
	cmd.SetArgs([]string{"--provider=openai", "--max-output-tokens=8192", "--chunk-concurrency=2", "--max-input-bytes=64"})
	cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+change\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("provider was not called")
	}
	for _, args := range [][]string{
		{"--max-input-bytes=8"}, {"--max-output-tokens=0"}, {"--chunk-concurrency=-1"}, {"--max-output-tokens=2147483648"},
	} {
		called = false
		cmd = newRootCmd(chat)
		cmd.SetArgs(append([]string{"--provider=openai"}, args...))
		cmd.SetIn(strings.NewReader("diff --git a/a b/a\n+change\n"))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected invalid limit/input failure for %v", args)
		}
		if called {
			t.Fatalf("provider called despite invalid limit/input for %v", args)
		}
	}
}

func TestSmartChunkConcurrencyBound(t *testing.T) {
	var active, peak atomic.Int32
	var calls atomic.Int32
	chat := func(ctx context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		calls.Add(1)
		if prompt == "synthesize" {
			return "review", nil
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-time.After(5 * time.Millisecond):
			return "summary", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	var diff strings.Builder
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&diff, "diff --git a/%d b/%d\n+change\n", i, i)
	}
	got, err := generateSmartChunkComment(rootGenerationRequest{Context: context.Background(), Config: &Config{ChunkConcurrency: 2}, Chat: chat, DiffContent: diff.String(), SystemPrompt: "synthesize"})
	if err != nil || got != "review" {
		t.Fatalf("generation: %q, %v", got, err)
	}
	if peak.Load() > 2 || peak.Load() == 0 || active.Load() != 0 || calls.Load() != 10 {
		t.Fatalf("concurrency=%d, active=%d, calls=%d", peak.Load(), active.Load(), calls.Load())
	}
}

func TestCheckJSONSuccessFailureAndMissingCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		name, key string
		callErr   error
		wantOK    bool
	}{
		{"success", "secret-value", nil, true}, {"failure", "secret-value", errors.New("provider unavailable"), false}, {"missing", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", tc.key)
			called := false
			cmd := newCheckCmd(func(context.Context, *Config, ApiProvider, string, string) (string, error) {
				called = true
				return "OK", tc.callErr
			})
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--provider=openai", "--model=future-model", "--format=json"})
			err := cmd.Execute()
			if (err == nil) != tc.wantOK {
				t.Fatalf("check error=%v, wantOK=%v", err, tc.wantOK)
			}
			if tc.key == "" && called {
				t.Fatal("missing credentials still triggered a ping")
			}
			var payload struct {
				OK      bool                             `json:"ok"`
				Results []struct{ Model, Status string } `json:"results"`
			}
			if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
				t.Fatalf("invalid JSON: %v: %s", err, out.String())
			}
			if payload.OK != tc.wantOK || len(payload.Results) != 1 || payload.Results[0].Model != "future-model" {
				t.Fatalf("unexpected result: %+v", payload)
			}
			if strings.Contains(out.String(), "secret-value") {
				t.Fatal("JSON output exposed credentials")
			}
		})
	}
}

func TestCheckAllJSONIncludesFailuresAndSkips(t *testing.T) {
	cfg := &Config{OpenAIAPIKey: "key", OpenAIModel: "new-model", ClaudeCLIPath: "/missing/claude", GeminiCLIPath: "/missing/gemini", CodexCLIPath: "/missing/codex"}
	var out strings.Builder
	err := runCheckAll(context.Background(), cfg, func(_ context.Context, _ *Config, p ApiProvider, _, _ string) (string, error) {
		if p == OpenAI {
			return "", errors.New("offline")
		}
		return "OK", nil
	}, &out, "json")
	if err == nil {
		t.Fatal("a failed ping must fail the check")
	}
	var payload struct {
		OK      bool                                `json:"ok"`
		Results []struct{ Provider, Status string } `json:"results"`
	}
	if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK || len(payload.Results) != len(allProviders) {
		t.Fatalf("bad check envelope: %+v", payload)
	}
	for i, result := range payload.Results {
		if result.Provider != string(allProviders[i]) {
			t.Fatal("result order changed")
		}
	}
	if payload.Results[0].Status != "fail" || payload.Results[1].Status != "skip" {
		t.Fatalf("bad statuses: %+v", payload.Results)
	}
}

func TestModelsJSONAndOllamaDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/api/tags" {
			t.Errorf("unexpected discovery URL: %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"models":[{"name":"z-model:latest"},{"name":"new-local:7b"},{"name":"new-local:7b"},{"model":"fallback:latest"}]}`)
	}))
	defer server.Close()
	t.Setenv("AI_MR_COMMENT_OLLAMA_ENDPOINT", server.URL+"/proxy/api/generate")
	for _, args := range [][]string{
		{"--provider=openai", "--format=json"}, {"--provider=anthropic", "--format=json"}, {"--provider=codex-cli", "--format=json"}, {"--provider=ollama", "--format=json", "--refresh"},
	} {
		cmd := newModelsCmd()
		var out strings.Builder
		cmd.SetArgs(args)
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Provider string   `json:"provider"`
			Models   []string `json:"models"`
			Source   string   `json:"source"`
		}
		if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
			t.Fatal(err)
		}
		switch payload.Provider {
		case "openai":
			if !slices.Contains(payload.Models, "gpt-6-astra") || !slices.Contains(payload.Models, "gpt-6.1-sol") {
				t.Fatal(payload.Models)
			}
		case "anthropic":
			if !slices.Contains(payload.Models, "claude-fable-5-1") || !slices.Contains(payload.Models, "claude-sonnet-5-5") {
				t.Fatal(payload.Models)
			}
		case "codex-cli":
			if payload.Models == nil || len(payload.Models) != 0 {
				t.Fatal("empty lists must encode as []")
			}
		case "ollama":
			if !slices.Equal(payload.Models, []string{"fallback:latest", "new-local:7b", "z-model:latest"}) || payload.Source != "ollama" {
				t.Fatalf("bad discovery result: %+v", payload)
			}
		}
	}
}

func TestOllamaDiscoveryErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{http.StatusUnauthorized, `{}`}, {http.StatusOK, `not json`}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, err := fetchOllamaModels(context.Background(), &Config{OllamaEndpoint: server.URL + "/api/generate"})
		server.Close()
		if err == nil {
			t.Fatal("bad server response must fail discovery")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchOllamaModels(ctx, &Config{OllamaEndpoint: "http://127.0.0.1:1/api/generate"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestUnknownPricingAndLocalModels(t *testing.T) {
	for _, tc := range []struct {
		provider    ApiProvider
		model, want string
	}{
		{OpenAI, "future-model", "unknown"}, {OpenAI, "hosted-llama-private", "unknown"}, {Ollama, "custom-local", "$0.000000"},
	} {
		cfg := &Config{Provider: tc.provider, OpenAIModel: tc.model, OllamaModel: tc.model}
		var out strings.Builder
		showCostEstimate(context.Background(), cfg, "system", "diff", &out)
		if !strings.Contains(out.String(), "Estimated Input Cost: "+tc.want) {
			t.Fatal(out.String())
		}
	}
}

func TestCommandsRejectUnexpectedArgumentsBeforeSideEffects(t *testing.T) {
	for _, args := range [][]string{{"unexpected"}, {"models", "unexpected"}, {"check", "unexpected"}, {"quick-commit", "unexpected"}, {"init-config", "unexpected"}, {"publish", "unexpected"}, {"changelog", "unexpected"}, {"review", "unexpected"}} {
		called := false
		cmd := newRootCmd(func(context.Context, *Config, ApiProvider, string, string) (string, error) {
			called = true
			return "", nil
		})
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("%v accepted unsupported positional args", args)
		}
		if called {
			t.Fatalf("%v called a provider", args)
		}
	}
}

func TestResourceConfigurationProfileAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("max_output_tokens=5000\n[profile.fast]\nmax_output_tokens=6000\nchunk_concurrency=2\nmax_input_bytes=1000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	v.SetEnvPrefix("AI_MR_COMMENT")
	v.AutomaticEnv()
	t.Setenv("AI_MR_COMMENT_MAX_OUTPUT_TOKENS", "7000")
	cfg, err := internalconfig.LoadWith(v, "fast")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxOutputTokens != 7000 || cfg.ChunkConcurrency != 2 || cfg.MaxInputBytes != 1000 {
		t.Fatalf("precedence failed: %+v", cfg)
	}
	for _, key := range []string{"max_output_tokens", "chunk_concurrency", "max_input_bytes"} {
		v := viper.New()
		v.SetConfigFile(filepath.Join(t.TempDir(), "missing.toml"))
		v.Set(key, 0)
		if _, err := internalconfig.LoadWith(v, ""); err == nil {
			t.Fatalf("zero %s accepted", key)
		}
	}
}

func TestSmartChunkStopsBeforeQueuedCallsOnFailureOrCancellation(t *testing.T) {
	diff := "diff --git a/a b/a\n+a\ndiff --git a/b b/b\n+b\ndiff --git a/c b/c\n+c\n"
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		calls := 0
		providerErr := errors.New("rate limited")
		_, err := generateSmartChunkComment(rootGenerationRequest{Context: ctx, Config: &Config{ChunkConcurrency: 1}, DiffContent: diff, Chat: func(context.Context, *Config, ApiProvider, string, string) (string, error) {
			calls++
			return "", providerErr
		}})
		cancel()
		if cancelled {
			if !errors.Is(err, context.Canceled) || calls != 0 {
				t.Fatalf("cancellation: error=%v calls=%d", err, calls)
			}
		} else if !errors.Is(err, providerErr) || calls != 1 {
			t.Fatalf("failure: error=%v calls=%d", err, calls)
		}
	}
}

func TestModelCompletionRespectsProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct{ provider, prefix, want string }{
		{"openai", "gpt-6", "gpt-6-astra"}, {"anthropic", "claude-sonnet", "claude-sonnet-5-5"}, {"gemini", "gemini-3.8", "gemini-3.8-flash"},
	} {
		cmd := newRootCmd(dummyChatFn)
		if err := cmd.Flags().Set("provider", tc.provider); err != nil {
			t.Fatal(err)
		}
		complete, ok := cmd.GetFlagCompletionFunc("model")
		if !ok {
			t.Fatal("model flag has no completion")
		}
		matches, _ := complete(cmd, nil, tc.prefix)
		if !slices.Contains(matches, tc.want) {
			t.Fatalf("%s completion: %v", tc.provider, matches)
		}
	}
}

func TestModelCompletionUsesNamedProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Chdir(dir)
	content := `provider="openai"
[profile.complete]
provider="anthropic"
`
	if err := os.WriteFile(filepath.Join(dir, ".ai-mr-comment.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd(dummyChatFn)
	if err := cmd.Flags().Set("profile", "complete"); err != nil {
		t.Fatal(err)
	}
	matches, _ := completeModels(cmd, nil, "claude-fable")
	if !slices.Contains(matches, "claude-fable-5-1") {
		t.Fatalf("profile completion: %v", matches)
	}
}
