package app

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestWriteChangelogDryRunText(t *testing.T) {
	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	err := writeChangelogDryRun(cmd, &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}, changelogArgs{
		format:     "text",
		preset:     "release",
		outputPath: "CHANGELOG.md",
	}, diffSummary{
		FileCount: 2,
		Additions: 10,
		Deletions: 3,
	}, "prompt")
	if err != nil {
		t.Fatalf("writeChangelogDryRun failed: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Dry run:", "- Provider: openai", "- Model: gpt-5.5", "- Preset: release", "- Would write output: CHANGELOG.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}
