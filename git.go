package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	gogithub "github.com/google/go-github/v68/github"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/oauth2"
)

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
			// Single commit: diff against its parent.
			args = []string{"diff", fmt.Sprintf("%s^", commit), commit}
		}
	} else if base, err := getAutoMergeBase(); err == nil {
		// Diff the merge base against the working tree (staged + unstaged).
		// This covers both committed and uncommitted changes on the branch.
		args = []string{"diff", base}
	} else {
		// No merge base found (no remote, detached HEAD, etc.).
		// Fall back to all changes relative to the last commit — includes both
		// staged and unstaged changes, so nothing is silently missed.
		args = []string{"diff", "HEAD"}
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

// parseHostedURL strips trailing slash, query string, and fragment from rawURL
// and returns the cleaned URL.
func parseHostedURL(rawURL string) string {
	rawURL = strings.TrimRight(rawURL, "/")
	if idx := strings.IndexByte(rawURL, '?'); idx != -1 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.IndexByte(rawURL, '#'); idx != -1 {
		rawURL = rawURL[:idx]
	}
	return rawURL
}

// parsePRURL extracts the owner, repo, and PR number from a GitHub PR URL.
// Works with github.com and self-hosted GitHub Enterprise instances.
// Expected path form: /{owner}/{repo}/pull/{number}
func parsePRURL(prURL string) (owner, repo string, number int, err error) {
	prURL = parseHostedURL(prURL)
	u, parseErr := url.Parse(prURL)
	if parseErr != nil || u.Host == "" || u.Scheme == "" {
		return "", "", 0, fmt.Errorf("invalid GitHub PR URL %q: must be a valid URL", prURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" || parts[0] == "" || parts[1] == "" || parts[3] == "" {
		return "", "", 0, fmt.Errorf("invalid GitHub PR URL %q: expected .../{owner}/{repo}/pull/{number}", prURL)
	}
	var num int
	if _, scanErr := fmt.Sscanf(parts[3], "%d", &num); scanErr != nil || num <= 0 {
		return "", "", 0, fmt.Errorf("invalid GitHub PR URL %q: PR number must be a positive integer", prURL)
	}
	return parts[0], parts[1], num, nil
}

// parseMRURL extracts the namespace (group/subgroup/project), project name, and
// MR IID from a GitLab MR URL. Works with gitlab.com and self-hosted instances.
// Expected path form: /{namespace}/{project}/-/merge_requests/{iid}
func parseMRURL(mrURL string) (namespace, project string, iid int64, err error) {
	mrURL = parseHostedURL(mrURL)
	u, parseErr := url.Parse(mrURL)
	if parseErr != nil || u.Host == "" || u.Scheme == "" {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: must be a valid URL", mrURL)
	}
	const marker = "/-/merge_requests/"
	idx := strings.Index(u.Path, marker)
	if idx == -1 {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: expected .../-/merge_requests/{iid}", mrURL)
	}
	projectPath := strings.Trim(u.Path[:idx], "/") // e.g. "group/subgroup/project"
	iidStr := u.Path[idx+len(marker):]              // e.g. "42"
	if projectPath == "" || iidStr == "" {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: missing project path or MR IID", mrURL)
	}
	slashIdx := strings.LastIndex(projectPath, "/")
	if slashIdx == -1 {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: expected {namespace}/{project}", mrURL)
	}
	var num int64
	if _, scanErr := fmt.Sscanf(iidStr, "%d", &num); scanErr != nil || num <= 0 {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: MR IID must be a positive integer", mrURL)
	}
	return projectPath[:slashIdx], projectPath[slashIdx+1:], num, nil
}

// newGitHubClient returns a go-github client. When token is non-empty the client
// is authenticated (5000 req/hr); otherwise unauthenticated (60 req/hr for
// public repos). When baseURL is non-empty the client is configured for a
// self-hosted GitHub Enterprise instance (e.g. https://github.myco.com); the
// SDK appends /api/v3/ automatically.
func newGitHubClient(ctx context.Context, token, baseURL string) (*gogithub.Client, error) {
	var httpClient *http.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(ctx, ts)
	}
	gh := gogithub.NewClient(httpClient)
	if baseURL != "" {
		return gh.WithEnterpriseURLs(baseURL, baseURL)
	}
	return gh, nil
}

// getPRDiffWithClient fetches the diff and metadata for a GitHub pull request
// using the provided go-github client. Separated from getPRDiff to allow tests
// to inject a client pointed at a local httptest server.
func getPRDiffWithClient(ctx context.Context, gh *gogithub.Client, prURL string) (string, error) {
	owner, repo, number, err := parsePRURL(prURL)
	if err != nil {
		return "", err
	}

	// Fetch PR metadata (title + body).
	pr, _, err := gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return "", fmt.Errorf("fetching GitHub PR metadata: %w", err)
	}

	// Fetch the raw unified diff via the SDK diff option.
	opts := &gogithub.RawOptions{Type: gogithub.Diff}
	rawDiff, _, err := gh.PullRequests.GetRaw(ctx, owner, repo, number, *opts)
	if err != nil {
		return "", fmt.Errorf("fetching GitHub PR diff: %w", err)
	}

	return formatPRContent(pr.GetTitle(), pr.GetBody(), rawDiff), nil
}

