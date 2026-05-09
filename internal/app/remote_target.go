package app

import (
	"context"

	"github.com/pbsladek/ai-mr-comment/internal/remote"
)

func remoteCredentials(cfg *Config) remote.Credentials {
	return remote.Credentials{
		GitHubToken:   cfg.GitHubToken,
		GitHubBaseURL: cfg.GitHubBaseURL,
		GitLabToken:   cfg.GitLabToken,
		GitLabBaseURL: cfg.GitLabBaseURL,
	}
}

func getRemoteDiff(ctx context.Context, cfg *Config, targetURL string) (string, error) {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return "", err
	}
	return target.Diff(ctx, remoteCredentials(cfg))
}

func postRemoteComment(ctx context.Context, cfg *Config, targetURL, body string) error {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return err
	}
	return target.PostComment(ctx, remoteCredentials(cfg), body)
}

func parseRemoteTarget(targetURL string) (remote.Target, error) {
	return remote.ParseTarget(targetURL)
}

func findOrCreateRemoteTarget(ctx context.Context, cfg *Config, info remoteInfo, branch, title string) (string, error) {
	target, err := remote.FindOrCreateTargetFromRemote(ctx, remoteCredentials(cfg), info, branch, title)
	if err != nil {
		return "", err
	}
	return target.URL, nil
}
