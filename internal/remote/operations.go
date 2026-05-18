package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gogithub "github.com/google/go-github/v68/github"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/oauth2"
)

// Metadata is the title and description/body for a remote PR or MR.
type Metadata struct {
	Title       string
	Description string
}

// NewGitHubClient returns a go-github client configured for public GitHub or GHE.
func NewGitHubClient(ctx context.Context, token, baseURL string) (*gogithub.Client, error) {
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

// NewGitLabClient returns a go-gitlab client configured for public or self-hosted GitLab.
func NewGitLabClient(token, baseURL string) (*gogitlab.Client, error) {
	var opts []gogitlab.ClientOptionFunc
	if baseURL != "" {
		opts = append(opts, gogitlab.WithBaseURL(baseURL))
	}
	return gogitlab.NewClient(token, opts...)
}

// FindOrCreateGitHubPR finds an open PR for branch on owner/repo or creates one.
func FindOrCreateGitHubPR(ctx context.Context, gh *gogithub.Client, owner, repo, branch, title string) (string, error) {
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

// FindOrCreateGitLabMR finds an open MR for branch in projectPath or creates one.
func FindOrCreateGitLabMR(ctx context.Context, gl *gogitlab.Client, projectPath, branch, title string) (string, error) {
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

// GetPRDiffWithClient fetches GitHub PR metadata and raw diff using gh.
func GetPRDiffWithClient(ctx context.Context, gh *gogithub.Client, prURL string) (string, error) {
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return "", err
	}

	pr, _, err := gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return "", wrapGitHubAuthError("fetching GitHub PR metadata", err)
	}

	opts := &gogithub.RawOptions{Type: gogithub.Diff}
	rawDiff, _, err := gh.PullRequests.GetRaw(ctx, owner, repo, number, *opts)
	if err != nil {
		return "", wrapGitHubAuthError("fetching GitHub PR diff", err)
	}

	return FormatPRContent(pr.GetTitle(), pr.GetBody(), rawDiff), nil
}

// GetPRDiff fetches GitHub PR metadata and raw diff using a token/base URL.
func GetPRDiff(ctx context.Context, prURL, token, baseURL string) (string, error) {
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return "", err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return "", err
	}
	return GetPRDiffWithClient(ctx, gh, prURL)
}

// GetMRDiffWithClient fetches GitLab MR metadata and raw diff using gl.
func GetMRDiffWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string) (string, error) {
	namespace, project, iid, err := ParseMRURL(mrURL)
	if err != nil {
		return "", err
	}

	projectPath := namespace + "/" + project
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
			diffBuilder.WriteString(formatGitLabMRDiff(c))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return FormatPRContent(mr.Title, mr.Description, diffBuilder.String()), nil
}

// GetMRDiff fetches GitLab MR metadata and raw diff using a token/base URL.
func GetMRDiff(ctx context.Context, mrURL, token, baseURL string) (string, error) {
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return "", err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return "", fmt.Errorf("creating GitLab client: %w", err)
	}
	return GetMRDiffWithClient(ctx, gl, mrURL)
}

// PostGitHubPRCommentWithClient posts body as a PR comment using gh.
func PostGitHubPRCommentWithClient(ctx context.Context, gh *gogithub.Client, prURL, body string) error {
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return err
	}
	_, _, err = gh.Issues.CreateComment(ctx, owner, repo, number, &gogithub.IssueComment{Body: &body})
	if err != nil {
		return fmt.Errorf("posting GitHub PR comment: %w", err)
	}
	return nil
}

// PostGitHubPRComment posts body as a comment on the GitHub PR at prURL.
func PostGitHubPRComment(ctx context.Context, prURL, token, baseURL, body string) error {
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return err
	}
	return PostGitHubPRCommentWithClient(ctx, gh, prURL, body)
}

