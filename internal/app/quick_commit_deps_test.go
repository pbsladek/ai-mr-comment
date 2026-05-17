package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func fakeQuickCommitDeps(actions *[]string) commandDeps {
	deps := defaultCommandDeps()
	deps.loadConfig = func(string) (*Config, error) {
		return &Config{
			Provider:      OpenAI,
			OpenAIAPIKey:  "test-key",
			OpenAIModel:   "gpt-5.5",
			GitHubToken:   "gh-token",
			GitLabToken:   "gl-token",
			GitHubBaseURL: "",
			GitLabBaseURL: "",
		}, nil
	}
	deps.isGitRepo = func() bool { return true }
	deps.getCurrentBranch = func() (string, error) { return "feat/test", nil }
	deps.getGitDiff = func(_ string, staged bool, _ []string) (string, error) {
		if staged {
			*actions = append(*actions, "diff:staged")
		} else {
			*actions = append(*actions, "diff:worktree")
		}
		return "diff --git a/file.txt b/file.txt\n+changed\n", nil
	}
	deps.getQuickDryRunDiff = func(trackedOnly bool) (string, error) {
		if trackedOnly {
			*actions = append(*actions, "diff:dry-run-tracked")
		} else {
			*actions = append(*actions, "diff:dry-run")
		}
		return "diff --git a/file.txt b/file.txt\n+changed\n", nil
	}
	deps.stageAll = func() error {
		*actions = append(*actions, "stage:all")
		return nil
	}
	deps.stageTracked = func() error {
		*actions = append(*actions, "stage:tracked")
		return nil
	}
	deps.commitMessage = func(string) error {
		*actions = append(*actions, "commit")
		return nil
	}
	deps.push = func(string) error {
		*actions = append(*actions, "push")
		return nil
	}
	deps.getRemoteURL = func() (string, error) {
		*actions = append(*actions, "remote-url")
		return "git@github.com:owner/repo.git", nil
	}
	deps.findOrCreateTarget = func(_ context.Context, cfg *Config, info remoteInfo, _, _ string) (string, error) {
		if deps.isGitLabHost(info.Host, cfg.GitLabBaseURL) {
			*actions = append(*actions, "find-gitlab")
			return "https://gitlab.com/group/repo/-/merge_requests/1", nil
		}
		*actions = append(*actions, "find-github")
		return "https://github.com/owner/repo/pull/1", nil
	}
	deps.getRemoteDiff = func(_ context.Context, _ *Config, targetURL string) (string, error) {
		if deps.isGitLabURL(targetURL) {
			*actions = append(*actions, "remote-diff:gitlab")
			return "remote gitlab diff", nil
		}
		*actions = append(*actions, "remote-diff:github")
		return "remote github diff", nil
	}
	deps.postRemoteComment = func(_ context.Context, _ *Config, targetURL, _ string) error {
		if deps.isGitLabURL(targetURL) {
			*actions = append(*actions, "post:gitlab")
			return nil
		}
		*actions = append(*actions, "post:github")
		return nil
	}
	return deps
}

func executeQuickCommitWithDeps(t *testing.T, deps commandDeps, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), args ...string) error {
	t.Helper()
	cmd := newQuickCommitCmdWithDeps(chatFn, deps)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func TestQuickCommitDeps_StagesBeforeDiffAndCommit(t *testing.T) {
	var actions []string
	deps := fakeQuickCommitDeps(&actions)
	chatFn := func(context.Context, *Config, ApiProvider, string, string) (string, error) {
		actions = append(actions, "chat:commit")
		return "fix(cli): generated message", nil
	}

	err := executeQuickCommitWithDeps(t, deps, chatFn, "--no-push", "--provider=openai")
	if err != nil {
		t.Fatalf("quick-commit failed: %v", err)
	}
	want := "stage:all,diff:staged,chat:commit,commit"
	if got := strings.Join(actions, ","); got != want {
		t.Fatalf("actions = %s, want %s", got, want)
	}
}

func TestQuickCommitDeps_PostFlowOrder(t *testing.T) {
	var actions []string
	deps := fakeQuickCommitDeps(&actions)
	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == defaultPromptTemplate {
			actions = append(actions, "chat:review")
			return "review body", nil
		}
		actions = append(actions, "chat:commit")
		return "fix(cli): generated message", nil
	}

	err := executeQuickCommitWithDeps(t, deps, chatFn, "--post", "--provider=openai")
	if err != nil {
		t.Fatalf("quick-commit --post failed: %v", err)
	}
	got := strings.Join(actions, ",")
	for _, want := range []string{
		"stage:all,diff:staged,chat:commit,commit,push",
		"find-github,remote-diff:github,chat:review,post:github",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected action subsequence %q in %q", want, got)
		}
	}
}

