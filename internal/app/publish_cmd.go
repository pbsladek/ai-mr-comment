package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const managedDescriptionStart = "<!-- ai-mr-comment:description:start -->"
const managedDescriptionEnd = "<!-- ai-mr-comment:description:end -->"
const managedCommentMarker = "<!-- ai-mr-comment:comment -->"

func newPublishCmdWithDeps(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), deps commandDeps) *cobra.Command {
	var prURL, provider, modelOverride, templateName, profileName, format string
	var dryRun, noUpdateTitle, noUpdateDescription, replaceDescription, postSummary, autoLabels, draftIfRisky bool
	var labels, reviewers []string

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Generate and sync PR/MR title, description, labels, reviewers, and managed summary",
		Long: `Generates a title and description, then synchronizes them to a remote
GitHub PR or GitLab MR. Pass --pr to target an existing PR/MR, or omit --pr to
find or create one from the current branch and origin remote.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.loadConfig(profileName)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if cmd.Flags().Changed("template") {
				cfg.Template = templateName
			}
			if !isSupportedProvider(cfg.Provider) {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}
			if format != "text" && format != "json" {
				return withExitCode(4, fmt.Errorf("unsupported format %q: must be text or json", format))
			}
			if noUpdateTitle && noUpdateDescription && !postSummary && len(labels) == 0 && len(reviewers) == 0 && !autoLabels {
				return withExitCode(4, errors.New("publish has no remote actions enabled"))
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}

			diffContent, targetURL, err := resolvePublishDiffWithDeps(cmd.Context(), cfg, prURL, deps)
			if err != nil {
				return err
			}
			if strings.TrimSpace(diffContent) == "" {
				return withExitCode(3, errors.New("no diff found to publish"))
			}
			summary := summarizeDiff(diffContent, "publish", getModelName(cfg), len(strings.Split(diffContent, "\n")) > 4000)
			diffContent = processDiff(diffContent, 4000)

			systemPrompt, templateErr := NewPromptTemplate(cfg.Template)
			if templateErr != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", templateErr)
			}

			var title, description string
			eg, egCtx := errgroup.WithContext(cmd.Context())
			eg.Go(func() error {
				var callErr error
				title, callErr = timedCall(cfg, "publish-title", func() (string, error) {
					return chatFn(egCtx, cfg, cfg.Provider, titlePrompt, diffContent)
				})
				return callErr
			})
			eg.Go(func() error {
				var callErr error
				description, callErr = timedCall(cfg, "publish-description", func() (string, error) {
					return chatFn(egCtx, cfg, cfg.Provider, systemPrompt, diffContent)
				})
				return callErr
			})
			if err := eg.Wait(); err != nil {
				return err
			}
			title = strings.TrimSpace(title)
			description = strings.TrimSpace(description)
			if draftIfRisky && publishLooksRisky(description) {
				title = ensureDraftTitle(title)
			}

			appliedLabels := cleanStringList(labels)
			if autoLabels {
				appliedLabels = cleanStringList(append(appliedLabels, derivePublishLabels(summary, description)...))
			}
			reviewers = cleanStringList(reviewers)

			if targetURL == "" {
				if dryRun {
					targetURL, err = plannedPublishTargetWithDeps(cfg, deps)
				} else {
					targetURL, err = findOrCreatePublishTargetWithDeps(cmd.Context(), cfg, title, deps)
				}
				if err != nil {
					return err
				}
			}

			updateTitle := !noUpdateTitle
			updateDescription := !noUpdateDescription
			var updateTitleValue *string
			var updateDescriptionValue *string
			if updateTitle {
				updateTitleValue = &title
			}
			if dryRun {
				return writePublishDryRun(cmd, cfg, targetURL, title, description, updateTitle, updateDescription, postSummary, appliedLabels, reviewers)
			}
			if updateDescription {
				body := description
				if !replaceDescription {
					metadata, metaErr := deps.getRemoteMetadata(cmd.Context(), cfg, targetURL)
					if metaErr != nil {
						return metaErr
					}
					body = mergeManagedSection(metadata.Description, description)
				}
				updateDescriptionValue = &body
			}

			if updateTitle || updateDescription {
				if err := deps.updateRemoteMetadata(cmd.Context(), cfg, targetURL, updateTitleValue, updateDescriptionValue); err != nil {
					return err
				}
			}
			if postSummary {
				if err := deps.upsertRemoteManagedComment(cmd.Context(), cfg, targetURL, buildManagedComment(title, description)); err != nil {
					return err
				}
			}
			if len(appliedLabels) > 0 {
				if err := deps.addRemoteLabels(cmd.Context(), cfg, targetURL, appliedLabels); err != nil {
					return err
				}
			}
			if len(reviewers) > 0 {
				if err := deps.requestRemoteReviewers(cmd.Context(), cfg, targetURL, reviewers); err != nil {
					return err
				}
			}

			if format == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					URL                string   `json:"url"`
					Title              string   `json:"title"`
					DescriptionUpdated bool     `json:"description_updated"`
					CommentUpserted    bool     `json:"comment_upserted"`
					Labels             []string `json:"labels,omitempty"`
					Reviewers          []string `json:"reviewers,omitempty"`
					Provider           string   `json:"provider"`
					Model              string   `json:"model"`
				}{
					URL:                targetURL,
					Title:              title,
					DescriptionUpdated: updateDescription,
					CommentUpserted:    postSummary,
					Labels:             appliedLabels,
					Reviewers:          reviewers,
					Provider:           string(cfg.Provider),
					Model:              getModelName(cfg),
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Published PR/MR metadata: %s\n", targetURL)
			return nil
		},
	}

	cmd.Flags().StringVar(&prURL, "pr", "", "GitHub PR or GitLab MR URL; omitted means find or create from the current branch")
	cmd.Flags().StringVar(&provider, "provider", "openai", "AI provider to use")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVarP(&templateName, "template", "t", "default", "Prompt template to use")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview planned remote changes without writing them")
	cmd.Flags().BoolVar(&noUpdateTitle, "no-update-title", false, "Do not update the remote PR/MR title")
	cmd.Flags().BoolVar(&noUpdateDescription, "no-update-description", false, "Do not update the remote PR/MR description/body")
	cmd.Flags().BoolVar(&replaceDescription, "replace-description", false, "Replace the full remote description instead of syncing a managed section")
	cmd.Flags().BoolVar(&postSummary, "post-summary", true, "Create or update a managed PR/MR summary comment")
	cmd.Flags().BoolVar(&autoLabels, "auto-labels", false, "Apply simple labels inferred from the changed files and generated description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "Label to add to the PR/MR; can be repeated or comma-separated")
	cmd.Flags().StringArrayVar(&reviewers, "reviewer", nil, "Reviewer to request; GitHub uses usernames, GitLab uses numeric user IDs")
	cmd.Flags().BoolVar(&draftIfRisky, "draft-if-risky", false, "Prefix the title with Draft: when generated text indicates high risk")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("template", completeValues(templateNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	return cmd
}

func resolvePublishDiffWithDeps(ctx context.Context, cfg *Config, prURL string, deps commandDeps) (diffContent, targetURL string, err error) {
	if prURL != "" {
		switch {
		case deps.isGitHubURL(prURL), deps.isGitLabURL(prURL):
			diffContent, err = deps.getRemoteDiff(ctx, cfg, prURL)
		default:
			return "", "", fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", prURL)
		}
		return diffContent, prURL, err
	}
	if !deps.isGitRepo() {
		return "", "", errors.New("not a git repository. Pass --pr to publish a remote PR/MR without a local checkout")
	}
	branch, err := deps.getCurrentBranch()
	if err != nil {
		return "", "", fmt.Errorf("could not determine current branch: %w", err)
	}
	diffContent, err = deps.getGitDiff("", false, nil)
	if err != nil {
		return "", "", fmt.Errorf("reading local diff: %w", err)
	}
	if branch != "" {
		diffContent = "Branch: " + branch + "\n\n" + diffContent
	}
	return diffContent, "", nil
}

func findOrCreatePublishTargetWithDeps(ctx context.Context, cfg *Config, title string, deps commandDeps) (string, error) {
	branch, err := deps.getCurrentBranch()
	if err != nil {
		return "", fmt.Errorf("could not determine current branch: %w", err)
	}
	if branch == "" {
		return "", errors.New("cannot publish from detached HEAD without --pr")
	}
	remoteURL, err := deps.getRemoteURL()
	if err != nil {
		return "", fmt.Errorf("getting remote URL: %w", err)
	}
	info, err := deps.parseRemoteInfo(remoteURL)
	if err != nil {
		return "", err
	}
	switch {
	case deps.isGitHubHost(info.Host, cfg.GitHubBaseURL), deps.isGitLabHost(info.Host, cfg.GitLabBaseURL):
		return deps.findOrCreateTarget(ctx, cfg, info, branch, title)
	}
	return "", fmt.Errorf("unrecognised remote host %q; set github_base_url or gitlab_base_url in config", info.Host)
}

func plannedPublishTargetWithDeps(cfg *Config, deps commandDeps) (string, error) {
	branch, err := deps.getCurrentBranch()
	if err != nil {
		return "", fmt.Errorf("could not determine current branch: %w", err)
	}
	if branch == "" {
		return "", errors.New("cannot publish from detached HEAD without --pr")
	}
	remoteURL, err := deps.getRemoteURL()
	if err != nil {
		return "", fmt.Errorf("getting remote URL: %w", err)
	}
	if createURL := prCreateURL(remoteURL, branch); createURL != "" {
		return createURL, nil
	}
	info, err := deps.parseRemoteInfo(remoteURL)
	if err != nil {
		return "", err
	}
	switch {
	case deps.isGitHubHost(info.Host, cfg.GitHubBaseURL):
		if len(info.PathParts) < 2 {
			return "", fmt.Errorf("could not parse owner/repo from remote URL")
		}
		return "https://" + info.Host + "/" + strings.Join(info.PathParts[:2], "/") + "/compare/" + url.PathEscape(branch) + "?expand=1", nil
	case deps.isGitLabHost(info.Host, cfg.GitLabBaseURL):
		q := url.Values{}
		q.Set("merge_request[source_branch]", branch)
		return "https://" + info.Host + "/" + strings.Join(info.PathParts, "/") + "/-/merge_requests/new?" + q.Encode(), nil
	default:
		return "", fmt.Errorf("unrecognised remote host %q; set github_base_url or gitlab_base_url in config", info.Host)
	}
}

func getRemoteMetadata(ctx context.Context, cfg *Config, targetURL string) (prMetadata, error) {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return prMetadata{}, err
	}
	return target.Metadata(ctx, remoteCredentials(cfg))
}

func updateRemoteMetadata(ctx context.Context, cfg *Config, targetURL string, title, description *string) error {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return err
	}
	return target.UpdateMetadata(ctx, remoteCredentials(cfg), title, description)
}

func upsertRemoteManagedComment(ctx context.Context, cfg *Config, targetURL, body string) error {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return err
	}
	return target.UpsertManagedComment(ctx, remoteCredentials(cfg), managedCommentMarker, body)
}

func addRemoteLabels(ctx context.Context, cfg *Config, targetURL string, labels []string) error {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return err
	}
	return target.AddLabels(ctx, remoteCredentials(cfg), labels)
}

func requestRemoteReviewers(ctx context.Context, cfg *Config, targetURL string, reviewers []string) error {
	target, err := parseRemoteTarget(targetURL)
	if err != nil {
		return err
	}
	return target.RequestReviewers(ctx, remoteCredentials(cfg), reviewers)
}

func mergeManagedSection(existing, generated string) string {
	block := managedDescriptionStart + "\n" + strings.TrimSpace(generated) + "\n" + managedDescriptionEnd
	existing = strings.TrimSpace(existing)
	start := strings.Index(existing, managedDescriptionStart)
	end := strings.Index(existing, managedDescriptionEnd)
	if start >= 0 && end > start {
		end += len(managedDescriptionEnd)
		before := strings.TrimSpace(existing[:start])
		after := strings.TrimSpace(existing[end:])
		parts := []string{}
		if before != "" {
			parts = append(parts, before)
		}
		parts = append(parts, block)
		if after != "" {
			parts = append(parts, after)
		}
		return strings.Join(parts, "\n\n")
	}
	if existing == "" {
		return block
	}
	return existing + "\n\n" + block
}

func buildManagedComment(title, description string) string {
	var b strings.Builder
	b.WriteString(managedCommentMarker)
	b.WriteString("\n\n")
	if strings.TrimSpace(title) != "" {
		b.WriteString("## ")
		b.WriteString(strings.TrimSpace(title))
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(description))
	b.WriteByte('\n')
	return b.String()
}

func derivePublishLabels(summary diffSummary, description string) []string {
	labelSet := map[string]bool{}
	lowerDescription := strings.ToLower(description)
	if strings.Contains(lowerDescription, "security") || strings.Contains(lowerDescription, "vulnerab") || strings.Contains(lowerDescription, "credential") {
		labelSet["security"] = true
	}
	if strings.Contains(lowerDescription, "breaking") || strings.Contains(lowerDescription, "migration") {
		labelSet["breaking-change"] = true
	}
	for _, file := range summary.Files {
		path := strings.ToLower(file.Path)
		switch {
		case strings.Contains(path, "test") || strings.HasSuffix(path, "_test.go"):
			labelSet["tests"] = true
		case strings.HasPrefix(path, "docs/") || strings.HasSuffix(path, ".md"):
			labelSet["docs"] = true
		case strings.Contains(path, "docker") || strings.Contains(path, ".github/workflows") || strings.Contains(path, "ci"):
			labelSet["ci"] = true
		case strings.Contains(path, "go.mod") || strings.Contains(path, "go.sum") || strings.Contains(path, "package-lock") || strings.Contains(path, "requirements"):
			labelSet["dependencies"] = true
		}
	}
	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func publishLooksRisky(description string) bool {
	lower := strings.ToLower(description)
	for _, marker := range []string{"breaking", "data loss", "security", "vulnerab", "unsafe", "risk", "fail"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func ensureDraftTitle(title string) string {
	title = strings.TrimSpace(title)
	if strings.HasPrefix(strings.ToLower(title), "draft:") {
		return title
	}
	return "Draft: " + title
}

func writePublishDryRun(cmd *cobra.Command, cfg *Config, targetURL, title, description string, updateTitle, updateDescription, postSummary bool, labels, reviewers []string) error {
	if cmd.Flag("format").Value.String() == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			DryRun            bool     `json:"dry_run"`
			URL               string   `json:"url,omitempty"`
			Title             string   `json:"title"`
			DescriptionBytes  int      `json:"description_bytes"`
			WouldUpdateTitle  bool     `json:"would_update_title"`
			WouldUpdateBody   bool     `json:"would_update_description"`
			WouldPostSummary  bool     `json:"would_post_summary"`
			WouldApplyLabels  []string `json:"would_apply_labels,omitempty"`
			WouldAddReviewers []string `json:"would_add_reviewers,omitempty"`
			Provider          string   `json:"provider"`
			Model             string   `json:"model"`
		}{
			DryRun:            true,
			URL:               targetURL,
			Title:             title,
			DescriptionBytes:  len(description),
			WouldUpdateTitle:  updateTitle,
			WouldUpdateBody:   updateDescription,
			WouldPostSummary:  postSummary,
			WouldApplyLabels:  labels,
			WouldAddReviewers: reviewers,
			Provider:          string(cfg.Provider),
			Model:             getModelName(cfg),
		})
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dry run: no PR/MR metadata, comment, label, or reviewer changes will be written.")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Target: %s\n", targetURL)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Title: %s\n", title)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Description bytes: %d\n", len(description))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Update title: %v\n", updateTitle)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Update description: %v\n", updateDescription)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Post summary: %v\n", postSummary)
	if len(labels) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Labels: %s\n", strings.Join(labels, ", "))
	}
	if len(reviewers) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- Reviewers: %s\n", strings.Join(reviewers, ", "))
	}
	return nil
}