// GetGitHubPRMetadataWithClient fetches title/body for a GitHub PR using gh.
func GetGitHubPRMetadataWithClient(ctx context.Context, gh *gogithub.Client, prURL string) (Metadata, error) {
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return Metadata{}, err
	}
	pr, _, err := gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return Metadata{}, fmt.Errorf("fetching GitHub PR metadata: %w", err)
	}
	return Metadata{Title: pr.GetTitle(), Description: pr.GetBody()}, nil
}

// GetGitHubPRMetadata fetches title/body for a GitHub PR.
func GetGitHubPRMetadata(ctx context.Context, prURL, token, baseURL string) (Metadata, error) {
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return Metadata{}, err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return Metadata{}, err
	}
	return GetGitHubPRMetadataWithClient(ctx, gh, prURL)
}

// UpdateGitHubPRMetadataWithClient updates the title and/or body of a GitHub PR.
func UpdateGitHubPRMetadataWithClient(ctx context.Context, gh *gogithub.Client, prURL string, title, body *string) error {
	if title == nil && body == nil {
		return nil
	}
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return err
	}
	_, _, err = gh.PullRequests.Edit(ctx, owner, repo, number, &gogithub.PullRequest{
		Title: title,
		Body:  body,
	})
	if err != nil {
		return fmt.Errorf("updating GitHub PR metadata: %w", err)
	}
	return nil
}

// UpdateGitHubPRMetadata updates the title and/or body of the GitHub PR at prURL.
func UpdateGitHubPRMetadata(ctx context.Context, prURL, token, baseURL string, title, body *string) error {
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return err
	}
	return UpdateGitHubPRMetadataWithClient(ctx, gh, prURL, title, body)
}

// UpsertGitHubPRCommentWithClient updates a marked comment or creates a new one.
func UpsertGitHubPRCommentWithClient(ctx context.Context, gh *gogithub.Client, prURL, marker, body string) error {
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return err
	}
	opts := &gogithub.IssueListCommentsOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := gh.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return fmt.Errorf("listing GitHub PR comments: %w", err)
		}
		for _, comment := range comments {
			if strings.Contains(comment.GetBody(), marker) {
				_, _, err = gh.Issues.EditComment(ctx, owner, repo, comment.GetID(), &gogithub.IssueComment{Body: &body})
				if err != nil {
					return fmt.Errorf("updating GitHub PR comment: %w", err)
				}
				return nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return PostGitHubPRCommentWithClient(ctx, gh, prURL, body)
}

// UpsertGitHubPRComment updates a marked comment or creates a new one.
func UpsertGitHubPRComment(ctx context.Context, prURL, token, baseURL, marker, body string) error {
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return err
	}
	return UpsertGitHubPRCommentWithClient(ctx, gh, prURL, marker, body)
}

// AddGitHubPRLabels adds labels to a GitHub PR.
func AddGitHubPRLabels(ctx context.Context, prURL, token, baseURL string, labels []string) error {
	labels = CleanStringList(labels)
	if len(labels) == 0 {
		return nil
	}
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return err
	}
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return err
	}
	_, _, err = gh.Issues.AddLabelsToIssue(ctx, owner, repo, number, labels)
	if err != nil {
		return fmt.Errorf("adding GitHub PR labels: %w", err)
	}
	return nil
}

// RequestGitHubPRReviewers requests reviewers on a GitHub PR.
func RequestGitHubPRReviewers(ctx context.Context, prURL, token, baseURL string, reviewers []string) error {
	reviewers = CleanStringList(reviewers)
	if len(reviewers) == 0 {
		return nil
	}
	resolvedBaseURL, err := ResolveGitHubBaseURL(prURL, baseURL)
	if err != nil {
		return err
	}
	gh, err := NewGitHubClient(ctx, token, resolvedBaseURL)
	if err != nil {
		return err
	}
	owner, repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return err
	}
	_, _, err = gh.PullRequests.RequestReviewers(ctx, owner, repo, number, gogithub.ReviewersRequest{Reviewers: reviewers})
	if err != nil {
		return fmt.Errorf("requesting GitHub PR reviewers: %w", err)
	}
	return nil
}

