package app

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"

	gogithub "github.com/google/go-github/v68/github"
	"github.com/pbsladek/ai-mr-comment/internal/gitdiff"
	"github.com/pbsladek/ai-mr-comment/internal/remote"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
)

func resolveGitHubBaseURL(prURL, configuredBaseURL string) (string, error) {
	return remote.ResolveGitHubBaseURL(prURL, configuredBaseURL)
}

func resolveGitLabBaseURL(mrURL, configuredBaseURL string) (string, error) {
	return remote.ResolveGitLabBaseURL(mrURL, configuredBaseURL)
}

// getRemoteURL returns the push URL for the "origin" remote.
func getRemoteURL() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, "origin" is a constant
	if err != nil {
		return "", fmt.Errorf("getting remote URL: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// prCreateURL converts a git remote URL and branch name into a browser URL
// for creating a new PR (GitHub) or MR (GitLab). Returns an empty string
// when the remote does not match a known hosting pattern.
//
// Handles:
//   - https://github.com/owner/repo.git      → github.com PR compare URL
//   - git@github.com:owner/repo.git           → same
//   - https://gitlab.com/group/project.git   → gitlab.com MR create URL
//   - git@gitlab.com:group/project.git        → same
func prCreateURL(remoteURL, branch string) string {
	return remote.CreateURL(remoteURL, branch)
}

// getAutoMergeBase returns the common ancestor commit between HEAD and the
// remote default branch, trying origin/main then origin/master.
func getAutoMergeBase() (string, error) {
	for _, branch := range []string{"origin/main", "origin/master"} {
		out, err := exec.Command("git", "merge-base", "HEAD", branch).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("could not determine merge base: no origin/main or origin/master found")
}

// isGitRepo reports whether the current directory is inside a git repository.
func isGitRepo() bool {
	err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run()
	return err == nil
}

// getCurrentBranch returns the name of the current git branch (e.g. "feat/ABC-123-add-login").
// Returns an empty string and no error when in a detached HEAD state.
func getCurrentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants
	if err != nil {
		// rev-parse fails on repos with no commits yet. Fall back to symbolic-ref
		// which reads the branch name directly from .git/HEAD without needing commits.
		out2, err2 := exec.Command("git", "symbolic-ref", "--short", "HEAD").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants
		if err2 != nil {
			return "", fmt.Errorf("getting current branch: %w", err)
		}
		return strings.TrimSpace(string(out2)), nil
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		// Detached HEAD state — no branch name available.
		return "", nil
	}
	return branch, nil
}

// gitAdd stages all changes in the working tree (git add .).
func gitAdd() error {
	out, err := exec.Command("git", "add", ".").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants
	if err != nil {
		return fmt.Errorf("git add: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitAddTracked stages only tracked modified/deleted files (git add -u).
func gitAddTracked() error {
	out, err := exec.Command("git", "add", "-u").CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are internal constants
	if err != nil {
		return fmt.Errorf("git add -u: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitCommit creates a commit with the given message. When body is non-empty it
// is passed as a second -m argument, producing a commit with a subject and body
// separated by a blank line (as git does with multiple -m flags).
func gitCommit(message, body string) error {
	args := []string{"commit", "-m", message}
	if body != "" {
		args = append(args, "-m", body)
	}
	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, message/body are user-provided commit text
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitCommitMessage creates a commit from the full multi-line message exactly as
// supplied.
func gitCommitMessage(message string) error {
	tmp, err := os.CreateTemp("", "ai-mr-comment-commit-*.txt")
	if err != nil {
		return fmt.Errorf("creating commit message file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		_ = os.Remove(name)
	}()
	if _, err := tmp.WriteString(message); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing commit message file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing commit message file: %w", err)
	}
	out, err := exec.Command("git", "commit", "-F", name).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, temp file path is created by this process
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func getGitConfigValue(key string) (string, error) {
	out, err := exec.Command("git", "config", "--get", key).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, key is caller-controlled from constants
	if err != nil {
		return "", fmt.Errorf("git config %s: %w", key, err)
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("git config %s is empty", key)
	}
	return value, nil
}

func getGitSignoffIdentity() (string, error) {
	name, err := getGitConfigValue("user.name")
	if err != nil {
		return "", err
	}
	email, err := getGitConfigValue("user.email")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s <%s>", name, email), nil
}

// gitPush pushes the current branch to its upstream remote.
// It uses --set-upstream origin <branch> so it works even on a branch with no
// tracking ref yet (e.g. the first push of a new branch).
func gitPush(branch string) error {
	out, err := exec.Command("git", "push", "--set-upstream", "origin", branch).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, branch is from getCurrentBranch
	if err != nil {
		return fmt.Errorf("git push: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// hasCommits reports whether the repository has at least one commit.
func hasCommits() bool {
	err := exec.Command("git", "rev-parse", "HEAD").Run() //nolint:gosec // G204: git is a fixed binary, args are internal constants
	return err == nil
}

// getGitDiff returns the git diff for the given mode.
// Priority: staged > explicit commit > auto merge-base > unstaged working tree.
// Patterns in exclude are passed as git pathspecs (":!pattern") to filter files at the source.
func getGitDiff(commit string, staged bool, exclude []string) (string, error) {
	var args []string
	if staged {
		args = []string{"diff", "--cached"}
	} else if commit != "" {
		if strings.Contains(commit, "..") {
			args = []string{"diff", commit}
		} else {
			// Single commit: show only that commit's patch. This works for both
			// root commits and commits with parents.
			args = []string{"show", "--format=", commit}
		}
	} else if base, err := getAutoMergeBase(); err == nil {
		// Diff the merge base against the working tree (staged + unstaged).
		// This covers both committed and uncommitted changes on the branch.
		args = []string{"diff", base}
	} else if hasCommits() {
		// No merge base found (no remote, detached HEAD, etc.).
		// Fall back to all changes relative to the last commit — includes both
		// staged and unstaged changes, so nothing is silently missed.
		args = []string{"diff", "HEAD"}
	} else {
		// No commits yet — show everything in the index as a staged diff.
		args = []string{"diff", "--cached"}
	}

	if len(exclude) > 0 {
		args = append(args, "--", ".")
		for _, pattern := range exclude {
			args = append(args, ":!"+pattern)
		}
	}

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // G204: git is a fixed binary, args are controlled by internal logic
	return string(out), err
}

// parsePRURL extracts the owner, repo, and PR number from a GitHub PR URL.
// Works with github.com and self-hosted GitHub Enterprise instances.
// Expected path form: /{owner}/{repo}/pull/{number}
func parsePRURL(prURL string) (owner, repo string, number int, err error) {
	return remote.ParsePRURL(prURL)
}

// parseMRURL extracts the namespace (group/subgroup/project), project name, and
// MR IID from a GitLab MR URL. Works with gitlab.com and self-hosted instances.
// Expected path form: /{namespace}/{project}/-/merge_requests/{iid}
func parseMRURL(mrURL string) (namespace, project string, iid int64, err error) {
	return remote.ParseMRURL(mrURL)
}

// remoteInfo holds parsed components of a git remote URL.
type remoteInfo = remote.Info

// parseRemoteInfo normalises a raw git remote URL (SSH or HTTPS) and extracts
// the host and path segments. Handles git@host:path.git and https://host/path.git.
func parseRemoteInfo(rawURL string) (remoteInfo, error) {
	return remote.ParseInfo(rawURL)
}

// isGitHubHost reports whether host belongs to GitHub (github.com or GHE instance).
func isGitHubHost(host, configuredBaseURL string) bool {
	return remote.IsGitHubHost(host, configuredBaseURL)
}

// isGitLabHost reports whether host belongs to GitLab (gitlab.com or self-hosted).
func isGitLabHost(host, configuredBaseURL string) bool {
	return remote.IsGitLabHost(host, configuredBaseURL)
}

// findOrCreateGitHubPR finds an open PR for branch on owner/repo, or creates
// one targeting the repo's default branch. Returns the PR HTML URL.
func findOrCreateGitHubPR(ctx context.Context, gh *gogithub.Client, owner, repo, branch, title string) (string, error) {
	return remote.FindOrCreateGitHubPR(ctx, gh, owner, repo, branch, title)
}

// findOrCreateGitHubPRFromConfig wraps findOrCreateGitHubPR using credentials from cfg.
func findOrCreateGitHubPRFromConfig(ctx context.Context, cfg *Config, owner, repo, branch, title string) (string, error) {
	gh, err := newGitHubClient(ctx, cfg.GitHubToken, cfg.GitHubBaseURL)
	if err != nil {
		return "", err
	}
	return findOrCreateGitHubPR(ctx, gh, owner, repo, branch, title)
}

// findOrCreateGitLabMR finds an open MR for branch in projectPath, or creates
// one targeting the project's default branch. Returns the MR web URL.
func findOrCreateGitLabMR(ctx context.Context, gl *gogitlab.Client, projectPath, branch, title string) (string, error) {
	return remote.FindOrCreateGitLabMR(ctx, gl, projectPath, branch, title)
}

// findOrCreateGitLabMRFromConfig wraps findOrCreateGitLabMR using credentials from cfg.
func findOrCreateGitLabMRFromConfig(ctx context.Context, cfg *Config, projectPath, branch, title string) (string, error) {
	gl, err := newGitLabClient(cfg.GitLabToken, cfg.GitLabBaseURL)
	if err != nil {
		return "", err
	}
	return findOrCreateGitLabMR(ctx, gl, projectPath, branch, title)
}

// newGitHubClient returns a go-github client. When token is non-empty the client
// is authenticated (5000 req/hr); otherwise unauthenticated (60 req/hr for
// public repos). When baseURL is non-empty the client is configured for a
// self-hosted GitHub Enterprise instance (e.g. https://github.myco.com); the
// SDK appends /api/v3/ automatically.
func newGitHubClient(ctx context.Context, token, baseURL string) (*gogithub.Client, error) {
	return remote.NewGitHubClient(ctx, token, baseURL)
}

// getPRDiffWithClient fetches the diff and metadata for a GitHub pull request
// using the provided go-github client. Separated from getPRDiff to allow tests
// to inject a client pointed at a local httptest server.
func getPRDiffWithClient(ctx context.Context, gh *gogithub.Client, prURL string) (string, error) {
	return remote.GetPRDiffWithClient(ctx, gh, prURL)
}

// getPRDiff fetches the diff and metadata for a GitHub pull request using the
// official go-github SDK and returns a string with the PR title, optional
// description, and raw unified diff. token may be empty for public repositories.
// baseURL may be empty for github.com, or set to a GitHub Enterprise host.
func getPRDiff(ctx context.Context, prURL, token, baseURL string) (string, error) {
	return remote.GetPRDiff(ctx, prURL, token, baseURL)
}

// newGitLabClient returns a go-gitlab client. When token is non-empty the client
// is authenticated; otherwise unauthenticated (for public projects). When
// baseURL is non-empty the client is configured for a self-hosted GitLab
// instance (e.g. https://gitlab.myco.com); the SDK appends /api/v4/ automatically.
func newGitLabClient(token, baseURL string) (*gogitlab.Client, error) {
	return remote.NewGitLabClient(token, baseURL)
}

// getMRDiffWithClient fetches the diff and metadata for a GitLab merge request
// using the provided go-gitlab client. Separated from getMRDiff to allow tests
// to inject a client pointed at a local httptest server.
func getMRDiffWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string) (string, error) {
	return remote.GetMRDiffWithClient(ctx, gl, mrURL)
}

// getMRDiff fetches the diff and metadata for a GitLab merge request using the
// official GitLab Go SDK and returns a string with the MR title, optional
// description, and raw unified diff. token may be empty for public projects.
// baseURL may be empty for gitlab.com, or set to a self-hosted GitLab host.
func getMRDiff(ctx context.Context, mrURL, token, baseURL string) (string, error) {
	return remote.GetMRDiff(ctx, mrURL, token, baseURL)
}

// postGitHubPRCommentWithClient posts body as a PR comment using the given client.
// Separated from postGitHubPRComment to allow tests to inject a client pointed
// at a local httptest server.
func postGitHubPRCommentWithClient(ctx context.Context, gh *gogithub.Client, prURL, body string) error {
	return remote.PostGitHubPRCommentWithClient(ctx, gh, prURL, body)
}

// postGitHubPRComment posts body as a comment on the GitHub PR at prURL.
func postGitHubPRComment(ctx context.Context, prURL, token, baseURL, body string) error {
	return remote.PostGitHubPRComment(ctx, prURL, token, baseURL, body)
}

type prMetadata = remote.Metadata

func getGitHubPRMetadata(ctx context.Context, prURL, token, baseURL string) (prMetadata, error) {
	return remote.GetGitHubPRMetadata(ctx, prURL, token, baseURL)
}

// updateGitHubPRMetadataWithClient updates the title and/or body of a GitHub PR.
// Nil fields are left unchanged.
func updateGitHubPRMetadataWithClient(ctx context.Context, gh *gogithub.Client, prURL string, title, body *string) error {
	return remote.UpdateGitHubPRMetadataWithClient(ctx, gh, prURL, title, body)
}

// updateGitHubPRMetadata updates the title and/or body of the GitHub PR at prURL.
func updateGitHubPRMetadata(ctx context.Context, prURL, token, baseURL string, title, body *string) error {
	return remote.UpdateGitHubPRMetadata(ctx, prURL, token, baseURL, title, body)
}

func upsertGitHubPRComment(ctx context.Context, prURL, token, baseURL, marker, body string) error {
	return remote.UpsertGitHubPRComment(ctx, prURL, token, baseURL, marker, body)
}

func addGitHubPRLabels(ctx context.Context, prURL, token, baseURL string, labels []string) error {
	return remote.AddGitHubPRLabels(ctx, prURL, token, baseURL, labels)
}

func requestGitHubPRReviewers(ctx context.Context, prURL, token, baseURL string, reviewers []string) error {
	return remote.RequestGitHubPRReviewers(ctx, prURL, token, baseURL, reviewers)
}

// postGitLabMRNoteWithClient posts body as an MR note using the given client.
// Separated from postGitLabMRNote to allow tests to inject a client pointed
// at a local httptest server.
func postGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, body string) error {
	return remote.PostGitLabMRNoteWithClient(ctx, gl, mrURL, body)
}

// postGitLabMRNote posts body as a note on the GitLab MR at mrURL.
func postGitLabMRNote(ctx context.Context, mrURL, token, baseURL, body string) error {
	return remote.PostGitLabMRNote(ctx, mrURL, token, baseURL, body)
}

func getGitLabMRMetadata(ctx context.Context, mrURL, token, baseURL string) (prMetadata, error) {
	return remote.GetGitLabMRMetadata(ctx, mrURL, token, baseURL)
}

// updateGitLabMRMetadataWithClient updates the title and/or description of a GitLab MR.
// Nil fields are left unchanged.
func updateGitLabMRMetadataWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string, title, description *string) error {
	return remote.UpdateGitLabMRMetadataWithClient(ctx, gl, mrURL, title, description)
}

// updateGitLabMRMetadata updates the title and/or description of the GitLab MR at mrURL.
func updateGitLabMRMetadata(ctx context.Context, mrURL, token, baseURL string, title, description *string) error {
	return remote.UpdateGitLabMRMetadata(ctx, mrURL, token, baseURL, title, description)
}

func upsertGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, marker, body string) error {
	return remote.UpsertGitLabMRNoteWithClient(ctx, gl, mrURL, marker, body)
}

func upsertGitLabMRNote(ctx context.Context, mrURL, token, baseURL, marker, body string) error {
	return remote.UpsertGitLabMRNote(ctx, mrURL, token, baseURL, marker, body)
}

func addGitLabMRLabels(ctx context.Context, mrURL, token, baseURL string, labels []string) error {
	return remote.AddGitLabMRLabels(ctx, mrURL, token, baseURL, labels)
}

func requestGitLabMRReviewers(ctx context.Context, mrURL, token, baseURL string, reviewers []string) error {
	return remote.RequestGitLabMRReviewers(ctx, mrURL, token, baseURL, reviewers)
}

func cleanStringList(values []string) []string {
	return remote.CleanStringList(values)
}

// formatPRContent builds the combined title + description + diff string that is
// passed to the AI provider.
func formatPRContent(title, body, rawDiff string) string {
	return remote.FormatPRContent(title, body, rawDiff)
}

// isGitHubURL reports whether rawURL looks like a GitHub pull request URL.
// Detects github.com and self-hosted GitHub Enterprise instances by path shape.
func isGitHubURL(rawURL string) bool {
	return remote.IsGitHubURL(rawURL)
}

// isGitLabURL reports whether rawURL looks like a GitLab merge request URL.
// Detects gitlab.com and self-hosted GitLab instances by path shape.
func isGitLabURL(rawURL string) bool {
	return remote.IsGitLabURL(rawURL)
}

// readDiffFromFile reads a raw diff from the given file path.
func readDiffFromFile(path string) (string, error) {
	return gitdiff.ReadFile(path)
}

// splitDiffByFile splits a raw git diff into per-file chunks.
// Each chunk starts with a "diff --git" header and includes all hunks for that file.
func splitDiffByFile(raw string) []string {
	return gitdiff.SplitByFile(raw)
}

// processDiff truncates the raw diff to at most maxLines lines to avoid
// exceeding provider context limits.
func processDiff(raw string, maxLines int) string {
	return gitdiff.Process(raw, maxLines)
}

// truncateDiff keeps the first and last halves of lines when the total exceeds
// max, inserting a marker at the cut point.
func truncateDiff(lines []string, max int) string {
	return gitdiff.Truncate(lines, max)
}