// getPRDiff fetches the diff and metadata for a GitHub pull request using the
// official go-github SDK and returns a string with the PR title, optional
// description, and raw unified diff. token may be empty for public repositories.
// baseURL may be empty for github.com, or set to a GitHub Enterprise host.
func getPRDiff(ctx context.Context, prURL, token, baseURL string) (string, error) {
	gh, err := newGitHubClient(ctx, token, baseURL)
	if err != nil {
		return "", err
	}
	return getPRDiffWithClient(ctx, gh, prURL)
}

// newGitLabClient returns a go-gitlab client. When token is non-empty the client
// is authenticated; otherwise unauthenticated (for public projects). When
// baseURL is non-empty the client is configured for a self-hosted GitLab
// instance (e.g. https://gitlab.myco.com); the SDK appends /api/v4/ automatically.
func newGitLabClient(token, baseURL string) (*gogitlab.Client, error) {
	var opts []gogitlab.ClientOptionFunc
	if baseURL != "" {
		opts = append(opts, gogitlab.WithBaseURL(baseURL))
	}
	return gogitlab.NewClient(token, opts...)
}

// getMRDiffWithClient fetches the diff and metadata for a GitLab merge request
// using the provided go-gitlab client. Separated from getMRDiff to allow tests
// to inject a client pointed at a local httptest server.
func getMRDiffWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string) (string, error) {
	namespace, project, iid, err := parseMRURL(mrURL)
	if err != nil {
		return "", err
	}

	projectPath := namespace + "/" + project

	// Fetch MR metadata (title + description).
	mr, _, err := gl.MergeRequests.GetMergeRequest(projectPath, iid, nil, gogitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("fetching GitLab MR metadata: %w", err)
	}

	// Fetch the raw unified diff via MR diffs.
	changes, _, err := gl.MergeRequests.ListMergeRequestDiffs(projectPath, iid, nil, gogitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("fetching GitLab MR diff: %w", err)
	}

	var diffBuilder strings.Builder
	for _, c := range changes {
		diffBuilder.WriteString(c.Diff)
	}

	return formatPRContent(mr.Title, mr.Description, diffBuilder.String()), nil
}

// getMRDiff fetches the diff and metadata for a GitLab merge request using the
// official GitLab Go SDK and returns a string with the MR title, optional
// description, and raw unified diff. token may be empty for public projects.
// baseURL may be empty for gitlab.com, or set to a self-hosted GitLab host.
func getMRDiff(ctx context.Context, mrURL, token, baseURL string) (string, error) {
	gl, err := newGitLabClient(token, baseURL)
	if err != nil {
		return "", fmt.Errorf("creating GitLab client: %w", err)
	}
	return getMRDiffWithClient(ctx, gl, mrURL)
}

// formatPRContent builds the combined title + description + diff string that is
// passed to the AI provider.
func formatPRContent(title, body, rawDiff string) string {
	var sb strings.Builder
	sb.WriteString("PR Title: ")
	sb.WriteString(title)
	sb.WriteByte('\n')
	if strings.TrimSpace(body) != "" {
		sb.WriteString("PR Description: ")
		sb.WriteString(strings.TrimSpace(body))
		sb.WriteString("\n\n")
	} else {
		sb.WriteByte('\n')
	}
	sb.WriteString(rawDiff)
	return sb.String()
}

// isGitHubURL reports whether rawURL looks like a GitHub pull request URL.
// Detects github.com and self-hosted GitHub Enterprise instances by path shape.
func isGitHubURL(rawURL string) bool {
	return strings.Contains(rawURL, "/pull/")
}

// isGitLabURL reports whether rawURL looks like a GitLab merge request URL.
// Detects gitlab.com and self-hosted GitLab instances by path shape.
func isGitLabURL(rawURL string) bool {
	return strings.Contains(rawURL, "/-/merge_requests/")
}

// readDiffFromFile reads a raw diff from the given file path.
func readDiffFromFile(path string) (string, error) {
	bytes, err := os.ReadFile(path) //nolint:gosec // G304: reading user-supplied diff file is intentional
	return string(bytes), err
}

// splitDiffByFile splits a raw git diff into per-file chunks.
// Each chunk starts with a "diff --git" header and includes all hunks for that file.
func splitDiffByFile(raw string) []string {
	var chunks []string
	var current strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git") && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if current.Len() > 0 && strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// processDiff truncates the raw diff to at most maxLines lines to avoid
// exceeding provider context limits.
func processDiff(raw string, maxLines int) string {
	lines := strings.Split(raw, "\n")
	return truncateDiff(lines, maxLines)
}

// truncateDiff keeps the first and last halves of lines when the total exceeds
// max, inserting a marker at the cut point.
func truncateDiff(lines []string, max int) string {
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}