// PostGitLabMRNoteWithClient posts body as an MR note using gl.
func PostGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, body string) error {
	namespace, project, iid, err := ParseMRURL(mrURL)
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

// PostGitLabMRNote posts body as a note on the GitLab MR at mrURL.
func PostGitLabMRNote(ctx context.Context, mrURL, token, baseURL, body string) error {
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return fmt.Errorf("creating GitLab client: %w", err)
	}
	return PostGitLabMRNoteWithClient(ctx, gl, mrURL, body)
}

// GetGitLabMRMetadataWithClient fetches title/description for a GitLab MR using gl.
func GetGitLabMRMetadataWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string) (Metadata, error) {
	namespace, project, iid, err := ParseMRURL(mrURL)
	if err != nil {
		return Metadata{}, err
	}
	projectPath := namespace + "/" + project
	mr, _, err := gl.MergeRequests.GetMergeRequest(projectPath, iid, nil, gogitlab.WithContext(ctx))
	if err != nil {
		return Metadata{}, fmt.Errorf("fetching GitLab MR metadata: %w", err)
	}
	return Metadata{Title: mr.Title, Description: mr.Description}, nil
}

// GetGitLabMRMetadata fetches title/description for a GitLab MR.
func GetGitLabMRMetadata(ctx context.Context, mrURL, token, baseURL string) (Metadata, error) {
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return Metadata{}, err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return Metadata{}, fmt.Errorf("creating GitLab client: %w", err)
	}
	return GetGitLabMRMetadataWithClient(ctx, gl, mrURL)
}

// UpdateGitLabMRMetadataWithClient updates the title and/or description of a GitLab MR.
func UpdateGitLabMRMetadataWithClient(ctx context.Context, gl *gogitlab.Client, mrURL string, title, description *string) error {
	if title == nil && description == nil {
		return nil
	}
	namespace, project, iid, err := ParseMRURL(mrURL)
	if err != nil {
		return err
	}
	projectPath := namespace + "/" + project
	_, _, err = gl.MergeRequests.UpdateMergeRequest(projectPath, iid, &gogitlab.UpdateMergeRequestOptions{
		Title:       title,
		Description: description,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("updating GitLab MR metadata: %w", err)
	}
	return nil
}

// UpdateGitLabMRMetadata updates the title and/or description of the GitLab MR at mrURL.
func UpdateGitLabMRMetadata(ctx context.Context, mrURL, token, baseURL string, title, description *string) error {
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return fmt.Errorf("creating GitLab client: %w", err)
	}
	return UpdateGitLabMRMetadataWithClient(ctx, gl, mrURL, title, description)
}

