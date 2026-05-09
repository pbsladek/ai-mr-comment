package app

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveRootDiffInputWithFakes(t *testing.T) {
	cfg := &Config{}
	deps := defaultCommandDeps()
	deps.isGitRepo = func() bool { return true }
	deps.getGitDiff = func(commit string, staged bool, exclude []string) (string, error) {
		if commit != "HEAD~1..HEAD" || staged || strings.Join(exclude, ",") != "*.md" {
			t.Fatalf("unexpected git diff args commit=%q staged=%v exclude=%v", commit, staged, exclude)
		}
		return "git diff", nil
	}
	cmd := &cobra.Command{}

	diff, source, err := resolveRootDiffInput(cmd, cfg, RootOptions{
		Format:            "text",
		InputFormat:       "text",
		EffectiveTemplate: "default",
		Commit:            "HEAD~1..HEAD",
	}, []string{"*.md"}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "git diff" || source != "git (commit: HEAD~1..HEAD)" {
		t.Fatalf("diff/source = %q/%q", diff, source)
	}
}

func TestResolveRootDiffInputRemoteDispatch(t *testing.T) {
	deps := defaultCommandDeps()
	deps.getRemoteDiff = func(context.Context, *Config, string) (string, error) {
		return "github diff", nil
	}
	diff, source, err := resolveRootDiffInput(&cobra.Command{}, &Config{}, RootOptions{
		Format:            "text",
		InputFormat:       "text",
		EffectiveTemplate: "default",
		PRURL:             "https://github.com/o/r/pull/1",
	}, nil, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "github diff" || source != "github-pr: https://github.com/o/r/pull/1" {
		t.Fatalf("diff/source = %q/%q", diff, source)
	}
}

func TestPrependLocalBranchContext(t *testing.T) {
	deps := defaultCommandDeps()
	deps.getCurrentBranch = func() (string, error) { return "feat/ABC-123", nil }
	got := prependLocalBranchContext("diff", "git", RootOptions{}, deps, &Config{})
	if !strings.HasPrefix(got, "Branch: feat/ABC-123\n\n") {
		t.Fatalf("expected branch prefix, got %q", got)
	}
	if unchanged := prependLocalBranchContext("diff", "stdin", RootOptions{}, deps, &Config{}); unchanged != "diff" {
		t.Fatalf("stdin diff should not be prefixed: %q", unchanged)
	}
}
