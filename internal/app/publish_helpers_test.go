package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/pbsladek/ai-mr-comment/internal/remote"
)

func TestRemoteCredentialsMapsConfig(t *testing.T) {
	cfg := &Config{
		GitHubToken:   "gh-token",
		GitHubBaseURL: "https://github.example",
		GitLabToken:   "gl-token",
		GitLabBaseURL: "https://gitlab.example",
	}
	got := remoteCredentials(cfg)
	if got.GitHubToken != cfg.GitHubToken || got.GitHubBaseURL != cfg.GitHubBaseURL ||
		got.GitLabToken != cfg.GitLabToken || got.GitLabBaseURL != cfg.GitLabBaseURL {
		t.Fatalf("remoteCredentials = %+v", got)
	}
}

func TestRemoteTargetHelpersRejectUnsupportedURL(t *testing.T) {
	cfg := &Config{}
	if _, err := getRemoteDiff(context.Background(), cfg, "https://example.com/owner/repo/issues/1"); err == nil {
		t.Fatal("expected getRemoteDiff to reject unsupported URL")
	}
	if err := postRemoteComment(context.Background(), cfg, "https://example.com/owner/repo/issues/1", "body"); err == nil {
		t.Fatal("expected postRemoteComment to reject unsupported URL")
	}
}

func TestFindOrCreatePublishTargetWithDeps(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "git@github.com:owner/repo.git", nil }
		deps.parseRemoteInfo = func(string) (remoteInfo, error) {
			return remote.Info{Host: "github.com", PathParts: []string{"owner", "repo"}}, nil
		}
		deps.findOrCreateTarget = func(_ context.Context, _ *Config, info remoteInfo, branch, title string) (string, error) {
			if info.Host != "github.com" || branch != "feat/test" || title != "Generated title" {
				t.Fatalf("unexpected target args: %+v %q %q", info, branch, title)
			}
			return "https://github.com/owner/repo/pull/1", nil
		}

		got, err := findOrCreatePublishTargetWithDeps(context.Background(), &Config{}, "Generated title", deps)
		if err != nil || got != "https://github.com/owner/repo/pull/1" {
			t.Fatalf("findOrCreatePublishTargetWithDeps = %q, %v", got, err)
		}
	})

	t.Run("detached head", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "", nil }
		if _, err := findOrCreatePublishTargetWithDeps(context.Background(), &Config{}, "Title", deps); err == nil || !strings.Contains(err.Error(), "detached HEAD") {
			t.Fatalf("expected detached HEAD error, got %v", err)
		}
	})

	t.Run("remote url error", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "", errors.New("no origin") }
		if _, err := findOrCreatePublishTargetWithDeps(context.Background(), &Config{}, "Title", deps); err == nil || !strings.Contains(err.Error(), "getting remote URL") {
			t.Fatalf("expected remote URL error, got %v", err)
		}
	})

	t.Run("unknown host", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "git@example.com:owner/repo.git", nil }
		deps.parseRemoteInfo = func(string) (remoteInfo, error) {
			return remote.Info{Host: "example.com", PathParts: []string{"owner", "repo"}}, nil
		}
		if _, err := findOrCreatePublishTargetWithDeps(context.Background(), &Config{}, "Title", deps); err == nil || !strings.Contains(err.Error(), "unrecognised remote host") {
			t.Fatalf("expected unknown host error, got %v", err)
		}
	})
}

