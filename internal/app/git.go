package app

import (
	"context"

	gogithub "github.com/google/go-github/v68/github"
	"github.com/pbsladek/ai-mr-comment/internal/gitdiff"
	"github.com/pbsladek/ai-mr-comment/internal/localgit"
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
	return localgit.RemoteURL()
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
	return localgit.AutoMergeBase()
}

// isGitRepo reports whether the current directory is inside a git repository.
func isGitRepo() bool {
	return localgit.IsRepo()
}

// getCurrentBranch returns the name of the current git branch (e.g. "feat/ABC-123-add-login").
// Returns an empty string and no error when in a detached HEAD state.
func getCurrentBranch() (string, error) {
	return localgit.CurrentBranch()
}

// gitCommit creates a commit with the given message. When body is non-empty it
// is passed as a second -m argument, producing a commit with a subject and body
// separated by a blank line (as git does with multiple -m flags).
func gitCommit(message, body string) error {
	return localgit.Commit(message, body)
}

// gitPush pushes the current branch to its upstream remote.
// It uses --set-upstream origin <branch> so it works even on a branch with no
// tracking ref yet (e.g. the first push of a new branch).
func gitPush(branch string) error {
	return localgit.Push(branch)
}

// hasCommits reports whether the repository has at least one commit.
func hasCommits() bool {
	return localgit.HasCommits()
}

// getGitDiff returns the git diff for the given mode.
// Priority: staged > explicit commit > auto merge-base > unstaged working tree.
// Patterns in exclude are passed as git pathspecs (":!pattern") to filter files at the source.
func getGitDiff(commit string, staged bool, exclude []string) (string, error) {
	return localgit.Diff(commit, staged, exclude)
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

// findOrCreateGitLabMR finds an open MR for branch in projectPath, or creates
// one targeting the project's default branch. Returns the MR web URL.
func findOrCreateGitLabMR(ctx context.Context, gl *gogitlab.Client, projectPath, branch, title string) (string, error) {
	return remote.FindOrCreateGitLabMR(ctx, gl, projectPath, branch, title)
}

// getPRDiffWithClient fetches the diff and metadata for a GitHub pull request
// using the provided go-github client. Separated from getPRDiff to allow tests
// to inject a client pointed at a local httptest server.
func getPRDiffWithClient(ctx context.Context, gh *gogithub.Client, prURL string) (string, error) {
	return remote.GetPRDiffWithClient(ctx, gh, prURL)
}

// getMRDiffWithClient fetches the diff and metadata for a GitLab merge request
// using the provided go-gitlab client. Separated from getMRDiff to allow tests
// to inject a client pointed at a local httptest server.
func getMRDiffWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string) (string, error) {
	return remote.GetMRDiffWithClient(ctx, gl, mrURL)
}

// postGitHubPRCommentWithClient posts body as a PR comment using the given client.
// Separated from postGitHubPRComment to allow tests to inject a client pointed
// at a local httptest server.
func postGitHubPRCommentWithClient(ctx context.Context, gh *gogithub.Client, prURL, body string) error {
	return remote.PostGitHubPRCommentWithClient(ctx, gh, prURL, body)
}

type prMetadata = remote.Metadata

// updateGitHubPRMetadataWithClient updates the title and/or body of a GitHub PR.
// Nil fields are left unchanged.
func updateGitHubPRMetadataWithClient(ctx context.Context, gh *gogithub.Client, prURL string, title, body *string) error {
	return remote.UpdateGitHubPRMetadataWithClient(ctx, gh, prURL, title, body)
}

// postGitLabMRNoteWithClient posts body as an MR note using the given client.
// Separated from postGitLabMRNote to allow tests to inject a client pointed
// at a local httptest server.
func postGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, body string) error {
	return remote.PostGitLabMRNoteWithClient(ctx, gl, mrURL, body)
}

// updateGitLabMRMetadataWithClient updates the title and/or description of a GitLab MR.
// Nil fields are left unchanged.
func updateGitLabMRMetadataWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string, title, description *string) error {
	return remote.UpdateGitLabMRMetadataWithClient(ctx, gl, mrURL, title, description)
}

func upsertGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, marker, body string) error {
	return remote.UpsertGitLabMRNoteWithClient(ctx, gl, mrURL, marker, body)
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