func TestQuickCommitDeps_NoPostAfterPushFailure(t *testing.T) {
	var actions []string
	deps := fakeQuickCommitDeps(&actions)
	deps.push = func(string) error {
		actions = append(actions, "push")
		return errors.New("push failed")
	}
	chatFn := func(context.Context, *Config, ApiProvider, string, string) (string, error) {
		actions = append(actions, "chat:commit")
		return "fix(cli): generated message", nil
	}

	err := executeQuickCommitWithDeps(t, deps, chatFn, "--post", "--provider=openai")
	if err == nil || !strings.Contains(err.Error(), "push failed") {
		t.Fatalf("expected push failure, got %v", err)
	}
	if got := strings.Join(actions, ","); strings.Contains(got, "post:") || strings.Contains(got, "find-") {
		t.Fatalf("post flow should not run after push failure, actions: %s", got)
	}
}

func TestQuickCommitDeps_GitLabPostDispatch(t *testing.T) {
	var actions []string
	deps := fakeQuickCommitDeps(&actions)
	deps.getRemoteURL = func() (string, error) {
		actions = append(actions, "remote-url")
		return "git@gitlab.com:group/repo.git", nil
	}
	chatFn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == defaultPromptTemplate {
			actions = append(actions, "chat:review")
			return "review body", nil
		}
		actions = append(actions, "chat:commit")
		return "fix(cli): generated message", nil
	}

	err := executeQuickCommitWithDeps(t, deps, chatFn, "--post", "--provider=openai")
	if err != nil {
		t.Fatalf("quick-commit --post failed: %v", err)
	}
	got := strings.Join(actions, ",")
	if !strings.Contains(got, "find-gitlab,remote-diff:gitlab,chat:review,post:gitlab") {
		t.Fatalf("expected GitLab post dispatch, actions: %s", got)
	}
	if strings.Contains(got, "find-github") || strings.Contains(got, "post:github") {
		t.Fatalf("unexpected GitHub dispatch, actions: %s", got)
	}
}

func TestQuickCommitDeps_PostFailureCases(t *testing.T) {
	tests := []struct {
		name string
		edit func(*commandDeps, *[]string)
		chat func(*[]string) func(context.Context, *Config, ApiProvider, string, string) (string, error)
		want string
	}{
		{
			name: "remote diff failure",
			edit: func(d *commandDeps, actions *[]string) {
				d.getRemoteDiff = func(context.Context, *Config, string) (string, error) {
					*actions = append(*actions, "remote-diff:github")
					return "", errors.New("remote diff failed")
				}
			},
			want: "fetching PR/MR diff",
		},
		{
			name: "review generation failure",
			chat: func(actions *[]string) func(context.Context, *Config, ApiProvider, string, string) (string, error) {
				return func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
					if prompt == defaultPromptTemplate {
						*actions = append(*actions, "chat:review")
						return "", errors.New("review failed")
					}
					*actions = append(*actions, "chat:commit")
					return "fix(cli): generated message", nil
				}
			},
			want: "generating review comment",
		},
		{
			name: "comment post failure",
			edit: func(d *commandDeps, actions *[]string) {
				d.postRemoteComment = func(context.Context, *Config, string, string) error {
					*actions = append(*actions, "post:github")
					return errors.New("post failed")
				}
			},
			want: "posting comment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var actions []string
			deps := fakeQuickCommitDeps(&actions)
			if tc.edit != nil {
				tc.edit(&deps, &actions)
			}
			chatFn := func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
				if prompt == defaultPromptTemplate {
					actions = append(actions, "chat:review")
					return "review body", nil
				}
				actions = append(actions, "chat:commit")
				return "fix(cli): generated message", nil
			}
			if tc.chat != nil {
				chatFn = tc.chat(&actions)
			}

			err := executeQuickCommitWithDeps(t, deps, chatFn, "--post", "--provider=openai")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v; actions=%s", tc.want, err, strings.Join(actions, ","))
			}
		})
	}
}
