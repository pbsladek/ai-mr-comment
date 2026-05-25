package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type testErrWriter struct {
	err error
}

func (w testErrWriter) Write([]byte) (int, error) { return 0, w.err }

func TestResolveRootPromptPipeline(t *testing.T) {
	result, err := resolveRootPrompt(rootPromptRequest{
		Template:     "default",
		PresetSuffix: "\n\nExtra focus.",
		ExitCode:     true,
	})
	if err != nil {
		t.Fatalf("resolveRootPrompt failed: %v", err)
	}
	if !strings.HasPrefix(result.SystemPrompt, exitCodePreamble) || !strings.Contains(result.SystemPrompt, "Extra focus.") {
		t.Fatalf("expected exit preamble and preset suffix in prompt:\n%s", result.SystemPrompt)
	}

	result, err = resolveRootPrompt(rootPromptRequest{
		Template:             "default",
		SystemPromptOverride: "manual prompt",
		PresetSuffix:         "should not apply",
	})
	if err != nil {
		t.Fatalf("resolveRootPrompt override failed: %v", err)
	}
	if result.SystemPrompt != "manual prompt" {
		t.Fatalf("override prompt = %q", result.SystemPrompt)
	}

	result, err = resolveRootPrompt(rootPromptRequest{Template: "default", MRStyles: []string{"chaos"}})
	if err != nil {
		t.Fatalf("resolveRootPrompt style failed: %v", err)
	}
	if result.SystemPrompt != mrChaosPrompt {
		t.Fatal("style prompt should replace the template prompt")
	}

	_, err = resolveRootPrompt(rootPromptRequest{Template: "default", MRStyles: []string{"unknown"}})
	if err == nil || !strings.Contains(err.Error(), "unknown style") {
		t.Fatalf("expected unknown style error, got %v", err)
	}
}

func TestGenerateRootProviderOutputBufferedTitleAndComment(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}
	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == titlePrompt {
			return "  Add root pipeline  ", nil
		}
		return "comment body", nil
	}

	got, err := generateRootProviderOutput(rootGenerationRequest{
		Context:      context.Background(),
		Config:       cfg,
		Options:      RootOptions{Format: "json", EffectiveTemplate: "default"},
		Chat:         chatFn,
		SystemPrompt: defaultPromptTemplate,
		DiffContent:  "diff",
		Out:          io.Discard,
	})
	if err != nil {
		t.Fatalf("generateRootProviderOutput failed: %v", err)
	}
	if got.Comment != "comment body" || got.Title != "Add root pipeline" {
		t.Fatalf("generation = %+v", got)
	}
}

func TestGenerateRootProviderOutputCommitMessageUsesCommitTemplate(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}
	var capturedPrompt string
	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		capturedPrompt = prompt
		return "Commit message: fix(cli): generated", nil
	}

	got, err := generateRootProviderOutput(rootGenerationRequest{
		Context:      context.Background(),
		Config:       cfg,
		Options:      RootOptions{Format: "text", EffectiveTemplate: "commit", GenerateCommitMsg: true},
		Chat:         chatFn,
		SystemPrompt: mrCommitPrompt,
		DiffContent:  "diff",
		Out:          io.Discard,
	})
	if err != nil {
		t.Fatalf("generateRootProviderOutput failed: %v", err)
	}
	if capturedPrompt != mrCommitPrompt {
		t.Fatal("commit-only template should be used as the commit message prompt")
	}
	if got.CommitMessage != "fix(cli): generated" {
		t.Fatalf("commit message = %q", got.CommitMessage)
	}
}

func TestGenerateRootProviderOutputDoesNotFallbackAfterPartialStream(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}
	var out strings.Builder
	chatCalled := false
	streamErr := errors.New("stream failed")

	_, err := generateRootProviderOutput(rootGenerationRequest{
		Context: context.Background(),
		Config:  cfg,
		Options: RootOptions{Format: "text", EffectiveTemplate: "default"},
		Chat: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
			chatCalled = true
			return "fallback", nil
		},
		Stream: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string, w io.Writer) (string, error) {
			_, _ = io.WriteString(w, "partial")
			return "", streamErr
		},
		SystemPrompt: defaultPromptTemplate,
		DiffContent:  "diff",
		ShouldStream: true,
		Out:          &out,
	})
	if !errors.Is(err, streamErr) {
		t.Fatalf("expected stream error, got %v", err)
	}
	if chatCalled {
		t.Fatal("fallback chat should not run after partial stream output")
	}
	if out.String() != "partial" {
		t.Fatalf("expected partial output only, got %q", out.String())
	}
}

func TestGenerateRootProviderOutputFallbackBeforeStreamOutput(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}
	var out strings.Builder

	got, err := generateRootProviderOutput(rootGenerationRequest{
		Context: context.Background(),
		Config:  cfg,
		Options: RootOptions{Format: "text", EffectiveTemplate: "default"},
		Chat: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string) (string, error) {
			return "fallback", nil
		},
		Stream: func(_ context.Context, _ *Config, _ ApiProvider, _, _ string, _ io.Writer) (string, error) {
			return "", errors.New("stream failed before output")
		},
		SystemPrompt: defaultPromptTemplate,
		DiffContent:  "diff",
		ShouldStream: true,
		Out:          &out,
	})
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if got.Comment != "fallback" || out.String() != "" {
		t.Fatalf("generation = %+v, out=%q", got, out.String())
	}
}

