package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDiffFixtureManifest(t *testing.T) {
	fixtures := []string{
		"added-files-only.diff",
		"binary-files.diff",
		"context-only.diff",
		"crlf-line-endings.diff",
		"deeply-nested-paths.diff",
		"deleted-files-only.diff",
		"deletion.diff",
		"diff.txt",
		"empty-commit.diff",
		"empty.diff",
		"large-multi-file.diff",
		"long-filenames.diff",
		"merge-conflict-markers.diff",
		"mode-change.diff",
		"multiple-files.diff",
		"multiple-hunks-one-file.diff",
		"new-file.diff",
		"no-newline-at-eof.diff",
		"rename-move.diff",
		"simple.diff",
		"submodule-changes.diff",
		"symlink-changes.diff",
		"truncation-trigger.diff",
		"unicode-emoji.diff",
		"very-large-single-file.diff",
		"whitespace-only.diff",
	}

	expected := append([]string(nil), fixtures...)
	sort.Strings(expected)
	entries, err := os.ReadDir(repoPath(t, "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			actual = append(actual, entry.Name())
		}
	}
	sort.Strings(actual)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("testdata fixture manifest is stale\nactual:\n%s\nexpected:\n%s", strings.Join(actual, "\n"), strings.Join(expected, "\n"))
	}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			raw := readTestdata(t, fixture)
			summary := summarizeDiff(raw, filepath.Join("testdata", fixture), "model", false)
			_ = splitDiffByFile(raw)
			_ = processDiff(raw, 100)
			if strings.TrimSpace(raw) != "" && fixture != "empty.diff" && summary.FileCount == 0 {
				t.Fatalf("expected fixture to produce at least one summarized file")
			}
		})
	}
}
