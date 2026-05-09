package remote

import (
	"context"
	"fmt"
	"strings"
)

type TargetKind string

const (
	TargetGitHubPR TargetKind = "github-pr"
	TargetGitLabMR TargetKind = "gitlab-mr"
)

type Credentials struct {
	GitHubToken   string
	GitHubBaseURL string
	GitLabToken   string
	GitLabBaseURL string
}

type Target struct {
	URL  string
	Kind TargetKind
}

func ParseTarget(rawURL string) (Target, error) {
	switch {
	case IsGitHubURL(rawURL):
		return Target{URL: rawURL, Kind: TargetGitHubPR}, nil
	case IsGitLabURL(rawURL):
		return Target{URL: rawURL, Kind: TargetGitLabMR}, nil
	default:
		return Target{}, fmt.Errorf("unsupported PR/MR URL %q", rawURL)
	}
}

func (t Target) Diff(ctx context.Context, creds Credentials) (string, error) {
	switch t.Kind {
	case TargetGitHubPR:
		return GetPRDiff(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL)
	case TargetGitLabMR:
		return GetMRDiff(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL)
	default:
		return "", fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func (t Target) Metadata(ctx context.Context, creds Credentials) (Metadata, error) {
	switch t.Kind {
	case TargetGitHubPR:
		return GetGitHubPRMetadata(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL)
	case TargetGitLabMR:
		return GetGitLabMRMetadata(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL)
	default:
		return Metadata{}, fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func (t Target) UpdateMetadata(ctx context.Context, creds Credentials, title, description *string) error {
	switch t.Kind {
	case TargetGitHubPR:
		return UpdateGitHubPRMetadata(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL, title, description)
	case TargetGitLabMR:
		return UpdateGitLabMRMetadata(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL, title, description)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func (t Target) PostComment(ctx context.Context, creds Credentials, body string) error {
	switch t.Kind {
	case TargetGitHubPR:
		return PostGitHubPRComment(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL, body)
	case TargetGitLabMR:
		return PostGitLabMRNote(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL, body)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func (t Target) UpsertManagedComment(ctx context.Context, creds Credentials, marker, body string) error {
	switch t.Kind {
	case TargetGitHubPR:
		return UpsertGitHubPRComment(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL, marker, body)
	case TargetGitLabMR:
		return UpsertGitLabMRNote(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL, marker, body)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func (t Target) AddLabels(ctx context.Context, creds Credentials, labels []string) error {
	switch t.Kind {
	case TargetGitHubPR:
		return AddGitHubPRLabels(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL, labels)
	case TargetGitLabMR:
		return AddGitLabMRLabels(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL, labels)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func (t Target) RequestReviewers(ctx context.Context, creds Credentials, reviewers []string) error {
	switch t.Kind {
	case TargetGitHubPR:
		return RequestGitHubPRReviewers(ctx, t.URL, creds.GitHubToken, creds.GitHubBaseURL, reviewers)
	case TargetGitLabMR:
		return RequestGitLabMRReviewers(ctx, t.URL, creds.GitLabToken, creds.GitLabBaseURL, reviewers)
	default:
		return fmt.Errorf("unsupported PR/MR URL %q", t.URL)
	}
}

func FindOrCreateTargetFromRemote(ctx context.Context, creds Credentials, info Info, branch, title string) (Target, error) {
	switch {
	case IsGitHubHost(info.Host, creds.GitHubBaseURL):
		if len(info.PathParts) < 2 {
			return Target{}, fmt.Errorf("could not parse owner/repo from remote URL")
		}
		gh, err := NewGitHubClient(ctx, creds.GitHubToken, creds.GitHubBaseURL)
		if err != nil {
			return Target{}, err
		}
		url, err := FindOrCreateGitHubPR(ctx, gh, info.PathParts[0], info.PathParts[1], branch, title)
		if err != nil {
			return Target{}, err
		}
		return Target{URL: url, Kind: TargetGitHubPR}, nil
	case IsGitLabHost(info.Host, creds.GitLabBaseURL):
		gl, err := NewGitLabClient(creds.GitLabToken, creds.GitLabBaseURL)
		if err != nil {
			return Target{}, fmt.Errorf("creating GitLab client: %w", err)
		}
		url, err := FindOrCreateGitLabMR(ctx, gl, strings.Join(info.PathParts, "/"), branch, title)
		if err != nil {
			return Target{}, err
		}
		return Target{URL: url, Kind: TargetGitLabMR}, nil
	default:
		return Target{}, fmt.Errorf("unrecognised remote host %q; set github_base_url or gitlab_base_url in config", info.Host)
	}
}
