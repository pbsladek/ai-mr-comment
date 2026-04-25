package main

import (
	"context"
	_ "embed"
	"errors"
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

func parseURLHost(rawURL string) (scheme, host, hostname string, err error) {
	clean := parseHostedURL(rawURL)
	u, parseErr := url.Parse(clean)
	if parseErr != nil || u.Host == "" || u.Scheme == "" {
		return "", "", "", fmt.Errorf("invalid URL %q: must be a valid http(s) URL", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", "", fmt.Errorf("invalid URL %q: only http(s) URLs are supported", rawURL)
	}
	return u.Scheme, u.Host, strings.ToLower(u.Hostname()), nil
}

func normalizeConfiguredBaseURL(rawBaseURL, provider string) (string, string, error) {
	u, err := url.Parse(rawBaseURL)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return "", "", fmt.Errorf("invalid %s_base_url %q: must be a valid http(s) URL", provider, rawBaseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("invalid %s_base_url %q: only http(s) URLs are supported", provider, rawBaseURL)
	}
	return u.Scheme + "://" + u.Host, strings.ToLower(u.Hostname()), nil
}

func resolveGitHubBaseURL(prURL, configuredBaseURL string) (string, error) {
	scheme, host, hostname, err := parseURLHost(prURL)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub PR URL %q: %w", prURL, err)
	}
	if configuredBaseURL == "" {
		if hostname == "github.com" {
			return "", nil
		}
		// For self-hosted instances, auto-derive the enterprise base URL from the PR URL host.
		return scheme + "://" + host, nil
	}
	normalizedBase, baseHost, err := normalizeConfiguredBaseURL(configuredBaseURL, "github")
	if err != nil {
		return "", err
	}
	if baseHost != hostname {
		return "", fmt.Errorf("GitHub PR URL host %q does not match github_base_url host %q", host, baseHost)
	}
	return normalizedBase, nil
}

func resolveGitLabBaseURL(mrURL, configuredBaseURL string) (string, error) {
	scheme, host, hostname, err := parseURLHost(mrURL)
	if err != nil {
		return "", fmt.Errorf("invalid GitLab MR URL %q: %w", mrURL, err)
	}
	if configuredBaseURL == "" {
		if hostname == "gitlab.com" {
			return "", nil
		}
		// For self-hosted instances, auto-derive the API base URL from the MR URL host.
		return scheme + "://" + host, nil
	}
	normalizedBase, baseHost, err := normalizeConfiguredBaseURL(configuredBaseURL, "gitlab")
	if err != nil {
		return "", err
	}
	if baseHost != hostname {
		return "", fmt.Errorf("GitLab MR URL host %q does not match gitlab_base_url host %q", host, baseHost)
	}
	return normalizedBase, nil
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
	// Normalise SSH → HTTPS form.
	// git@github.com:owner/repo.git → https://github.com/owner/repo.git
	// git@gitlab.com:group/proj.git → https://gitlab.com/group/proj.git
	raw := remoteURL
	if strings.HasPrefix(raw, "git@") {
		raw = strings.TrimPrefix(raw, "git@")
		raw = strings.Replace(raw, ":", "/", 1)
		raw = "https://" + raw
	}
	raw = strings.TrimSuffix(raw, ".git")

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}

	host := strings.ToLower(u.Host)
	path := strings.Trim(u.Path, "/")

	switch {
	case strings.Contains(host, "github"):
		// https://github.com/owner/repo/compare/branch-name?expand=1
		return "https://" + u.Host + "/" + path + "/compare/" + url.PathEscape(branch) + "?expand=1"
	case strings.Contains(host, "gitlab"):
		// https://gitlab.com/group/project/-/merge_requests/new?merge_request[source_branch]=branch-name
		q := url.Values{}
		q.Set("merge_request[source_branch]", branch)
		return "https://" + u.Host + "/" + path + "/-/merge_requests/new?" + q.Encode()
	}
	return ""
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
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", 0, fmt.Errorf("invalid GitHub PR URL %q: only http(s) URLs are supported", prURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" || parts[0] == "" || parts[1] == "" || parts[3] == "" {
		return "", "", 0, fmt.Errorf("invalid GitHub PR URL %q: expected .../{owner}/{repo}/pull/{number}", prURL)
	}
	var num int
	if n, scanErr := fmt.Sscanf(parts[3], "%d", &num); scanErr != nil || n != 1 || num <= 0 || fmt.Sprintf("%d", num) != parts[3] {
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
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: only http(s) URLs are supported", mrURL)
	}
	const marker = "/-/merge_requests/"
	idx := strings.Index(u.Path, marker)
	if idx == -1 {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: expected .../-/merge_requests/{iid}", mrURL)
	}
	projectPath := strings.Trim(u.Path[:idx], "/") // e.g. "group/subgroup/project"
	iidStr := u.Path[idx+len(marker):]             // e.g. "42"
	if projectPath == "" || iidStr == "" {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: missing project path or MR IID", mrURL)
	}
	slashIdx := strings.LastIndex(projectPath, "/")
	if slashIdx == -1 {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: expected {namespace}/{project}", mrURL)
	}
	var num int64
	if n, scanErr := fmt.Sscanf(iidStr, "%d", &num); scanErr != nil || n != 1 || num <= 0 || fmt.Sprintf("%d", num) != iidStr {
		return "", "", 0, fmt.Errorf("invalid GitLab MR URL %q: MR IID must be a positive integer", mrURL)
	}
	return projectPath[:slashIdx], projectPath[slashIdx+1:], num, nil
}

