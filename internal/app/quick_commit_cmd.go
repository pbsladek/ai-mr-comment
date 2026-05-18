package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newQuickCommitCmd returns a command that stages all changes, generates an
// AI commit message, commits, and pushes — all in one step.
func newQuickCommitCmdWithDeps(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), deps commandDeps) *cobra.Command {
	var provider, modelOverride, format, profileName, commitType, commitScope, messageTemplate string
	var dryRun, noPush, breaking, multiLine, longBody, emoji, noConventional, postFlag, verbose bool
	var editMessage, includeUntracked, trackedOnly, signoff bool
	var chaos, haiku, roast, fortune bool
	var qcMonday, qcJira, qcEmoji, qcSassy, qcTechnical bool
	var qcIntern, qcShakespeare, qcManager, qcYoda, qcExcuse bool
	var bodyLines int

	cmd := &cobra.Command{
		Use:   "quick-commit",
		Short: "Stage, AI-commit, and push in one step",
		Long: `Stages all changes (git add .), generates a conventional commit message
using AI, commits with that message, and pushes to the current branch's
remote. Use --dry-run to preview the generated message without committing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !deps.isGitRepo() {
				return fmt.Errorf("not a git repository")
			}

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
			if verbose {
				cfg.DebugWriter = cmd.ErrOrStderr()
			}
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}
			if bodyLines < 0 {
				return fmt.Errorf("--body-lines must be 0 or greater")
			}
			commitType = strings.ToLower(strings.TrimSpace(commitType))
			commitScope = strings.TrimSpace(commitScope)
			messageTemplate = strings.ToLower(strings.TrimSpace(messageTemplate))
			if commitType != "" && !isValidCommitType(commitType) {
				return fmt.Errorf("--type must be one of: %s", strings.Join(conventionalCommitTypes, ", "))
			}
			if messageTemplate != "" && !isValidQuickCommitMessageTemplate(messageTemplate) {
				return fmt.Errorf("--message-template must be one of: %s", strings.Join(quickCommitMessageTemplateNames, ", "))
			}
			if cmd.Flags().Changed("scope") {
				if err := validateCommitScope(commitScope); err != nil {
					return err
				}
			}
			if noConventional && (commitType != "" || commitScope != "") {
				return fmt.Errorf("--type and --scope cannot be combined with --no-conventional")
			}
			if breaking && commitType != "" && commitType != "feat" {
				return fmt.Errorf("--breaking can only be combined with --type=feat")
			}
			if includeUntracked && trackedOnly {
				return fmt.Errorf("--include-untracked and --tracked-only are mutually exclusive")
			}
			if longBody || bodyLines > 0 || quickCommitMessageTemplateImpliesBody(messageTemplate) {
				multiLine = true
			}
			if postFlag && dryRun {
				return errors.New("--post cannot be combined with --dry-run")
			}
			if postFlag && noPush {
				return errors.New("--post cannot be combined with --no-push")
			}

			branch, err := deps.getCurrentBranch()
			if err != nil {
				return fmt.Errorf("could not determine current branch: %w", err)
			}
			if branch == "" {
				return fmt.Errorf("cannot quick-commit in detached HEAD state")
			}

			out := cmd.OutOrStdout()

			// Stage everything (skipped in dry-run).
			if !dryRun {
				if trackedOnly {
					_, _ = fmt.Fprintln(out, "Staging tracked changes (git add -u)...")
				} else {
					_, _ = fmt.Fprintln(out, "Staging all changes (git add .)...")
				}
				if err := deps.stageQuickCommitChanges(trackedOnly); err != nil {
					return err
				}
			}

			// Get the diff to feed to the AI.
			// After a real git add the staged diff is the right source.
			// In dry-run mode nothing was staged, so use the full working-tree diff
			// (staged + unstaged) so the preview is still meaningful.
			var diffContent string
			if dryRun {
				if deps.getQuickDryRunDiff != nil {
					diffContent, err = deps.getQuickDryRunDiff(trackedOnly)
				} else {
					diffContent, err = deps.getGitDiff("", false, nil)
				}
			} else {
				diffContent, err = deps.getGitDiff("", true, nil)
			}
			if err != nil {
				return fmt.Errorf("reading diff: %w", err)
			}
			if strings.TrimSpace(diffContent) == "" && !chaos {
				return fmt.Errorf("no changes found to generate a commit message for")
			}

			// Validate mutually exclusive style flags.
			styleFlagNames := []string{"--chaos", "--haiku", "--roast", "--monday", "--jira", "--emoji-commit", "--sassy", "--technical", "--intern", "--shakespeare", "--manager", "--yoda", "--excuse"}
			styleFlags := []bool{chaos, haiku, roast, qcMonday, qcJira, qcEmoji, qcSassy, qcTechnical, qcIntern, qcShakespeare, qcManager, qcYoda, qcExcuse}
			styleCount := 0
			for _, f := range styleFlags {
				if f {
					styleCount++
				}
			}
			if styleCount > 1 {
				return fmt.Errorf("%s are mutually exclusive", strings.Join(styleFlagNames, ", "))
			}
			if chaos && (multiLine || noConventional) {
				return fmt.Errorf("--chaos cannot be combined with --multi-line or --no-conventional")
			}
			if haiku && (multiLine || noConventional) {
				return fmt.Errorf("--haiku cannot be combined with --multi-line or --no-conventional")
			}
			if roast && (multiLine || noConventional) {
				return fmt.Errorf("--roast cannot be combined with --multi-line or --no-conventional")
			}

			// Prepend branch name so the AI can reference the ticket key.
			diffContent = "Branch: " + branch + "\n\n" + diffContent
			diffContent = processDiff(diffContent, 4000)

			// --chaos ignores the real diff; just pass a fixed token.
			if chaos {
				diffContent = "chaos mode"
			}

			// Generate commit message via AI.
			var prompt string
			switch {
			case chaos:
				prompt = quickCommitChaosPrompt
			case haiku:
				prompt = quickCommitHaikuPrompt
			case roast:
				prompt = quickCommitRoastPrompt
			case qcMonday:
				prompt = quickCommitMondayPrompt
			case qcJira:
				prompt = quickCommitJiraPrompt
			case qcEmoji:
				prompt = quickCommitEmojiPrompt
			case qcSassy:
				prompt = quickCommitSassyPrompt
			case qcTechnical:
				prompt = quickCommitTechnicalPrompt
			case qcIntern:
				prompt = quickCommitInternPrompt
			case qcShakespeare:
				prompt = quickCommitShakespearePrompt
			case qcManager:
				prompt = quickCommitManagerPrompt
			case qcYoda:
				prompt = quickCommitYodaPrompt
			case qcExcuse:
				prompt = quickCommitExcusePrompt
			case multiLine:
				prompt = commitMsgBodyPrompt
				if longBody || bodyLines > 0 {
					prompt += longCommitBodyPromptSuffix(bodyLines)
				}
			case noConventional:
				prompt = quickCommitFreePrompt
			default:
				prompt = quickCommitPrompt
			}
			if breaking {
				prompt += "\n\nThis is a BREAKING CHANGE release. You MUST use the 'feat!' type (with an exclamation mark) to signal a breaking change, e.g. \"feat(scope)!: description\" or \"feat!: description\"."
				diffContent += "\n\nBREAKING CHANGE: this release introduces a breaking change and must use the feat! conventional commit type."
			}
			prompt = appendCommitGuidance(prompt, commitType, commitScope, messageTemplate)
			commitMessage, err := chatFn(cmd.Context(), cfg, cfg.Provider, prompt, diffContent)
			if err != nil {
				return fmt.Errorf("generating commit message: %w", err)
			}
			if multiLine {
				commitMessage = normalizeCommitBody(commitMessage)
			} else {
				commitMessage = normalizeCommitMessage(commitMessage)
			}
			if breaking {
				commitMessage = enforceBreakingChange(commitMessage)
				// Append a BREAKING CHANGE footer so semantic-release detects the
				// major bump even when the commit is squashed into a merge commit
				// (where the subject line is replaced by "Merge pull request #N...").
				commitMessage += "\n\nBREAKING CHANGE: breaking change"
			}
			commitMessage = applyCommitTypeScope(commitMessage, commitType, commitScope)
			if breaking {
				commitMessage = enforceBreakingChange(commitMessage)
			}
			if emoji {
				commitMessage = appendCommitEmoji(commitMessage)
			}
			if commitMessage == "" {
				return fmt.Errorf("AI returned an empty commit message")
			}

			// Generate a fortune trailer if requested.
			var fortuneBody string
			if fortune {
				rawFortune, fortuneErr := chatFn(cmd.Context(), cfg, cfg.Provider, fortunePrompt, "generate a fortune")
				if fortuneErr != nil {
					return fmt.Errorf("generating fortune: %w", fortuneErr)
				}
				fortuneBody = strings.TrimSpace(rawFortune)
			}

			jsonMsg := commitMessage
			if fortuneBody != "" {
				jsonMsg += "\n\n" + fortuneBody
			}
			if editMessage {
				edited, editErr := deps.editCommitMessage(jsonMsg)
				if editErr != nil {
					return editErr
				}
				jsonMsg = edited
				commitMessage = edited
				fortuneBody = ""
			}
			if signoff {
				identity, signoffErr := deps.getSignoffIdentity()
				if signoffErr != nil {
					return fmt.Errorf("--signoff: %w", signoffErr)
				}
				jsonMsg = appendSignedOffBy(jsonMsg, identity)
				commitMessage = jsonMsg
				fortuneBody = ""
			}
			if format == "json" {
				if err := json.NewEncoder(out).Encode(struct {
					CommitMessage string `json:"commit_message"`
					Provider      string `json:"provider"`
					Model         string `json:"model"`
				}{
					CommitMessage: jsonMsg,
					Provider:      string(cfg.Provider),
					Model:         getModelName(cfg),
				}); err != nil {
					return err
				}
			} else {
				_, _ = fmt.Fprintf(out, "%s\n\n", commitMessage)
				if fortuneBody != "" {
					_, _ = fmt.Fprintf(out, "%s\n\n", fortuneBody)
				}
			}

			if dryRun {
				if format != "json" {
					_, _ = fmt.Fprintln(out, "(dry-run: no changes committed)")
				}
				return nil
			}

			// Commit.
			if format != "json" {
				_, _ = fmt.Fprintln(out, "Committing...")
			}
			if err := deps.commitMessage(jsonMsg); err != nil {
				return err
			}

			if noPush {
				if format != "json" {
					_, _ = fmt.Fprintln(out, "Done. (skipped push)")
				}
				return nil
			}

			// Push.
			if format != "json" {
				_, _ = fmt.Fprintf(out, "Pushing to origin/%s...\n", branch)
			}
			if err := deps.push(branch); err != nil {
				return err
			}

			if format != "json" {
				_, _ = fmt.Fprintln(out, "Done.")
				if remoteURL, remErr := deps.getRemoteURL(); remErr == nil {
					if createURL := prCreateURLWithConfig(remoteURL, branch, cfg); createURL != "" {
						_, _ = fmt.Fprintf(out, "\nOpen PR/MR: %s\n", createURL)
					}
				}
			}

			if postFlag {
				remoteURL, remErr := deps.getRemoteURL()
				if remErr != nil {
					return fmt.Errorf("--post: getting remote URL: %w", remErr)
				}
				info, parseErr := deps.parseRemoteInfo(remoteURL)
				if parseErr != nil {
					return fmt.Errorf("--post: %w", parseErr)
				}

				// Derive PR/MR title from the commit message subject line.
				subject, _, _ := strings.Cut(commitMessage, "\n")

				// Find an existing open PR/MR or create a new one.
				if !deps.isGitHubHost(info.Host, cfg.GitHubBaseURL) && !deps.isGitLabHost(info.Host, cfg.GitLabBaseURL) {
					return fmt.Errorf("--post: unrecognised remote host %q; set github_base_url or gitlab_base_url in config", info.Host)
				}
				prMRURL, err := deps.findOrCreateTarget(cmd.Context(), cfg, info, branch, subject)
				if err != nil {
					return fmt.Errorf("--post: finding or creating PR/MR: %w", err)
				}
				_, _ = fmt.Fprintf(out, "PR/MR: %s\n", prMRURL)

				// Fetch the PR/MR diff from the remote API.
				_, _ = fmt.Fprintln(out, "Generating AI review comment...")
				diffForReview, err := deps.getRemoteDiff(cmd.Context(), cfg, prMRURL)
				if err != nil {
					return fmt.Errorf("--post: fetching PR/MR diff: %w", err)
				}

				// Generate the AI review using the default MR system prompt.
				reviewComment, reviewErr := chatFn(cmd.Context(), cfg, cfg.Provider, defaultPromptTemplate, diffForReview)
				if reviewErr != nil {
					return fmt.Errorf("--post: generating review comment: %w", reviewErr)
				}

				if err := deps.postRemoteComment(cmd.Context(), cfg, prMRURL, reviewComment); err != nil {
					return fmt.Errorf("--post: posting comment: %w", err)
				}
				_, _ = fmt.Fprintln(out, "Posted AI review comment to PR/MR.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to use (openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate and print the commit message without staging, committing, or pushing")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Commit but skip the push step")
	cmd.Flags().BoolVar(&editMessage, "edit", false, "Open the generated commit message in $GIT_EDITOR, $VISUAL, or $EDITOR before committing")
	cmd.Flags().BoolVar(&includeUntracked, "include-untracked", false, "Explicitly stage tracked and untracked changes (default behaviour)")
	cmd.Flags().BoolVar(&trackedOnly, "tracked-only", false, "Stage only tracked modified/deleted files with git add -u")
	cmd.Flags().BoolVar(&signoff, "signoff", false, "Append a Signed-off-by trailer using git user.name and user.email")
	cmd.Flags().StringVar(&commitType, "type", "", "Force a conventional commit type (feat, fix, docs, style, refactor, test, chore, perf, ci, build, revert)")
	cmd.Flags().StringVar(&commitScope, "scope", "", "Force a conventional commit scope")
	cmd.Flags().StringVar(&messageTemplate, "message-template", "", "Apply a commit message template style (short, detailed, release, ticket)")
	cmd.Flags().BoolVar(&postFlag, "post", false, "After pushing, find or create a PR/MR and post an AI review comment (requires GITHUB_TOKEN or GITLAB_TOKEN)")
	cmd.Flags().BoolVar(&breaking, "breaking", false, "Mark as a breaking change: forces feat! conventional commit type for a major version bump")
	cmd.Flags().BoolVar(&multiLine, "multi-line", false, "Generate a multi-line commit message (subject + body) that pre-fills the PR/MR title and description")
	cmd.Flags().BoolVar(&longBody, "long", false, "Generate a longer multi-section commit body (implies --multi-line)")
	cmd.Flags().IntVar(&bodyLines, "body-lines", 0, "Target body line count for long multi-line commits (implies --multi-line)")
	cmd.Flags().BoolVar(&emoji, "emoji", false, "Append a type-matched gitmoji to the commit subject (e.g. feat → ✨, fix → 🐛, breaking → 💥)")
	cmd.Flags().BoolVar(&noConventional, "no-conventional", false, "Disable conventional commits enforcement (use the AI output as-is)")
	cmd.Flags().BoolVar(&chaos, "chaos", false, "Generate a random funny/absurd conventional commit message (great for pipeline trigger commits)")
	cmd.Flags().BoolVar(&haiku, "haiku", false, "Generate the commit message description as a 5-7-5 haiku about the diff")
	cmd.Flags().BoolVar(&roast, "roast", false, "Generate a technically accurate but passive-aggressively judgmental commit message")
	cmd.Flags().BoolVar(&fortune, "fortune", false, "Append a developer-wisdom fortune-cookie quote as a commit message trailer")
	cmd.Flags().BoolVar(&qcMonday, "monday", false, "Generate a casual, low-energy Monday-morning style commit message")
	cmd.Flags().BoolVar(&qcJira, "jira", false, "Prefix commit message with Jira ticket key extracted from the branch name")
	cmd.Flags().BoolVar(&qcEmoji, "emoji-commit", false, "Append a type-matched gitmoji to the commit description")
	cmd.Flags().BoolVar(&qcSassy, "sassy", false, "Generate a sassy but technically accurate commit message")
	cmd.Flags().BoolVar(&qcTechnical, "technical", false, "Generate a commit message with maximum technical precision")
	cmd.Flags().BoolVar(&qcIntern, "intern", false, "Generate an overly enthusiastic junior-developer commit message")
	cmd.Flags().BoolVar(&qcShakespeare, "shakespeare", false, "Generate the commit description in Shakespearean Early Modern English")
	cmd.Flags().BoolVar(&qcManager, "manager", false, "Generate the commit description in passive-aggressive corporate non-speak")
	cmd.Flags().BoolVar(&qcYoda, "yoda", false, "Generate the commit description in Yoda's inverted syntax")
	cmd.Flags().BoolVar(&qcExcuse, "excuse", false, "Generate a technically accurate commit message with a built-in excuse")
	cmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate (defined in ~/.ai-mr-comment.toml under [profile.<name>])")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print debug info (provider, model, prompt size, timing) to stderr")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	_ = cmd.RegisterFlagCompletionFunc("type", completeValues(conventionalCommitTypes))
	_ = cmd.RegisterFlagCompletionFunc("message-template", completeValues(quickCommitMessageTemplateNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}
