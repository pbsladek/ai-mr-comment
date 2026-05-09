package app

import (
	"strings"
	"testing"
)

func TestValidateRootOptions(t *testing.T) {
	valid := RootOptions{
		Format:            "text",
		InputFormat:       "text",
		EffectiveTemplate: "default",
	}
	tests := []struct {
		name string
		edit func(*RootOptions)
		want string
	}{
		{
			name: "valid",
		},
		{
			name: "post requires pr",
			edit: func(o *RootOptions) { o.Post = true },
			want: "--post requires --pr",
		},
		{
			name: "dry run allows missing post target",
			edit: func(o *RootOptions) {
				o.Post = true
				o.DryRun = true
			},
		},
		{
			name: "metadata update requires pr",
			edit: func(o *RootOptions) { o.UpdateTitle = true },
			want: "--update-title and --update-description require --pr",
		},
		{
			name: "metadata update cannot use commit msg",
			edit: func(o *RootOptions) {
				o.UpdateTitle = true
				o.PRURL = "https://github.com/o/r/pull/1"
				o.GenerateCommitMsg = true
			},
			want: "cannot be used with --commit-msg",
		},
		{
			name: "staged and commit",
			edit: func(o *RootOptions) {
				o.Staged = true
				o.Commit = "HEAD"
			},
			want: "--staged and --commit",
		},
		{
			name: "pr and local source",
			edit: func(o *RootOptions) {
				o.PRURL = "https://github.com/o/r/pull/1"
				o.DiffFilePath = "diff.patch"
			},
			want: "--pr cannot be combined",
		},
		{
			name: "commit only template requires commit msg",
			edit: func(o *RootOptions) { o.EffectiveTemplate = "commit" },
			want: "--template commit requires --commit-msg",
		},
		{
			name: "mr template rejects commit msg",
			edit: func(o *RootOptions) {
				o.EffectiveTemplate = "technical"
				o.GenerateCommitMsg = true
			},
			want: "--template technical cannot be combined with --commit-msg",
		},
		{
			name: "style flags mutually exclusive",
			edit: func(o *RootOptions) { o.MRStyles = []string{"haiku", "roast"} },
			want: "mutually exclusive",
		},
		{
			name: "style flag conflicts template",
			edit: func(o *RootOptions) {
				o.MRStyles = []string{"haiku"}
				o.TemplateChanged = true
			},
			want: "style flags cannot be combined",
		},
		{
			name: "invalid format",
			edit: func(o *RootOptions) { o.Format = "xml" },
			want: "unsupported format",
		},
		{
			name: "json input rejects remote source",
			edit: func(o *RootOptions) {
				o.InputFormat = "json"
				o.PRURL = "https://github.com/o/r/pull/1"
			},
			want: "--input=json cannot be combined",
		},
		{
			name: "jsonl output conflict",
			edit: func(o *RootOptions) {
				o.StreamMode = "jsonl"
				o.OutputPath = "out.jsonl"
			},
			want: "--stream=jsonl cannot be combined with --output",
		},
		{
			name: "dry run metadata conflict",
			edit: func(o *RootOptions) {
				o.DryRun = true
				o.PrintRequest = true
			},
			want: "--dry-run cannot be combined",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := valid
			if tc.edit != nil {
				tc.edit(&opts)
			}
			err := validateRootOptions(opts)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestBuildRootOutputPayload(t *testing.T) {
	cfg := &Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"}

	comment := buildRootOutputPayload(cfg, false, false, "Add API", "Body", "", "PASS", "file: diff.patch", true)
	if comment.Title != "Add API" || comment.Description != "Body" || comment.Comment != "Body" || comment.Verdict != "PASS" {
		t.Fatalf("unexpected comment payload: %+v", comment)
	}
	if comment.Provider != "openai" || comment.Model != "gpt-5.5" || !comment.Truncated {
		t.Fatalf("unexpected metadata: %+v", comment)
	}

	commit := buildRootOutputPayload(cfg, false, true, "", "", "fix: bug", "", "git", false)
	if commit.CommitMessage != "fix: bug" || commit.Description != "" || commit.Comment != "" {
		t.Fatalf("unexpected commit payload: %+v", commit)
	}

	title := buildRootOutputPayload(cfg, true, false, "Fix crash", "ignored", "", "", "git", false)
	if title.Title != "Fix crash" || title.Description != "" || title.Comment != "" {
		t.Fatalf("unexpected title payload: %+v", title)
	}
}