// remoteInfo holds parsed components of a git remote URL.
type remoteInfo struct {
	Host      string   // e.g. "github.com" or "gitlab.myco.com"
	PathParts []string // path segments, e.g. ["owner","repo"] or ["group","sub","project"]
}

// parseRemoteInfo normalises a raw git remote URL (SSH or HTTPS) and extracts
// the host and path segments. Handles git@host:path.git and https://host/path.git.
func parseRemoteInfo(rawURL string) (remoteInfo, error) {
	raw := rawURL
	if strings.HasPrefix(raw, "git@") {
		raw = strings.TrimPrefix(raw, "git@")
		raw = strings.Replace(raw, ":", "/", 1)
		raw = "https://" + raw
	}
	raw = strings.TrimSuffix(raw, ".git")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return remoteInfo{}, fmt.Errorf("cannot parse remote URL %q", rawURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		return remoteInfo{}, fmt.Errorf("remote URL %q has too few path segments", rawURL)
	}
	return remoteInfo{Host: strings.ToLower(u.Host), PathParts: parts}, nil
}

// isGitHubHost reports whether host belongs to GitHub (github.com or GHE instance).
func isGitHubHost(host, configuredBaseURL string) bool {
	if strings.Contains(host, "github") {
		return true
	}
	if configuredBaseURL == "" {
		return false
	}
	u, err := url.Parse(configuredBaseURL)
	return err == nil && strings.ToLower(u.Hostname()) == host
}

// isGitLabHost reports whether host belongs to GitLab (gitlab.com or self-hosted).
func isGitLabHost(host, configuredBaseURL string) bool {
	if strings.Contains(host, "gitlab") {
		return true
	}
	if configuredBaseURL == "" {
		return false
	}
	u, err := url.Parse(configuredBaseURL)
	return err == nil && strings.ToLower(u.Hostname()) == host
}

