package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeTestExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\necho ok\n"
	if runtime.GOOS == "windows" {
		body = "@echo off\r\necho ok\r\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckCmdSuccessPrintsResolvedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, ".ai-mr-comment.toml"), []byte(`
provider = "openai"
openai_api_key = "sk-test"
openai_model = "gpt-file"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	cmd := newCheckCmd(func(_ context.Context, _ *Config, provider ApiProvider, prompt, diff string) (string, error) {
		if provider != OpenAI || prompt != checkPingPrompt || diff != "" {
			t.Fatalf("unexpected ping args: %s %q %q", provider, prompt, diff)
		}
		return " OK ", nil
	})
	cmd.SetArgs([]string{"--provider=openai", "--model=gpt-override"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("check failed: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Provider : openai", "Model    : gpt-override", "API key  : sk-t****", "Sending ping", "Response : OK"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestCheckCmdProviderDisplayBranches(t *testing.T) {
	dir := t.TempDir()
	cli := writeTestExecutable(t, dir, "ai-cli")
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name: "anthropic",
			config: `
provider = "anthropic"
anthropic_api_key = "sk-ant"
anthropic_model = "claude-test"
anthropic_endpoint = "https://anthropic.example/"
`,
			want: "Endpoint : https://anthropic.example/",
		},
		{
			name: "gemini",
			config: `
provider = "gemini"
gemini_api_key = "gem-test"
gemini_model = "gemini-test"
`,
			want: "API key  : gem-****",
		},
		{
			name: "ollama",
			config: `
provider = "ollama"
ollama_model = "llama-test"
ollama_endpoint = "http://localhost:11434/api/generate"
`,
			want: "Endpoint : http://localhost:11434/api/generate",
		},
		{
			name: "claude cli",
			config: `
provider = "claude-cli"
claude_cli_model = "claude-test"
claude_cli_path = "` + cli + `"
`,
			want: "Binary   : " + cli,
		},
		{
			name: "gemini cli",
			config: `
provider = "gemini-cli"
gemini_cli_model = "gemini-test"
gemini_cli_path = "` + cli + `"
`,
			want: "Binary   : " + cli,
		},
		{
			name: "codex cli",
			config: `
provider = "codex-cli"
codex_cli_model = "codex-test"
codex_cli_path = "` + cli + `"
`,
			want: "Binary   : " + cli,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Chdir(home)
			t.Setenv("HOME", home)
			if err := os.WriteFile(filepath.Join(home, ".ai-mr-comment.toml"), []byte(tc.config), 0o644); err != nil {
				t.Fatal(err)
			}

			var out strings.Builder
			cmd := newCheckCmd(func(context.Context, *Config, ApiProvider, string, string) (string, error) {
				return "OK", nil
			})
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("check failed: %v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("expected %q in output:\n%s", tc.want, out.String())
			}
		})
	}
}

func TestRunCheckAllReportsFailuresAndSkips(t *testing.T) {
	dir := t.TempDir()
	cli := writeTestExecutable(t, dir, "ai-cli")
	cfg := &Config{
		OpenAIAPIKey:    "openai",
		AnthropicAPIKey: "anthropic",
		GeminiAPIKey:    "gemini",
		OpenAIModel:     "gpt",
		AnthropicModel:  "claude",
		GeminiModel:     "gemini",
		OllamaModel:     "llama",
		ClaudeCLIPath:   cli,
		GeminiCLIPath:   cli,
		CodexCLIPath:    cli,
		RequestTimeout:  time.Second,
	}

	var out strings.Builder
	err := runCheckAll(context.Background(), cfg, func(_ context.Context, _ *Config, provider ApiProvider, _, _ string) (string, error) {
		if provider == Gemini {
			return "", errors.New("gemini down")
		}
		return "OK", nil
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "one or more providers failed") {
		t.Fatalf("expected aggregate failure, got %v", err)
	}
	got := out.String()
	for _, want := range []string{"Pinging all providers", "PROVIDER", "openai", "gemini", "FAIL", "tip: run 'check --provider <name>'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestDoctorCmdJSONAndText(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, ".ai-mr-comment.toml"), []byte(`
provider = "openai"
openai_api_key = "sk-test"
openai_model = "gpt-file"
template = "default"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "json", args: []string{"--format=json"}, want: `"provider":"openai"`},
		{name: "text", args: []string{"--provider=openai", "--model=gpt-override"}, want: "Provider    : openai"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			cmd := newDoctorCmd()
			cmd.SetArgs(tc.args)
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("doctor failed: %v", err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("expected %q in output:\n%s", tc.want, out.String())
			}
		})
	}
}

func TestCompleteProfilesUsesConfigProfiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, ".ai-mr-comment.toml"), []byte(`
[profile.alpha]
provider = "openai"

[profile.beta]
provider = "anthropic"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, directive := completeProfiles(nil, nil, "a")
	if directive == 0 || len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("completeProfiles = %v, %v", got, directive)
	}
}
