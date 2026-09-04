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

func TestQuickCommitDeps_ValidationAndFailureBranches(t *testing.T) {
	tests := []struct {
		name string
		args []string
		edit func(*commandDeps)
		chat func(context.Context, *Config, ApiProvider, string, string) (string, error)
		want string
	}{
		{
			name: "not repo",
			edit: func(d *commandDeps) { d.isGitRepo = func() bool { return false } },
			want: "not a git repository",
		},
		{
			name: "load config",
			edit: func(d *commandDeps) {
				d.loadConfig = func(string) (*Config, error) { return nil, errors.New("load failed") }
			},
			want: "load failed",
		},
		{name: "bad format", args: []string{"--format=xml"}, want: "unsupported format"},
		{name: "bad body lines", args: []string{"--body-lines=-1"}, want: "--body-lines"},
		{name: "bad type", args: []string{"--type=feature"}, want: "--type must be one of"},
		{name: "bad template", args: []string{"--message-template=verbose"}, want: "--message-template"},
		{name: "bad scope", args: []string{"--scope=bad scope"}, want: "invalid character"},
		{name: "type with no conventional", args: []string{"--type=fix", "--no-conventional"}, want: "cannot be combined"},
		{name: "breaking wrong type", args: []string{"--breaking", "--type=fix"}, want: "--breaking can only"},
		{name: "tracked conflicts", args: []string{"--include-untracked", "--tracked-only"}, want: "mutually exclusive"},
		{name: "post dry run", args: []string{"--post", "--dry-run"}, want: "--post cannot be combined"},
		{name: "post no push", args: []string{"--post", "--no-push"}, want: "--post cannot be combined"},
		{
			name: "branch error",
			edit: func(d *commandDeps) {
				d.getCurrentBranch = func() (string, error) { return "", errors.New("branch failed") }
			},
			want: "could not determine current branch",
		},
		{
			name: "detached",
			edit: func(d *commandDeps) {
				d.getCurrentBranch = func() (string, error) { return "", nil }
			},
			want: "detached HEAD",
		},
		{
			name: "stage error",
			edit: func(d *commandDeps) {
				d.stageAll = func() error { return errors.New("stage failed") }
			},
			want: "stage failed",
		},
		{
			name: "diff error",
			edit: func(d *commandDeps) {
				d.getGitDiff = func(string, bool, []string) (string, error) { return "", errors.New("diff failed") }
			},
			want: "reading diff",
		},
		{
			name: "empty diff",
			edit: func(d *commandDeps) {
				d.getGitDiff = func(string, bool, []string) (string, error) { return "", nil }
			},
			want: "no changes found",
		},
		{name: "style conflict", args: []string{"--chaos", "--roast"}, want: "mutually exclusive"},
		{name: "chaos multiline conflict", args: []string{"--chaos", "--multi-line"}, want: "--chaos cannot"},
		{name: "haiku multiline conflict", args: []string{"--haiku", "--multi-line"}, want: "--haiku cannot"},
		{name: "roast multiline conflict", args: []string{"--roast", "--multi-line"}, want: "--roast cannot"},
		{
			name: "empty ai message",
			chat: func(context.Context, *Config, ApiProvider, string, string) (string, error) { return "", nil },
			want: "empty commit message",
		},
		{
			name: "fortune error",
			args: []string{"--fortune"},
			chat: func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
				if prompt == fortunePrompt {
					return "", errors.New("fortune failed")
				}
				return "fix: generated", nil
			},
			want: "generating fortune",
		},
		{
			name: "edit error",
			args: []string{"--edit"},
			edit: func(d *commandDeps) {
				d.editCommitMessage = func(string) (string, error) { return "", errors.New("edit failed") }
			},
			want: "edit failed",
		},
		{
			name: "signoff error",
			args: []string{"--signoff"},
			edit: func(d *commandDeps) {
				d.getSignoffIdentity = func() (string, error) { return "", errors.New("identity failed") }
			},
			want: "--signoff",
		},
		{
			name: "commit error",
			edit: func(d *commandDeps) {
				d.commitMessage = func(string) error { return errors.New("commit failed") }
			},
			want: "commit failed",
		},
		{
			name: "post remote url error",
			args: []string{"--post"},
			edit: func(d *commandDeps) {
				d.getRemoteURL = func() (string, error) { return "", errors.New("remote failed") }
			},
			want: "getting remote URL",
		},
		{
			name: "post parse error",
			args: []string{"--post"},
			edit: func(d *commandDeps) {
				d.parseRemoteInfo = func(string) (remoteInfo, error) { return remoteInfo{}, errors.New("bad remote") }
			},
			want: "bad remote",
		},
		{
			name: "post unknown host",
			args: []string{"--post"},
			edit: func(d *commandDeps) {
				d.parseRemoteInfo = func(string) (remoteInfo, error) {
					return remoteInfo{Host: "example.com", PathParts: []string{"owner", "repo"}}, nil
				}
			},
			want: "unrecognised remote host",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var actions []string
			deps := fakeQuickCommitDeps(&actions)
			if tc.edit != nil {
				tc.edit(&deps)
			}
			chatFn := tc.chat
			if chatFn == nil {
				chatFn = func(context.Context, *Config, ApiProvider, string, string) (string, error) {
					return "fix: generated", nil
				}
			}
			args := append([]string{"--provider=openai"}, tc.args...)
			err := executeQuickCommitWithDeps(t, deps, chatFn, args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestQuickCommitDeps_InvalidStylesDoNotStage(t *testing.T) {
	var actions []string
	deps := fakeQuickCommitDeps(&actions)
	err := executeQuickCommitWithDeps(t, deps, dummyChatFn, "--provider=openai", "--chaos", "--roast")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected style validation error, got %v", err)
	}
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 4 {
		t.Fatalf("expected invalid usage exit code 4, got %T %v", err, err)
	}
	if len(actions) != 0 {
		t.Fatalf("invalid invocation performed side effects: %v", actions)
	}
}

func TestQuickCommitDeps_DryRunStylesAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		wantPrompt string
	}{
		{name: "monday", args: []string{"--dry-run", "--monday"}, wantPrompt: quickCommitMondayPrompt},
		{name: "jira", args: []string{"--dry-run", "--jira"}, wantPrompt: quickCommitJiraPrompt},
		{name: "emoji", args: []string{"--dry-run", "--emoji-commit"}, wantPrompt: quickCommitEmojiPrompt},
		{name: "sassy", args: []string{"--dry-run", "--sassy"}, wantPrompt: quickCommitSassyPrompt},
		{name: "technical", args: []string{"--dry-run", "--technical"}, wantPrompt: quickCommitTechnicalPrompt},
		{name: "intern", args: []string{"--dry-run", "--intern"}, wantPrompt: quickCommitInternPrompt},
		{name: "shakespeare", args: []string{"--dry-run", "--shakespeare"}, wantPrompt: quickCommitShakespearePrompt},
		{name: "manager", args: []string{"--dry-run", "--manager"}, wantPrompt: quickCommitManagerPrompt},
		{name: "yoda", args: []string{"--dry-run", "--yoda"}, wantPrompt: quickCommitYodaPrompt},
		{name: "excuse", args: []string{"--dry-run", "--excuse"}, wantPrompt: quickCommitExcusePrompt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actions []string
			deps := fakeQuickCommitDeps(&actions)
			var promptSeen string
			err := executeQuickCommitWithDeps(t, deps, func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
				promptSeen = prompt
				return "fix: generated", nil
			}, append([]string{"--provider=openai"}, tc.args...)...)
			if err != nil {
				t.Fatalf("quick-commit failed: %v", err)
			}
			if promptSeen != tc.wantPrompt {
				t.Fatalf("prompt = %q, want %q", promptSeen, tc.wantPrompt)
			}
		})
	}

	var actions []string
	deps := fakeQuickCommitDeps(&actions)
	deps.editCommitMessage = func(string) (string, error) { return "feat: edited", nil }
	deps.getSignoffIdentity = func() (string, error) { return "A User <a@example.com>", nil }
	err := executeQuickCommitWithDeps(t, deps, func(_ context.Context, _ *Config, _ ApiProvider, prompt, _ string) (string, error) {
		if prompt == fortunePrompt {
			return "fortune favors tests", nil
		}
		return "feat: generated", nil
	}, "--provider=openai", "--dry-run", "--format=json", "--fortune", "--edit", "--signoff")
	if err != nil {
		t.Fatalf("quick-commit json dry-run failed: %v", err)
	}
}