// findOrCreateGitHubPR finds an open PR for branch on owner/repo, or creates
// one targeting the repo's default branch. Returns the PR HTML URL.
func findOrCreateGitHubPR(ctx context.Context, gh *gogithub.Client, owner, repo, branch, title string) (string, error) {
	// Search for an existing open PR from this branch.
	prs, _, err := gh.PullRequests.List(ctx, owner, repo, &gogithub.PullRequestListOptions{
		State: "open",
		Head:  owner + ":" + branch,
	})
	if err != nil {
		return "", wrapGitHubAuthError("listing GitHub PRs", err)
	}
	if len(prs) > 0 {
		return prs[0].GetHTMLURL(), nil
	}

	// No existing PR — find the default branch then create one.
	repoInfo, _, err := gh.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", wrapGitHubAuthError("getting GitHub repo info", err)
	}
	base := repoInfo.GetDefaultBranch()
	if base == "" {
		base = "main"
	}
	pr, _, err := gh.PullRequests.Create(ctx, owner, repo, &gogithub.NewPullRequest{
		Title: &title,
		Head:  &branch,
		Base:  &base,
	})
	if err != nil {
		return "", wrapGitHubAuthError("creating GitHub PR", err)
	}
	return pr.GetHTMLURL(), nil
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
	state := "opened"
	mrs, _, err := gl.MergeRequests.ListProjectMergeRequests(projectPath, &gogitlab.ListProjectMergeRequestsOptions{
		State:        &state,
		SourceBranch: &branch,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return "", wrapGitLabAuthError("listing GitLab MRs", err)
	}
	if len(mrs) > 0 {
		return mrs[0].WebURL, nil
	}

	// No existing MR — find the default branch then create one.
	proj, _, err := gl.Projects.GetProject(projectPath, nil, gogitlab.WithContext(ctx))
	if err != nil {
		return "", wrapGitLabAuthError("getting GitLab project info", err)
	}
	base := proj.DefaultBranch
	if base == "" {
		base = "main"
	}
	mr, _, err := gl.MergeRequests.CreateMergeRequest(projectPath, &gogitlab.CreateMergeRequestOptions{
		Title:        &title,
		SourceBranch: &branch,
		TargetBranch: &base,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return "", wrapGitLabAuthError("creating GitLab MR", err)
	}
	return mr.WebURL, nil
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

// wrapGitHubAuthError inspects a go-github error and, for 401/403/404
// responses, appends a hint about setting GITHUB_TOKEN.
func wrapGitHubAuthError(msg string, err error) error {
	var ghErr *gogithub.ErrorResponse
	if errors.As(err, &ghErr) {
		switch ghErr.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("%s: %w (set GITHUB_TOKEN for private repos)", msg, err)
		}
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// wrapGitLabAuthError inspects a go-gitlab error and, for 401/403/404
// responses, appends a hint about setting GITLAB_TOKEN.
func wrapGitLabAuthError(msg string, err error) error {
	var glErr *gogitlab.ErrorResponse
	if errors.As(err, &glErr) {
		switch glErr.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("%s: %w (set GITLAB_TOKEN for private repos)", msg, err)
		}
	}
	return fmt.Errorf("%s: %w", msg, err)
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
		return "", wrapGitHubAuthError("fetching GitHub PR metadata", err)
	}

	// Fetch the raw unified diff via the SDK diff option.
	opts := &gogithub.RawOptions{Type: gogithub.Diff}
	rawDiff, _, err := gh.PullRequests.GetRaw(ctx, owner, repo, number, *opts)
	if err != nil {
		return "", wrapGitHubAuthError("fetching GitHub PR diff", err)
	}

	return formatPRContent(pr.GetTitle(), pr.GetBody(), rawDiff), nil
}

// getPRDiff fetches the diff and metadata for a GitHub pull request using the
// official go-github SDK and returns a string with the PR title, optional
// description, and raw unified diff. token may be empty for public repositories.
// baseURL may be empty for github.com, or set to a GitHub Enterprise host.
func getPRDiff(ctx context.Context, prURL, token, baseURL string) (string, error) {
	resolvedBaseURL, err := resolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return "", err
	}
	gh, err := newGitHubClient(ctx, token, resolvedBaseURL)
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
		return "", wrapGitLabAuthError("fetching GitLab MR metadata", err)
	}

	var diffBuilder strings.Builder
	opts := &gogitlab.ListMergeRequestDiffsOptions{
		ListOptions: gogitlab.ListOptions{PerPage: 100},
	}
	for {
		changes, resp, err := gl.MergeRequests.ListMergeRequestDiffs(projectPath, iid, opts, gogitlab.WithContext(ctx))
		if err != nil {
			return "", wrapGitLabAuthError("fetching GitLab MR diff", err)
		}
		for _, c := range changes {
			diffBuilder.WriteString(c.Diff)
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return formatPRContent(mr.Title, mr.Description, diffBuilder.String()), nil
}

// getMRDiff fetches the diff and metadata for a GitLab merge request using the
// official GitLab Go SDK and returns a string with the MR title, optional
// description, and raw unified diff. token may be empty for public projects.
// baseURL may be empty for gitlab.com, or set to a self-hosted GitLab host.
func getMRDiff(ctx context.Context, mrURL, token, baseURL string) (string, error) {
	resolvedBaseURL, err := resolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return "", err
	}
	gl, err := newGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return "", fmt.Errorf("creating GitLab client: %w", err)
	}
	return getMRDiffWithClient(ctx, gl, mrURL)
}

