package main

import (
	"os"
	"strings"
	"testing"
)

// FuzzSplitDiffByFile tests that splitDiffByFile never panics and that every
// returned chunk contains the "diff --git" header it was split on.
func FuzzSplitDiffByFile(f *testing.F) {
	// Inline seeds covering common diff shapes.
	f.Add("")
	f.Add("diff --git a/foo.txt b/foo.txt\n--- a/foo.txt\n+++ b/foo.txt\n@@ -1 +1 @@\n-old\n+new\n")
	f.Add("diff --git a/foo b/foo\n+line\ndiff --git a/bar b/bar\n+line\n")
	f.Add("not a diff at all")
	f.Add("diff --git")
	f.Add("\n\n\n")

	// Seed from existing testdata diff files.
	for _, name := range []string{
		"testdata/simple.diff",
		"testdata/multiple-files.diff",
		"testdata/deletion.diff",
		"testdata/new-file.diff",
	} {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(string(b))
		}
	}

	f.Fuzz(func(t *testing.T, raw string) {
		chunks := splitDiffByFile(raw)
		// The first chunk may be pre-header content (no "diff --git") when the
		// input has text before the first marker. Every chunk after the first
		// must start with "diff --git" since splits only happen at those markers.
		for i, c := range chunks {
			if i == 0 {
				continue
			}
			if !strings.HasPrefix(c, "diff --git") {
				t.Errorf("chunk[%d] should start with 'diff --git'; got: %q", i, c[:min(len(c), 80)])
			}
		}
	})
}

// FuzzProcessDiff tests that processDiff never panics for any combination of
// raw string input and maxLines value.
func FuzzProcessDiff(f *testing.F) {
	f.Add("", 0)
	f.Add("", 1)
	f.Add("single line", 0)
	f.Add("single line", 1)
	f.Add("line1\nline2\nline3\n", 2)
	f.Add("diff --git a/x b/x\n+content\n", 100)

	if b, err := os.ReadFile("testdata/multiple-files.diff"); err == nil {
		f.Add(string(b), 10)
		f.Add(string(b), 1)
	}

	f.Fuzz(func(t *testing.T, raw string, maxLines int) {
		// maxLines must be non-negative; reflect negative values to positive.
		if maxLines < 0 {
			maxLines = -maxLines
		}
		_ = processDiff(raw, maxLines)
	})
}

// FuzzEstimateCost tests that EstimateCost never returns a negative cost and
// never panics for arbitrary model name strings and token counts.
func FuzzEstimateCost(f *testing.F) {
	knownModels := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4o-2024-05-13",
		"gpt-4-turbo",
		"gpt-3.5-turbo",
		"claude-3-5-sonnet-20240620",
		"claude-3-7-sonnet-20250219",
		"claude-3-opus-20240229",
		"claude-3-haiku-20240307",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-2.0-flash",
		"gemini-2.5-flash",
		"llama3",
		"",
		"unknown-model",
		"GPT-4O",
		"CLAUDE-3-OPUS",
	}
	for _, m := range knownModels {
		f.Add(m, int32(1000))
	}
	f.Add("gpt-4o-mini-something-extra", int32(500000))
	f.Add("gemini-pro-experimental", int32(0))

	f.Fuzz(func(t *testing.T, model string, tokens int32) {
		if tokens < 0 {
			tokens = -tokens
		}
		cost := EstimateCost(model, tokens)
		if cost < 0 {
			t.Errorf("EstimateCost returned negative cost %f for model=%q tokens=%d", cost, model, tokens)
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
