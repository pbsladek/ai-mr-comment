package app

import (
	"testing"

	internalconfig "github.com/pbsladek/ai-mr-comment/internal/config"
)

func TestProviderRegistryDrivesCompletionAndValidation(t *testing.T) {
	if len(providerRegistry) != len(providerNames) {
		t.Fatalf("providerRegistry/providerNames mismatch: %d vs %d", len(providerRegistry), len(providerNames))
	}
	seen := map[string]bool{}
	for i, meta := range providerRegistry {
		if providerNames[i] != meta.Name {
			t.Fatalf("providerNames[%d] = %q, want %q", i, providerNames[i], meta.Name)
		}
		if meta.Name == "" {
			t.Fatalf("provider metadata must include name: %+v", meta)
		}
		if meta.Provider != CodexCLI && meta.DefaultModel == "" {
			t.Fatalf("provider metadata must include default model unless it delegates to a local CLI default: %+v", meta)
		}
		if meta.ModelName == nil || meta.Call == nil {
			t.Fatalf("provider metadata must include model/call dispatch: %+v", meta)
		}
		if meta.Streaming && meta.Stream == nil {
			t.Fatalf("streaming provider %q must include stream dispatch", meta.Name)
		}
		if seen[meta.Name] {
			t.Fatalf("duplicate provider registry entry %q", meta.Name)
		}
		seen[meta.Name] = true
		if !isSupportedProvider(meta.Provider) {
			t.Fatalf("provider %q should be supported", meta.Name)
		}
	}
	if isSupportedProvider(ApiProvider("unknown")) {
		t.Fatal("unknown provider should not be supported")
	}
}

func TestProviderRegistryDefaultsMatchConfigDefaults(t *testing.T) {
	cfg := &Config{
		OpenAIModel:    internalconfig.DefaultOpenAIModel,
		AnthropicModel: internalconfig.DefaultAnthropicModel,
		GeminiModel:    internalconfig.DefaultGeminiModel,
		OllamaModel:    internalconfig.DefaultOllamaModel,
		ClaudeCLIModel: internalconfig.DefaultClaudeCLIModel,
		GeminiCLIModel: internalconfig.DefaultGeminiCLIModel,
		CodexCLIModel:  internalconfig.DefaultCodexCLIModel,
	}
	for _, meta := range providerRegistry {
		if got := meta.ModelName(cfg); got != meta.DefaultModel {
			t.Fatalf("%s registry default %q does not match config default %q", meta.Name, meta.DefaultModel, got)
		}
	}
}

func TestTemplateRegistryDrivesCompletionAndCompatibility(t *testing.T) {
	if len(templateRegistry) != len(templateNames) {
		t.Fatalf("templateRegistry/templateNames mismatch: %d vs %d", len(templateRegistry), len(templateNames))
	}
	seen := map[string]bool{}
	for i, meta := range templateRegistry {
		if templateNames[i] != meta.Name {
			t.Fatalf("templateNames[%d] = %q, want %q", i, templateNames[i], meta.Name)
		}
		if meta.Name == "" || meta.PromptType == "" {
			t.Fatalf("template metadata must include name/prompt type: %+v", meta)
		}
		if seen[meta.Name] {
			t.Fatalf("duplicate template registry entry %q", meta.Name)
		}
		seen[meta.Name] = true
	}
	if !isCommitOnlyTemplate("commit") || !isCommitOnlyTemplate("commit-emoji") || !isCommitOnlyTemplate("commit-conventional") {
		t.Fatal("commit templates should be commit-only")
	}
	if !isMROnlyTemplate("technical") || !isMROnlyTemplate("haiku") {
		t.Fatal("MR templates should be MR-only")
	}
	if isCommitOnlyTemplate("default") || isMROnlyTemplate("default") {
		t.Fatal("default template should work for MR and commit paths")
	}
}