func TestPlannedPublishTargetWithDeps(t *testing.T) {
	t.Run("create url", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "git@github.com:owner/repo.git", nil }

		got, err := plannedPublishTargetWithDeps(&Config{}, deps)
		if err != nil || !strings.Contains(got, "/compare/feat%2Ftest?expand=1") {
			t.Fatalf("plannedPublishTargetWithDeps = %q, %v", got, err)
		}
	})

	t.Run("custom gitlab host", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "ssh://git@gitlab.example/group/repo.git", nil }
		deps.parseRemoteInfo = func(string) (remoteInfo, error) {
			return remote.Info{Host: "gitlab.example", PathParts: []string{"group", "repo"}}, nil
		}

		got, err := plannedPublishTargetWithDeps(&Config{GitLabBaseURL: "https://gitlab.example"}, deps)
		if err != nil || !strings.Contains(got, "gitlab.example/group/repo/-/merge_requests/new?") ||
			!strings.Contains(got, "merge_request%5Bsource_branch%5D=feat%2Ftest") {
			t.Fatalf("plannedPublishTargetWithDeps = %q, %v", got, err)
		}
	})

	t.Run("custom github host", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "ssh://git@github.example/owner/repo.git", nil }
		deps.parseRemoteInfo = func(string) (remoteInfo, error) {
			return remote.Info{Host: "github.example", PathParts: []string{"owner", "repo", "extra"}}, nil
		}

		got, err := plannedPublishTargetWithDeps(&Config{GitHubBaseURL: "https://github.example"}, deps)
		if err != nil || got != "https://github.example/owner/repo/compare/feat%2Ftest?expand=1" {
			t.Fatalf("plannedPublishTargetWithDeps = %q, %v", got, err)
		}
	})

	t.Run("detached head", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "", nil }
		if _, err := plannedPublishTargetWithDeps(&Config{}, deps); err == nil || !strings.Contains(err.Error(), "detached HEAD") {
			t.Fatalf("expected detached HEAD error, got %v", err)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		deps := defaultCommandDeps()
		deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
		deps.getRemoteURL = func() (string, error) { return "not-a-remote", nil }
		deps.parseRemoteInfo = func(string) (remoteInfo, error) { return remoteInfo{}, errors.New("bad remote") }
		if _, err := plannedPublishTargetWithDeps(&Config{}, deps); err == nil || !strings.Contains(err.Error(), "bad remote") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})
}

func TestWritePublishDryRunText(t *testing.T) {
	cmd := newPublishCmdWithDeps(dummyChatFn, defaultCommandDeps())
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	err := writePublishDryRun(
		cmd,
		&Config{Provider: OpenAI, OpenAIModel: "gpt-5.5"},
		"https://github.com/owner/repo/pull/1",
		"Generated title",
		"Generated description",
		true,
		false,
		true,
		[]string{"bug", "docs"},
		[]string{"alice"},
	)
	if err != nil {
		t.Fatalf("writePublishDryRun failed: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Dry run:", "- Target:", "- Update title: true", "- Update description: false", "- Labels: bug, docs", "- Reviewers: alice"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in dry-run output:\n%s", want, got)
		}
	}
}

