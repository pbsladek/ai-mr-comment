package remote

import (
	"fmt"
	"net/url"
	"strings"
)

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

// ResolveGitHubBaseURL returns the API base URL needed by go-github for prURL.
func ResolveGitHubBaseURL(prURL, configuredBaseURL string) (string, error) {
	_, host, hostname, err := parseURLHost(prURL)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub PR URL %q: %w", prURL, err)
	}
	if configuredBaseURL == "" {
		if hostname == "github.com" {
			return "", nil
		}
		return "", fmt.Errorf("GitHub PR URL host %q is not github.com; set github_base_url for GitHub Enterprise hosts", host)
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

// ResolveGitLabBaseURL returns the API base URL needed by go-gitlab for mrURL.
func ResolveGitLabBaseURL(mrURL, configuredBaseURL string) (string, error) {
	_, host, hostname, err := parseURLHost(mrURL)
	if err != nil {
		return "", fmt.Errorf("invalid GitLab MR URL %q: %w", mrURL, err)
	}
	if configuredBaseURL == "" {
		if hostname == "gitlab.com" {
			return "", nil
		}
		return "", fmt.Errorf("GitLab MR URL host %q is not gitlab.com; set gitlab_base_url for self-hosted GitLab hosts", host)
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

// CreateURL converts a git remote URL and branch name into a browser URL for
// creating a new PR (GitHub) or MR (GitLab). It returns "" for unknown hosts.
func CreateURL(remoteURL, branch string) string {
	return CreateURLWithBase(remoteURL, branch, "", "")
}

// CreateURLWithBase converts a git remote URL and branch name into a browser URL
// for creating a new PR (GitHub) or MR (GitLab), accepting configured self-hosted
// base URLs. It returns "" for unknown hosts.
func CreateURLWithBase(remoteURL, branch, githubBaseURL, gitlabBaseURL string) string {
	raw := remoteURL
	if stripped, ok := strings.CutPrefix(raw, "git@"); ok {
		raw = stripped
		raw = strings.Replace(raw, ":", "/", 1)
		raw = "https://" + raw
	}
	raw = strings.TrimSuffix(raw, ".git")

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}

	hostname := strings.ToLower(u.Hostname())
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}

	switch {
	case IsGitHubHost(hostname, githubBaseURL):
		return "https://" + u.Host + "/" + path + "/compare/" + url.PathEscape(branch) + "?expand=1"
	case IsGitLabHost(hostname, gitlabBaseURL):
		q := url.Values{}
		q.Set("merge_request[source_branch]", branch)
		return "https://" + u.Host + "/" + path + "/-/merge_requests/new?" + q.Encode()
	}
	return ""
}

// ParsePRURL extracts the owner, repo, and PR number from a GitHub PR URL.
func ParsePRURL(prURL string) (owner, repo string, number int, err error) {
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

// ParseMRURL extracts the namespace, project name, and MR IID from a GitLab MR URL.
func ParseMRURL(mrURL string) (namespace, project string, iid int64, err error) {
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
	projectPath := strings.Trim(u.Path[:idx], "/")
	iidStr := u.Path[idx+len(marker):]
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

// Info holds parsed components of a git remote URL.
type Info struct {
	Host      string
	PathParts []string
}

// ParseInfo normalises a raw git remote URL and extracts host/path components.
func ParseInfo(rawURL string) (Info, error) {
	raw := rawURL
	if stripped, ok := strings.CutPrefix(raw, "git@"); ok {
		raw = stripped
		raw = strings.Replace(raw, ":", "/", 1)
		raw = "https://" + raw
	}
	raw = strings.TrimSuffix(raw, ".git")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Info{}, fmt.Errorf("cannot parse remote URL %q", rawURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		return Info{}, fmt.Errorf("remote URL %q has too few path segments", rawURL)
	}
	return Info{Host: strings.ToLower(u.Hostname()), PathParts: parts}, nil
}

// IsGitHubHost reports whether host belongs to GitHub.
func IsGitHubHost(host, configuredBaseURL string) bool {
	host = strings.ToLower(host)
	if host == "github.com" {
		return true
	}
	if configuredBaseURL == "" {
		return false
	}
	u, err := url.Parse(configuredBaseURL)
	return err == nil && strings.ToLower(u.Hostname()) == host
}

// IsGitLabHost reports whether host belongs to GitLab.
func IsGitLabHost(host, configuredBaseURL string) bool {
	host = strings.ToLower(host)
	if host == "gitlab.com" {
		return true
	}
	if configuredBaseURL == "" {
		return false
	}
	u, err := url.Parse(configuredBaseURL)
	return err == nil && strings.ToLower(u.Hostname()) == host
}

// IsGitHubURL reports whether rawURL looks like a GitHub pull request URL.
func IsGitHubURL(rawURL string) bool {
	_, _, _, err := ParsePRURL(rawURL)
	return err == nil
}

// IsGitLabURL reports whether rawURL looks like a GitLab merge request URL.
func IsGitLabURL(rawURL string) bool {
	_, _, _, err := ParseMRURL(rawURL)
	return err == nil
}