// UpsertGitLabMRNoteWithClient updates a marked note or creates a new one.
func UpsertGitLabMRNoteWithClient(ctx context.Context, gl *gogitlab.Client, mrURL, marker, body string) error {
	namespace, project, iid, err := ParseMRURL(mrURL)
	if err != nil {
		return err
	}
	projectPath := namespace + "/" + project
	opts := &gogitlab.ListMergeRequestNotesOptions{
		ListOptions: gogitlab.ListOptions{PerPage: 100},
	}
	for {
		notes, resp, err := gl.Notes.ListMergeRequestNotes(projectPath, iid, opts, gogitlab.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("listing GitLab MR notes: %w", err)
		}
		for _, note := range notes {
			if strings.Contains(note.Body, marker) {
				_, _, err = gl.Notes.UpdateMergeRequestNote(projectPath, iid, note.ID, &gogitlab.UpdateMergeRequestNoteOptions{
					Body: &body,
				}, gogitlab.WithContext(ctx))
				if err != nil {
					return fmt.Errorf("updating GitLab MR note: %w", err)
				}
				return nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return PostGitLabMRNoteWithClient(ctx, gl, mrURL, body)
}

// UpsertGitLabMRNote updates a marked note or creates a new one.
func UpsertGitLabMRNote(ctx context.Context, mrURL, token, baseURL, marker, body string) error {
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return fmt.Errorf("creating GitLab client: %w", err)
	}
	return UpsertGitLabMRNoteWithClient(ctx, gl, mrURL, marker, body)
}

// AddGitLabMRLabels adds labels to a GitLab MR.
func AddGitLabMRLabels(ctx context.Context, mrURL, token, baseURL string, labels []string) error {
	labels = CleanStringList(labels)
	if len(labels) == 0 {
		return nil
	}
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return fmt.Errorf("creating GitLab client: %w", err)
	}
	namespace, project, iid, err := ParseMRURL(mrURL)
	if err != nil {
		return err
	}
	projectPath := namespace + "/" + project
	labelOptions := gogitlab.LabelOptions(labels)
	_, _, err = gl.MergeRequests.UpdateMergeRequest(projectPath, iid, &gogitlab.UpdateMergeRequestOptions{
		AddLabels: &labelOptions,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("adding GitLab MR labels: %w", err)
	}
	return nil
}

// RequestGitLabMRReviewers requests GitLab reviewers by numeric user ID.
func RequestGitLabMRReviewers(ctx context.Context, mrURL, token, baseURL string, reviewers []string) error {
	reviewers = CleanStringList(reviewers)
	if len(reviewers) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(reviewers))
	for _, reviewer := range reviewers {
		id, err := strconv.ParseInt(reviewer, 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("GitLab reviewer %q must be a numeric user ID", reviewer)
		}
		ids = append(ids, id)
	}
	resolvedBaseURL, err := ResolveGitLabBaseURL(mrURL, baseURL)
	if err != nil {
		return err
	}
	gl, err := NewGitLabClient(token, resolvedBaseURL)
	if err != nil {
		return fmt.Errorf("creating GitLab client: %w", err)
	}
	namespace, project, iid, err := ParseMRURL(mrURL)
	if err != nil {
		return err
	}
	projectPath := namespace + "/" + project
	_, _, err = gl.MergeRequests.UpdateMergeRequest(projectPath, iid, &gogitlab.UpdateMergeRequestOptions{
		ReviewerIDs: &ids,
	}, gogitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("requesting GitLab MR reviewers: %w", err)
	}
	return nil
}

// CleanStringList trims, comma-splits, deduplicates, and drops empty values.
func CleanStringList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			cleaned = append(cleaned, part)
		}
	}
	return cleaned
}

// FormatPRContent builds the combined title + description + diff string for provider prompts.
func FormatPRContent(title, body, rawDiff string) string {
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

func formatGitLabMRDiff(c *gogitlab.MergeRequestDiff) string {
	if c == nil {
		return ""
	}
	diff := ensureTrailingNewline(c.Diff)
	if strings.HasPrefix(diff, "diff --git ") {
		return diff
	}

	oldPath := c.OldPath
	newPath := c.NewPath
	if oldPath == "" {
		oldPath = newPath
	}
	if newPath == "" {
		newPath = oldPath
	}
	if oldPath == "" && newPath == "" {
		return diff
	}

	var b strings.Builder
	b.WriteString("diff --git ")
	b.WriteString(formatGitPathForDiff("a/", oldPath))
	b.WriteByte(' ')
	b.WriteString(formatGitPathForDiff("b/", newPath))
	b.WriteByte('\n')
	if c.NewFile {
		b.WriteString("--- /dev/null\n")
	} else {
		b.WriteString("--- ")
		b.WriteString(formatGitPathForDiff("a/", oldPath))
		b.WriteByte('\n')
	}
	if c.DeletedFile {
		b.WriteString("+++ /dev/null\n")
	} else {
		b.WriteString("+++ ")
		b.WriteString(formatGitPathForDiff("b/", newPath))
		b.WriteByte('\n')
	}
	b.WriteString(diff)
	return b.String()
}

func formatGitPathForDiff(prefix, path string) string {
	path = prefix + path
	if strings.ContainsAny(path, " \t\n\r\"\\") {
		return strconv.Quote(path)
	}
	return path
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