func TestGenerateRootProviderOutputTitleOnlyAndSmartChunk(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}
	title, err := generateRootProviderOutput(rootGenerationRequest{
		Context: context.Background(),
		Config:  cfg,
		Options: RootOptions{TitleOnly: true},
		Chat: func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
			if prompt != titlePrompt {
				t.Fatalf("unexpected title prompt %q", prompt)
			}
			return "  Generated title  ", nil
		},
		SystemPrompt: defaultPromptTemplate,
		DiffContent:  "diff",
		Out:          io.Discard,
	})
	if err != nil {
		t.Fatalf("title-only generation failed: %v", err)
	}
	if title.Title != "Generated title" {
		t.Fatalf("title = %q", title.Title)
	}

	var prompts []string
	got, err := generateRootProviderOutput(rootGenerationRequest{
		Context: context.Background(),
		Config:  cfg,
		Options: RootOptions{},
		Chat: func(_ context.Context, _ *Config, _ ApiProvider, prompt, diff string) (string, error) {
			prompts = append(prompts, prompt)
			if strings.Contains(prompt, "Summarize the changes") {
				return "summary for " + firstLine(diff), nil
			}
			if strings.Contains(diff, "---") {
				return "synthesized comment", nil
			}
			return "single comment", nil
		},
		SystemPrompt: defaultPromptTemplate,
		DiffContent: "diff --git a/a.go b/a.go\n+one\n" +
			"diff --git a/b.go b/b.go\n+two\n",
		SmartChunk: true,
		Out:        io.Discard,
	})
	if err != nil {
		t.Fatalf("smart-chunk generation failed: %v", err)
	}
	if got.Comment != "synthesized comment" {
		t.Fatalf("smart-chunk comment = %q", got.Comment)
	}
	if len(prompts) != 3 {
		t.Fatalf("expected two chunk prompts and synthesis, got %d: %v", len(prompts), prompts)
	}
}

func TestCountingWriterNilAndWriterError(t *testing.T) {
	if (&countingWriter{}).Written() != 0 {
		t.Fatal("empty countingWriter should report zero")
	}
	var nilWriter *countingWriter
	if nilWriter.Written() != 0 {
		t.Fatal("nil countingWriter should report zero")
	}
	cw := &countingWriter{w: testErrWriter{err: errors.New("closed")}}
	n, err := cw.Write([]byte("hello"))
	if err == nil || n != 0 || cw.Written() != 0 {
		t.Fatalf("Write = %d, %v, written=%d", n, err, cw.Written())
	}
}

func TestNormalizeProviderConnectionError(t *testing.T) {
	cfg := &Config{Provider: Ollama, OllamaEndpoint: "http://localhost:11434/api/generate"}
	err := normalizeProviderConnectionError(cfg, errors.New("dial tcp: connection refused"))
	if err == nil || !strings.Contains(err.Error(), "failed to connect to Ollama") {
		t.Fatalf("expected Ollama connection hint, got %v", err)
	}
}

func TestRootStylePromptCoversAllStyles(t *testing.T) {
	for _, name := range []string{"chaos", "haiku", "roast", "intern", "shakespeare", "manager", "yoda", "excuse"} {
		t.Run(name, func(t *testing.T) {
			got, ok := rootStylePrompt(name)
			if !ok || got == "" {
				t.Fatalf("rootStylePrompt(%q) = %q, %v", name, got, ok)
			}
		})
	}
	if got, ok := rootStylePrompt("plain"); ok || got != "" {
		t.Fatalf("unknown style = %q, %v", got, ok)
	}
}

func TestBuildRootDryRunPlan(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5", Template: "technical"}
	plan := buildRootDryRunPlan(cfg, RootOptions{
		PRURL:             "https://github.com/owner/repo/pull/1",
		OutputPath:        "out.md",
		Clipboard:         "all",
		Post:              true,
		UpdateTitle:       true,
		UpdateDescription: true,
	}, "ci", "remote", diffSummary{FileCount: 2, Additions: 3, Deletions: 1})
	if !plan.DryRun || !plan.WouldCallProvider || !plan.WouldWriteOutput || !plan.WouldCopyClipboard || !plan.WouldPostComment || !plan.WouldUpdateTitle || !plan.WouldUpdateBody {
		t.Fatalf("unexpected dry-run plan: %+v", plan)
	}
	if plan.MissingPostTarget || plan.MissingUpdateTarget || plan.PostTarget == "" {
		t.Fatalf("unexpected target fields: %+v", plan)
	}

	missing := buildRootDryRunPlan(cfg, RootOptions{Post: true, UpdateDescription: true}, "", "local", diffSummary{})
	if !missing.MissingPostTarget || !missing.MissingUpdateTarget || missing.WouldCallProvider {
		t.Fatalf("missing-target plan = %+v", missing)
	}
}