func TestPublishCommandFullActionFlow(t *testing.T) {
	deps := defaultCommandDeps()
	deps.loadConfig = func(string) (*Config, error) {
		return &Config{
			Provider:     OpenAI,
			OpenAIAPIKey: "dummy",
			OpenAIModel:  "gpt-5.5",
			Template:     "default",
		}, nil
	}
	deps.isGitRepo = func() bool { return true }
	deps.getCurrentBranch = func() (string, error) { return "feat/security-update", nil }
	deps.getGitDiff = func(string, bool, []string) (string, error) {
		return strings.Join([]string{
			"diff --git a/docs/readme.md b/docs/readme.md",
			"+docs",
			"diff --git a/internal/app/app_test.go b/internal/app/app_test.go",
			"+test",
			"diff --git a/.github/workflows/test.yml b/.github/workflows/test.yml",
			"+ci",
			"diff --git a/go.mod b/go.mod",
			"+dep",
		}, "\n"), nil
	}
	deps.getRemoteURL = func() (string, error) { return "git@github.com:owner/repo.git", nil }
	deps.parseRemoteInfo = func(string) (remoteInfo, error) {
		return remote.Info{Host: "github.com", PathParts: []string{"owner", "repo"}}, nil
	}
	deps.findOrCreateTarget = func(_ context.Context, _ *Config, info remoteInfo, branch, title string) (string, error) {
		if info.Host != "github.com" || branch != "feat/security-update" || !strings.HasPrefix(title, "Draft:") {
			t.Fatalf("unexpected find target args: %+v %q %q", info, branch, title)
		}
		return "https://github.com/owner/repo/pull/7", nil
	}
	var updatedTitle, updatedDescription string
	deps.updateRemoteMetadata = func(_ context.Context, _ *Config, targetURL string, title, description *string) error {
		if targetURL != "https://github.com/owner/repo/pull/7" {
			t.Fatalf("unexpected target %q", targetURL)
		}
		if title != nil {
			updatedTitle = *title
		}
		if description != nil {
			updatedDescription = *description
		}
		return nil
	}
	var comment string
	deps.upsertRemoteManagedComment = func(_ context.Context, _ *Config, _ string, body string) error {
		comment = body
		return nil
	}
	var labels, reviewers []string
	deps.addRemoteLabels = func(_ context.Context, _ *Config, _ string, got []string) error {
		labels = got
		return nil
	}
	deps.requestRemoteReviewers = func(_ context.Context, _ *Config, _ string, got []string) error {
		reviewers = got
		return nil
	}

	cmd := newPublishCmdWithDeps(func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == titlePrompt {
			return "Security dependency update", nil
		}
		return "Security breaking migration with credential handling updates", nil
	}, deps)
	var out, errOut strings.Builder
	cmd.SetArgs([]string{
		"--provider=openai",
		"--model=gpt-override",
		"--template=does-not-exist",
		"--format=json",
		"--auto-labels",
		"--draft-if-risky",
		"--replace-description",
		"--label=manual,docs",
		"--reviewer=alice,bob",
	})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("publish failed: %v\nstderr=%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "Warning:") {
		t.Fatalf("expected template warning, got %q", errOut.String())
	}
	if updatedTitle != "Draft: Security dependency update" {
		t.Fatalf("updated title = %q", updatedTitle)
	}
	if updatedDescription != "Security breaking migration with credential handling updates" {
		t.Fatalf("updated description = %q", updatedDescription)
	}
	if !strings.Contains(comment, managedCommentMarker) || !strings.Contains(comment, "Security breaking migration") {
		t.Fatalf("managed comment = %q", comment)
	}
	for _, want := range []string{"manual", "docs", "security", "breaking-change", "tests", "dependencies"} {
		if !containsString(labels, want) {
			t.Fatalf("expected label %q in %v", want, labels)
		}
	}
	if strings.Join(reviewers, ",") != "alice,bob" {
		t.Fatalf("reviewers = %v", reviewers)
	}
	var payload struct {
		URL                string   `json:"url"`
		Title              string   `json:"title"`
		DescriptionUpdated bool     `json:"description_updated"`
		CommentUpserted    bool     `json:"comment_upserted"`
		Labels             []string `json:"labels"`
		Reviewers          []string `json:"reviewers"`
		Model              string   `json:"model"`
	}
	if err := json.Unmarshal([]byte(out.String()), &payload); err != nil {
		t.Fatalf("invalid publish json: %v\n%s", err, out.String())
	}
	if payload.URL == "" || payload.Title != updatedTitle || !payload.DescriptionUpdated || !payload.CommentUpserted || payload.Model != "gpt-override" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestPublishCommandValidationBranches(t *testing.T) {
	tests := []struct {
		name string
		args []string
		edit func(*commandDeps)
		want string
	}{
		{
			name: "load config error",
			edit: func(d *commandDeps) {
				d.loadConfig = func(string) (*Config, error) { return nil, errors.New("load failed") }
			},
			want: "load failed",
		},
		{name: "unsupported provider", args: []string{"--provider=bogus"}, want: "unsupported provider"},
		{name: "bad format", args: []string{"--format=xml"}, want: "unsupported format"},
		{name: "no actions", args: []string{"--no-update-title", "--no-update-description", "--post-summary=false"}, want: "no remote actions"},
		{
			name: "diff error",
			edit: func(d *commandDeps) {
				d.getGitDiff = func(string, bool, []string) (string, error) { return "", errors.New("diff failed") }
			},
			want: "reading local diff",
		},
		{
			name: "empty diff",
			edit: func(d *commandDeps) {
				d.getRemoteDiff = func(context.Context, *Config, string) (string, error) { return "", nil }
			},
			args: []string{"--pr=https://github.com/owner/repo/pull/1"},
			want: "no diff found",
		},
		{
			name: "generation failure",
			edit: func(d *commandDeps) {
				d.getRemoteDiff = func(context.Context, *Config, string) (string, error) {
					return "diff --git a/a.go b/a.go\n+one\n", nil
				}
			},
			args: []string{"--pr=https://github.com/owner/repo/pull/1"},
			want: "generate failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := defaultCommandDeps()
			deps.loadConfig = func(string) (*Config, error) {
				return &Config{Provider: OpenAI, OpenAIAPIKey: "dummy", OpenAIModel: "gpt-5.5", Template: "default"}, nil
			}
			deps.isGitRepo = func() bool { return true }
			deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
			deps.getGitDiff = func(string, bool, []string) (string, error) {
				return "diff --git a/file.txt b/file.txt\n+changed\n", nil
			}
			deps.getRemoteURL = func() (string, error) { return "git@github.com:owner/repo.git", nil }
			if tc.edit != nil {
				tc.edit(&deps)
			}
			chat := func(context.Context, *Config, ApiProvider, string, string) (string, error) {
				if tc.name == "generation failure" {
					return "", errors.New("generate failed")
				}
				return "generated", nil
			}
			cmd := newPublishCmdWithDeps(chat, deps)
			cmd.SetArgs(append([]string{"--provider=openai"}, tc.args...))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}
