package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

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

func TestNormalizeProviderConnectionError(t *testing.T) {
	cfg := &Config{Provider: Ollama, OllamaEndpoint: "http://localhost:11434/api/generate"}
	err := normalizeProviderConnectionError(cfg, errors.New("dial tcp: connection refused"))
	if err == nil || !strings.Contains(err.Error(), "failed to connect to Ollama") {
		t.Fatalf("expected Ollama connection hint, got %v", err)
	}
}