// postGitHubPRCommentWithClient posts body as a PR comment using the given client.
// Separated from postGitHubPRComment to allow tests to inject a client pointed
// at a local httptest server.
func postGitHubPRCommentWithClient(ctx context.Context, gh *gogithub.Client, prURL, body string) error {
	owner, repo, number, err := parsePRURL(prURL)
	if err != nil {
		return err
	}
	_, _, err = gh.Issues.CreateComment(ctx, owner, repo, number, &gogithub.IssueComment{Body: &body})
	if err != nil {
		return fmt.Errorf("posting GitHub PR comment: %w", err)
	}
	return nil
}

// postGitHubPRComment posts body as a comment on the GitHub PR at prURL.
func postGitHubPRComment(ctx context.Context, prURL, token, baseURL, body string) error {
	resolvedBaseURL, err := resolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return err
	}
	gh, err := newGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return err
	}
	return postGitHubPRCommentWithClient(ctx, gh, prURL, body)
}

// postGitLabMRNoteWithClient posts body as an MR note using the given client.
// Separated from postGitLabMRNote to allow tests to inject a client pointed
// at a local httptest server.
func postGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, body string) error {
	namespace, project, iid, err := parseMRURL(mrURL)
	if err != nil {
		return err
	}
	projectPath := namespace + "/" + project
	_, _, err = gl.Notes.CreateMergeRequestNote(projectPath, iid, &gogitlab.CreateMergeRequestNoteOptions{
		Body: &body,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("posting GitLab MR note: %w", err)
	}
	return nil
}

// postGitLabMRNote posts body as a note on the GitLab MR at mrURL.
func postGitLabMRNote(ctx context.Context, mrURL, token, baseURL, body string) error {
	resolvedBaseURL, err := resolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return err
	}
	gl, err := newGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return fmt.Errorf("creating GitLab client: %w", err)
	}
	return postGitLabMRNoteWithClient(ctx, gl, mrURL, body)
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
	_, _, _, err := parsePRURL(rawURL)
	return err == nil
}

// isGitLabURL reports whether rawURL looks like a GitLab merge request URL.
// Detects gitlab.com and self-hosted GitLab instances by path shape.
func isGitLabURL(rawURL string) bool {
	_, _, _, err := parseMRURL(rawURL)
	return err == nil
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
	if max <= 0 {
		return strings.Join(lines, "\n")
	}
	if len(lines) <= max {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:max/2], "\n")
	tail := strings.Join(lines[len(lines)-(max/2):], "\n")
	return head + "\n[...diff truncated...]\n" + tail
}
