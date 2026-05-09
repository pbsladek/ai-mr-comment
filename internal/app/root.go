package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// newRootCmd builds the root cobra command, wiring flags to the provided chatFn.
// Accepting chatFn as a parameter allows tests to inject a mock without real API calls.
func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider, modelOverride, templateName, format, prURL, clipboardFlag, systemPromptFlag, profileName, presetName string
	var inputFormat, streamMode string
	var debug, staged, smartChunk, generateTitle, generateCommitMsg, multiLine, verbose, exitCodeFlag, postFlag, estimate, autoYes, versionFlag, dryRun bool
	var updateTitleFlag, updateDescriptionFlag bool
	var changedFilesOnly, summaryOnly bool
	var quiet, plain, printPrompt, printRequest, verdictOnly, titleOnly bool
	var mrChaos, mrHaiku, mrRoast bool
	var mrIntern, mrShakespeare, mrManager, mrYoda, mrExcuse bool
	var exclude []string

	rootCmd := &cobra.Command{
		Use:           "ai-mr-comment",
		Short:         "Generate MR/PR comments using AI",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if versionFlag {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "version=%s\ncommit=%s\ncommit_full=%s\nrepo=https://github.com/pbsladek/ai-mr-comment\n", Version, Commit, CommitFull)
				return nil
			}
			runStart := time.Now()
			cfg, err := loadConfigForProfile(profileName)
			if err != nil {
				return err
			}
			presetPromptSuffix, err := applyRootPreset(cmd, presetName, cfg, &format, &exitCodeFlag, &plain, &generateTitle)
			if err != nil {
				return withExitCode(4, err)
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
			if verbose {
				cfg.DebugWriter = cmd.ErrOrStderr()
				defer func() {
					debugLog(cfg, "total elapsed: %dms", time.Since(runStart).Milliseconds())
				}()
			}
			configFile := cfg.ConfigFile
			if configFile == "" {
				configFile = "(none)"
			}
			debugLog(cfg, "config: file=%s provider=%s model=%s template=%s", configFile, cfg.Provider, getModelName(cfg), cfg.Template)

			if !isSupportedProvider(cfg.Provider) {
				return errors.New("unsupported provider: " + string(cfg.Provider))
			}
			if postFlag && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--post requires --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--update-title and --update-description require --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && generateCommitMsg {
				return withExitCode(4, errors.New("--update-title and --update-description cannot be used with --commit-msg"))
			}
			if updateDescriptionFlag && titleOnly {
				return withExitCode(4, errors.New("--update-description cannot be used with --title-only"))
			}
			metadataOnly := dryRun || changedFilesOnly || summaryOnly || debug || printPrompt || printRequest
			if !metadataOnly {
				if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
					return cfgErr
				}
			}
			if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
				defer cancel()
			}
			if quiet {
				format = "json"
			}
			if staged && commit != "" {
				return withExitCode(4, errors.New("--staged and --commit are mutually exclusive"))
			}
			if prURL != "" && (staged || commit != "" || diffFilePath != "") {
				return withExitCode(4, errors.New("--pr cannot be combined with --staged, --commit, or --file"))
			}
			if multiLine && !generateCommitMsg {
				return withExitCode(4, errors.New("--multi-line requires --commit-msg"))
			}
			effectiveTemplate := cfg.Template
			commitOnlyTemplates := map[string]bool{"commit": true, "commit-emoji": true, "commit-conventional": true}
			if commitOnlyTemplates[effectiveTemplate] && !generateCommitMsg {
				return fmt.Errorf("--template %s requires --commit-msg", effectiveTemplate)
			}
			mrOnlyTemplates := map[string]bool{
				"technical": true, "user-focused": true, "emoji": true, "sassy": true,
				"monday": true, "jira": true, "conventional": true,
				"chaos": true, "haiku": true, "roast": true, "intern": true,
				"shakespeare": true, "manager": true, "yoda": true, "excuse": true,
			}
			if mrOnlyTemplates[effectiveTemplate] && generateCommitMsg {
				return fmt.Errorf("--template %s cannot be combined with --commit-msg", effectiveTemplate)
			}
			if generateCommitMsg && generateTitle {
				return withExitCode(4, errors.New("--commit-msg and --title cannot be used together"))
			}
			if titleOnly && generateCommitMsg {
				return withExitCode(4, errors.New("--title-only cannot be used with --commit-msg"))
			}
			if exitCodeFlag && generateCommitMsg {
				return withExitCode(4, errors.New("--exit-code cannot be used with --commit-msg"))
			}
			if postFlag && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--post requires --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && prURL == "" && !dryRun {
				return withExitCode(4, errors.New("--update-title and --update-description require --pr to specify a GitHub PR or GitLab MR URL"))
			}
			if (updateTitleFlag || updateDescriptionFlag) && generateCommitMsg {
				return withExitCode(4, errors.New("--update-title and --update-description cannot be used with --commit-msg"))
			}
			if updateDescriptionFlag && titleOnly {
				return withExitCode(4, errors.New("--update-description cannot be used with --title-only"))
			}
			if cmd.Flags().Changed("system-prompt") && cmd.Flags().Changed("template") {
				return withExitCode(4, errors.New("--system-prompt and --template are mutually exclusive"))
			}
			mrStyleFlags := []bool{mrChaos, mrHaiku, mrRoast, mrIntern, mrShakespeare, mrManager, mrYoda, mrExcuse}
			funStyleCount := 0
			for _, f := range mrStyleFlags {
				if f {
					funStyleCount++
				}
			}
			if funStyleCount > 1 {
				return withExitCode(4, errors.New("--chaos, --haiku, --roast, --intern, --shakespeare, --manager, --yoda, and --excuse are mutually exclusive"))
			}
			if funStyleCount > 0 && (cmd.Flags().Changed("template") || cmd.Flags().Changed("system-prompt")) {
				return withExitCode(4, errors.New("style flags cannot be combined with --template or --system-prompt"))
			}
			if funStyleCount > 0 && generateCommitMsg {
				return withExitCode(4, errors.New("style flags cannot be combined with --commit-msg"))
			}

			if format != "text" && format != "json" {
				return withExitCode(4, fmt.Errorf("unsupported format %q: must be text or json", format))
			}
			if inputFormat == "" {
				inputFormat = "text"
			}
			if inputFormat != "text" && inputFormat != "json" {
				return withExitCode(4, fmt.Errorf("unsupported input format %q: must be text or json", inputFormat))
			}
			if streamMode != "" && streamMode != "jsonl" {
				return withExitCode(4, fmt.Errorf("unsupported stream mode %q: must be jsonl", streamMode))
			}
			if streamMode == "jsonl" && outputPath != "" {
				return withExitCode(4, errors.New("--stream=jsonl cannot be combined with --output"))
			}
			if dryRun && streamMode == "jsonl" {
				return withExitCode(4, errors.New("--dry-run cannot be combined with --stream=jsonl"))
			}
			if dryRun && (debug || estimate || printPrompt || printRequest || changedFilesOnly || summaryOnly) {
				return withExitCode(4, errors.New("--dry-run cannot be combined with --debug, --estimate, --print-prompt, --print-request, --changed-files, or --summary-only"))
			}
			if (changedFilesOnly || summaryOnly) && (postFlag || clipboardFlag != "") {
				return withExitCode(4, errors.New("--changed-files and --summary-only cannot be combined with --post or --clipboard"))
			}

			var diffContent string
			var diffSource string
			diffFetchStart := time.Now()
			err = nil
			if inputFormat == "json" {
				if prURL != "" || staged || commit != "" {
					return withExitCode(4, errors.New("--input=json cannot be combined with --pr, --staged, or --commit"))
				}
				diffSource = "json"
				var rawInput string
				rawInput, err = readCommandInput(cmd, diffFilePath)
				if err == nil {
					diffContent, err = decodeAgentInput(rawInput)
				}
			} else if prURL != "" {
				switch {
				case isGitHubURL(prURL):
					diffSource = "github-pr: " + prURL
					diffContent, err = getPRDiff(cmd.Context(), prURL, cfg.GitHubToken, cfg.GitHubBaseURL)
				case isGitLabURL(prURL):
					diffSource = "gitlab-mr: " + prURL
					diffContent, err = getMRDiff(cmd.Context(), prURL, cfg.GitLabToken, cfg.GitLabBaseURL)
				default:
					return fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", prURL)
				}
			} else if diffFilePath != "" {
				if diffFilePath == "-" {
					diffSource = "stdin"
				} else {
					diffSource = "file: " + diffFilePath
				}
				diffContent, err = readCommandInput(cmd, diffFilePath)
			} else if commandStdinIsPiped(cmd) {
				diffSource = "stdin"
				diffContent, err = readCommandInput(cmd, "-")
			} else {
				if !isGitRepo() {
					return fmt.Errorf("not a git repository. Run from inside a git repo or use --file to provide a diff")
				}
				switch {
				case staged:
					diffSource = "git (staged)"
				case commit != "":
					diffSource = "git (commit: " + commit + ")"
				default:
					diffSource = "git"
				}
				diffContent, err = getGitDiff(commit, staged, exclude)
			}
			debugLog(cfg, "diff fetch: elapsed=%dms", time.Since(diffFetchStart).Milliseconds())
			if err != nil {
				return err
			}
			if strings.TrimSpace(diffContent) == "" {
				if staged {
					return withExitCode(3, fmt.Errorf("no staged changes found. Stage your changes with 'git add' first"))
				}
				return withExitCode(3, fmt.Errorf("no diff found. Make sure you have uncommitted changes or specify a commit range with --commit"))
			}
			debugLog(cfg, "diff: source=%s bytes=%d", diffSource, len(diffContent))

			// Prepend the current branch name when diffing a local git repo.
			// This lets the AI and templates reference the branch/ticket number
			// (e.g. "feat/ABC-123-add-login") for linking in systems like Jira.
			// Skipped for --file and --pr since those have no local branch context.
			if prURL == "" && diffFilePath == "" && diffSource != "stdin" && diffSource != "json" {
				if branch, branchErr := getCurrentBranch(); branchErr == nil && branch != "" {
					diffContent = "Branch: " + branch + "\n\n" + diffContent
					debugLog(cfg, "branch: name=%s", branch)
				}
			}

			out := cmd.OutOrStdout()
			// When writing to a file, suppress all text output to the terminal.
			if outputPath != "" && !dryRun && !changedFilesOnly && !summaryOnly {
				out = io.Discard
			}
			// Summarize the raw diff before truncating so metadata-only modes do
			// not hide files that appear after the generation line limit.
			diffLines := strings.Split(diffContent, "\n")
			rawLines := len(diffLines)
			diffTruncated := rawLines > 4000
			summary := summarizeDiff(diffContent, diffSource, getModelName(cfg), diffTruncated)
			diffContent = truncateDiff(diffLines, 4000)
			debugLog(cfg, "diff: lines before truncation=%d after=%d (max=4000)", rawLines, strings.Count(diffContent, "\n")+1)

			if changedFilesOnly || summaryOnly {
				if outputPath != "" {
					return writeDiffSummaryToFile(outputPath, summary, format, changedFilesOnly && !summaryOnly)
				}
				return writeDiffSummary(cmd, summary, format, changedFilesOnly && !summaryOnly)
			}

			systemPrompt, templateErr := NewPromptTemplate(cfg.Template)
			if templateErr != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", templateErr)
			}
			templateSource := "embedded"
			if cfg.Template != "default" {
				if templateErr == nil {
					templateSource = "filesystem"
				} else {
					templateSource = "embedded (fallback)"
				}
			}
			debugLog(cfg, "template: name=%q source=%s length=%d", cfg.Template, templateSource, len(systemPrompt))

			// --system-prompt overrides the template-derived prompt entirely.
			if systemPromptFlag != "" {
				override, spErr := resolveSystemPrompt(systemPromptFlag)
				if spErr != nil {
					return spErr
				}
				systemPrompt = override
				debugLog(cfg, "system-prompt: override applied length=%d", len(systemPrompt))
			}

			// Fun style flags override the system prompt with a personality template.
			switch {
			case mrChaos:
				systemPrompt = mrChaosPrompt
				debugLog(cfg, "style: chaos mode enabled")
			case mrHaiku:
				systemPrompt = mrHaikuPrompt
				debugLog(cfg, "style: haiku mode enabled")
			case mrRoast:
				systemPrompt = mrRoastPrompt
				debugLog(cfg, "style: roast mode enabled")
			case mrIntern:
				systemPrompt = mrInternPrompt
				debugLog(cfg, "style: intern mode enabled")
			case mrShakespeare:
				systemPrompt = mrShakespearePrompt
				debugLog(cfg, "style: shakespeare mode enabled")
			case mrManager:
				systemPrompt = mrManagerPrompt
				debugLog(cfg, "style: manager mode enabled")
			case mrYoda:
				systemPrompt = mrYodaPrompt
				debugLog(cfg, "style: yoda mode enabled")
			case mrExcuse:
				systemPrompt = mrExcusePrompt
				debugLog(cfg, "style: excuse mode enabled")
			}
			if presetPromptSuffix != "" && systemPromptFlag == "" {
				systemPrompt += presetPromptSuffix
			}

			// When --exit-code is set, prepend a verdict instruction so the AI starts
			// its response with "VERDICT: PASS" or "VERDICT: FAIL".
			const exitCodePreamble = "Before your review, output a verdict on the very first line in exactly this format:\nVERDICT: PASS\nor\nVERDICT: FAIL\nUse FAIL if the diff contains critical bugs, security vulnerabilities, data loss risks, or broken public APIs. Use PASS for everything else. Then continue with your normal review on the next line.\n\n"
			if exitCodeFlag {
				systemPrompt = exitCodePreamble + systemPrompt
			}

			if printPrompt {
				_, _ = fmt.Fprint(out, systemPrompt)
				if !strings.HasSuffix(systemPrompt, "\n") {
					_, _ = fmt.Fprintln(out)
				}
				return nil
			}

			if printRequest {
				request := struct {
					Provider     string `json:"provider"`
					Model        string `json:"model"`
					SystemPrompt string `json:"system_prompt"`
					Diff         string `json:"diff"`
					DiffSource   string `json:"diff_source"`
					Template     string `json:"template"`
					Preset       string `json:"preset,omitempty"`
					Truncated    bool   `json:"truncated"`
				}{
					Provider:     string(cfg.Provider),
					Model:        getModelName(cfg),
					SystemPrompt: systemPrompt,
					Diff:         diffContent,
					DiffSource:   diffSource,
					Template:     cfg.Template,
					Preset:       presetName,
					Truncated:    diffTruncated,
				}
				return json.NewEncoder(out).Encode(request)
			}

			if dryRun {
				plan := struct {
					DryRun              bool        `json:"dry_run"`
					Provider            string      `json:"provider"`
					Model               string      `json:"model"`
					Template            string      `json:"template"`
					Preset              string      `json:"preset,omitempty"`
					DiffSource          string      `json:"diff_source"`
					Summary             diffSummary `json:"summary"`
					WouldCallProvider   bool        `json:"would_call_provider"`
					WouldWriteOutput    bool        `json:"would_write_output"`
					WouldCopyClipboard  bool        `json:"would_copy_clipboard"`
					WouldPostComment    bool        `json:"would_post_comment"`
					WouldUpdateTitle    bool        `json:"would_update_title"`
					WouldUpdateBody     bool        `json:"would_update_description"`
					MissingPostTarget   bool        `json:"missing_post_target,omitempty"`
					MissingUpdateTarget bool        `json:"missing_update_target,omitempty"`
					PostTarget          string      `json:"post_target,omitempty"`
				}{
					DryRun:              true,
					Provider:            string(cfg.Provider),
					Model:               getModelName(cfg),
					Template:            cfg.Template,
					Preset:              presetName,
					DiffSource:          diffSource,
					Summary:             summary,
					WouldCallProvider:   (!postFlag && !updateTitleFlag && !updateDescriptionFlag) || prURL != "",
					WouldWriteOutput:    outputPath != "",
					WouldCopyClipboard:  clipboardFlag != "",
					WouldPostComment:    postFlag,
					WouldUpdateTitle:    updateTitleFlag,
					WouldUpdateBody:     updateDescriptionFlag,
					MissingPostTarget:   postFlag && prURL == "",
					MissingUpdateTarget: (updateTitleFlag || updateDescriptionFlag) && prURL == "",
					PostTarget:          prURL,
				}
				if format == "json" {
					return json.NewEncoder(out).Encode(plan)
				}
				_, _ = fmt.Fprintln(out, "Dry run: no provider call, file write, clipboard write, PR/MR post, or PR/MR metadata update will be performed.")
				_, _ = fmt.Fprintf(out, "- Provider: %s\n", plan.Provider)
				_, _ = fmt.Fprintf(out, "- Model: %s\n", plan.Model)
				_, _ = fmt.Fprintf(out, "- Template: %s\n", plan.Template)
				if presetName != "" {
					_, _ = fmt.Fprintf(out, "- Preset: %s\n", presetName)
				}
				_, _ = fmt.Fprintf(out, "- Diff source: %s\n", diffSource)
				_, _ = fmt.Fprintf(out, "- Files: %d\n", summary.FileCount)
				_, _ = fmt.Fprintf(out, "- Additions: %d\n", summary.Additions)
				_, _ = fmt.Fprintf(out, "- Deletions: %d\n", summary.Deletions)
				if outputPath != "" {
					_, _ = fmt.Fprintf(out, "- Would write output: %s\n", outputPath)
				}
				if clipboardFlag != "" {
					_, _ = fmt.Fprintf(out, "- Would copy clipboard: %s\n", clipboardFlag)
				}
				if postFlag {
					if prURL == "" {
						_, _ = fmt.Fprintln(out, "- Would post comment: (missing --pr)")
					} else {
						_, _ = fmt.Fprintf(out, "- Would post comment: %s\n", prURL)
					}
				}
				if updateTitleFlag || updateDescriptionFlag {
					if prURL == "" {
						_, _ = fmt.Fprintln(out, "- Would update PR/MR metadata: (missing --pr)")
					} else {
						fields := []string{}
						if updateTitleFlag {
							fields = append(fields, "title")
						}
						if updateDescriptionFlag {
							fields = append(fields, "description")
						}
						_, _ = fmt.Fprintf(out, "- Would update PR/MR %s: %s\n", strings.Join(fields, "+"), prURL)
					}
				}
				return nil
			}

			if debug {
				showCostEstimate(cmd.Context(), cfg, systemPrompt, diffContent, out)
				return nil
			}

			if estimate {
				estimateOut := out
				if format == "json" {
					estimateOut = cmd.ErrOrStderr()
				}
				showCostEstimate(cmd.Context(), cfg, systemPrompt, diffContent, estimateOut)
				if !promptConfirm(cmd.ErrOrStderr(), os.Stdin, autoYes) {
					return nil
				}
			}

			// Stream tokens directly to the terminal when output is a real TTY,
			// text format is selected, smart-chunk is off, and no output file is set.
			// All other paths use the buffered chatFn to get the full response first.
			isTTY := fileIsTerminal(os.Stdout)
			shouldStream := isTTY && format == "text" && streamMode == "" && !smartChunk && outputPath == ""
			debugLog(cfg, "streaming: tty=%v format=%s smart-chunk=%v output-file=%q → enabled=%v",
				isTTY, format, smartChunk, outputPath, shouldStream)
			// streamedOK is set to true only when streaming completes successfully.
			// The output block uses it to decide whether body was already written.
			var streamedOK bool

			var comment string
			var title string
			var commitMessage string
			if titleOnly {
				title, err = timedCall(cfg, "title", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, titlePrompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				title = strings.TrimSpace(title)
			} else if generateCommitMsg {
				// Skip description generation; produce only a commit message.
				debugLog(cfg, "commit-msg: generating commit message with separate API call (multi-line=%v)", multiLine)
				prompt := commitMsgPrompt
				if multiLine {
					prompt = commitMsgBodyPrompt
				} else if commitOnlyTemplates[effectiveTemplate] {
					prompt = systemPrompt
				}
				commitMessage, err = timedCall(cfg, "commit-msg", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, prompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				if multiLine {
					commitMessage = normalizeCommitBody(commitMessage)
				} else {
					commitMessage = normalizeCommitMessage(commitMessage)
				}
			} else if smartChunk {
				chunks := splitDiffByFile(diffContent)
				debugLog(cfg, "smart-chunk: files=%d", len(chunks))
				if len(chunks) > 1 {
					// Summarize each file chunk independently in parallel, then do a final synthesis call.
					const chunkPrompt = "Summarize the changes in this file diff in 3-5 bullet points. Be concise and technical."
					summaries := make([]string, len(chunks))
					debugLog(cfg, "smart-chunk: summarizing %d chunks in parallel", len(chunks))
					eg, egCtx := errgroup.WithContext(cmd.Context())
					for i, chunk := range chunks {
						i, chunk := i, chunk // capture loop vars
						eg.Go(func() error {
							debugLog(cfg, "smart-chunk: processing chunk %d/%d", i+1, len(chunks))
							summary, chunkErr := timedCall(cfg, fmt.Sprintf("chunk-summary-%d", i+1), func() (string, error) {
								return chatFn(egCtx, cfg, cfg.Provider, chunkPrompt, processDiff(chunk, 1000))
							})
							if chunkErr != nil {
								return chunkErr
							}
							summaries[i] = summary
							return nil
						})
					}
					if chunkErr := eg.Wait(); chunkErr != nil {
						if cfg.Provider == Ollama && strings.Contains(chunkErr.Error(), "connection refused") {
							return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
						}
						return chunkErr
					}
					debugLog(cfg, "smart-chunk: all chunks summarized, running synthesis call")
					combinedSummaries := strings.Join(summaries, "\n\n---\n\n")
					comment, err = timedCall(cfg, "synthesis", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, combinedSummaries)
					})
				} else {
					comment, err = timedCall(cfg, "comment", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
					})
				}
			} else if shouldStream {
				comment, err = timedCall(cfg, "comment (stream)", func() (string, error) {
					return streamToWriter(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent, out)
				})
				if err != nil {
					// Streaming failed; fall back to the buffered call.
					// headerPrinted=true tells the output block to skip reprinting the separator.
					comment, err = timedCall(cfg, "comment (fallback)", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
					})
				} else {
					streamedOK = true
				}
			} else {
				// When a title is also needed, run comment and title concurrently to
				// save one full LLM round-trip of wall-clock time.
				needsTitle := (generateTitle || updateTitleFlag || format == "json") && !generateCommitMsg
				if needsTitle {
					debugLog(cfg, "title+comment: running in parallel")
					var parallelComment, parallelTitle string
					eg, egCtx := errgroup.WithContext(cmd.Context())
					eg.Go(func() error {
						var callErr error
						parallelComment, callErr = timedCall(cfg, "comment (parallel)", func() (string, error) {
							return chatFn(egCtx, cfg, cfg.Provider, systemPrompt, diffContent)
						})
						return callErr
					})
					eg.Go(func() error {
						var callErr error
						parallelTitle, callErr = timedCall(cfg, "title (parallel)", func() (string, error) {
							return chatFn(egCtx, cfg, cfg.Provider, titlePrompt, diffContent)
						})
						return callErr
					})
					err = eg.Wait()
					comment = parallelComment
					title = strings.TrimSpace(parallelTitle)
				} else {
					comment, err = timedCall(cfg, "comment", func() (string, error) {
						return chatFn(cmd.Context(), cfg, cfg.Provider, systemPrompt, diffContent)
					})
				}
			}
			if !generateCommitMsg && !titleOnly && err != nil {
				if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
					return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
				}
				return err
			}

			// Generate a title when explicitly requested (--title) or when producing
			// JSON output for pipeline consumers (--format=json implies title).
			// Skip title generation entirely when --commit-msg is active.
			// NOTE: title may already be set above by the parallel path; this block
			// only runs for the streaming case where the comment was written token-by-token.
			if (generateTitle || updateTitleFlag || format == "json") && !generateCommitMsg && !titleOnly && title == "" {
				debugLog(cfg, "title: generating title after stream")
				title, err = timedCall(cfg, "title", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, titlePrompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				title = strings.TrimSpace(title)
			}

			// Parse and strip the VERDICT line when --exit-code is active.
			var verdict string
			if exitCodeFlag {
				verdict, comment = parseVerdict(comment)
				if verdict != "PASS" && verdict != "FAIL" {
					verdict = "FAIL"
				}
				debugLog(cfg, "exit-code: verdict=%s", verdict)
			}

			dest := "stdout"
			if outputPath != "" {
				dest = "file: " + outputPath
			} else if clipboardFlag != "" {
				dest = "stdout+clipboard:" + clipboardFlag
			}
			debugLog(cfg, "output: format=%s destination=%s", format, dest)

			// outputJSON is the structured response emitted when --format=json is set.
			// For --commit-msg: only commit_message, provider, model are populated.
			// For normal description: title and description are the primary fields;
			// comment mirrors description for backwards compatibility.
			// Hoisted to outer scope so --output file can reference it when format=json.
			type outputJSON struct {
				Title         string `json:"title,omitempty"`
				Description   string `json:"description,omitempty"`
				Comment       string `json:"comment,omitempty"`
				CommitMessage string `json:"commit_message,omitempty"`
				Verdict       string `json:"verdict,omitempty"`
				Provider      string `json:"provider"`
				Model         string `json:"model"`
				DiffSource    string `json:"diff_source,omitempty"`
				Truncated     bool   `json:"truncated,omitempty"`
			}
			var payload outputJSON
			if titleOnly {
				payload = outputJSON{
					Title:      title,
					Provider:   string(cfg.Provider),
					Model:      getModelName(cfg),
					DiffSource: diffSource,
					Truncated:  diffTruncated,
				}
			} else if generateCommitMsg {
				payload = outputJSON{
					CommitMessage: commitMessage,
					Provider:      string(cfg.Provider),
					Model:         getModelName(cfg),
					DiffSource:    diffSource,
					Truncated:     diffTruncated,
				}
			} else {
				payload = outputJSON{
					Title:       title,
					Description: comment,
					Comment:     comment,
					Verdict:     verdict,
					Provider:    string(cfg.Provider),
					Model:       getModelName(cfg),
					DiffSource:  diffSource,
					Truncated:   diffTruncated,
				}
			}

			if verdictOnly {
				if format == "json" {
					if err := json.NewEncoder(out).Encode(struct {
						Verdict    string `json:"verdict"`
						Provider   string `json:"provider"`
						Model      string `json:"model"`
						DiffSource string `json:"diff_source,omitempty"`
						Truncated  bool   `json:"truncated,omitempty"`
					}{
						Verdict:    verdict,
						Provider:   string(cfg.Provider),
						Model:      getModelName(cfg),
						DiffSource: diffSource,
						Truncated:  diffTruncated,
					}); err != nil {
						return err
					}
				} else {
					_, _ = fmt.Fprintln(out, verdict)
				}
			} else if streamMode == "jsonl" {
				if err := encodeJSONLine(out, "start", map[string]any{
					"provider":    string(cfg.Provider),
					"model":       getModelName(cfg),
					"diff_source": diffSource,
					"truncated":   diffTruncated,
				}); err != nil {
					return err
				}
				if titleOnly {
					if err := encodeJSONLine(out, "token", map[string]any{"text": title}); err != nil {
						return err
					}
				} else if generateCommitMsg {
					if err := encodeJSONLine(out, "token", map[string]any{"text": commitMessage}); err != nil {
						return err
					}
				} else {
					if err := encodeJSONLine(out, "token", map[string]any{"text": comment}); err != nil {
						return err
					}
				}
				if err := encodeJSONLine(out, "done", map[string]any{"result": payload}); err != nil {
					return err
				}
			} else if format == "json" {
				if err := json.NewEncoder(out).Encode(payload); err != nil {
					return err
				}
			} else if titleOnly {
				_, _ = fmt.Fprintln(out, title)
			} else if generateCommitMsg {
				// --commit-msg text output: just the message, no headers, clean for shell piping.
				_, _ = fmt.Fprintln(out, commitMessage)
			} else if plain {
				if title != "" {
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
				_, _ = fmt.Fprintln(out, comment)
			} else if streamedOK {
				// Streaming succeeded: body was already written token-by-token.
				_, _ = fmt.Fprintln(out)
				if title != "" {
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, "── Title ────────────────────────────────")
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
			} else {
				if title != "" {
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, "── Title ────────────────────────────────")
					_, _ = fmt.Fprintln(out)
					_, _ = fmt.Fprintln(out, title)
					_, _ = fmt.Fprintln(out)
				}
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, "── Description ──────────────────────────")
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, comment)
				_, _ = fmt.Fprintln(out)
			}

			if clipboardFlag != "" {
				var clipContent string
				switch clipboardFlag {
				case "title":
					clipContent = title
				case "description", "comment":
					clipContent = comment
				case "commit-msg":
					clipContent = commitMessage
				case "all":
					if title != "" {
						clipContent = title + "\n\n" + comment
					} else {
						clipContent = comment
					}
				default:
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: unknown --clipboard value %q (use title, description, commit-msg, or all)\n", clipboardFlag)
				}
				if clipContent != "" {
					if err := clipboard.WriteAll(clipContent); err != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not copy to clipboard: %v\n", err)
					}
				}
			}

			if outputPath != "" {
				var fileContent []byte
				if format == "json" {
					var buf bytes.Buffer
					if encErr := json.NewEncoder(&buf).Encode(payload); encErr != nil {
						return encErr
					}
					fileContent = buf.Bytes()
				} else if titleOnly {
					fileContent = []byte(title + "\n")
				} else if generateCommitMsg {
					fileContent = []byte(commitMessage + "\n")
				} else {
					fileContent = []byte(comment)
				}
				if err := os.WriteFile(outputPath, fileContent, 0600); err != nil {
					return err
				}
			}

			if updateTitleFlag || updateDescriptionFlag {
				var updateTitle *string
				var updateDescription *string
				if updateTitleFlag {
					updateTitle = &title
				}
				if updateDescriptionFlag {
					metadata, metaErr := getRemoteMetadata(cmd.Context(), cfg, prURL)
					if metaErr != nil {
						return metaErr
					}
					body := mergeManagedSection(metadata.Description, comment)
					updateDescription = &body
				}
				switch {
				case isGitHubURL(prURL):
					if err := updateGitHubPRMetadata(cmd.Context(), prURL, cfg.GitHubToken, cfg.GitHubBaseURL, updateTitle, updateDescription); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Updated GitHub PR metadata.")
				case isGitLabURL(prURL):
					if err := updateGitLabMRMetadata(cmd.Context(), prURL, cfg.GitLabToken, cfg.GitLabBaseURL, updateTitle, updateDescription); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Updated GitLab MR metadata.")
				default:
					return fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", prURL)
				}
			}

			// --post: publish the generated comment back to the GitHub PR or GitLab MR.
			if postFlag {
				postBody := comment
				if title != "" {
					postBody = "**" + title + "**\n\n" + comment
				}
				switch {
				case isGitHubURL(prURL):
					if err := postGitHubPRComment(cmd.Context(), prURL, cfg.GitHubToken, cfg.GitHubBaseURL, postBody); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Posted comment to GitHub PR.")
				case isGitLabURL(prURL):
					if err := postGitLabMRNote(cmd.Context(), prURL, cfg.GitLabToken, cfg.GitLabBaseURL, postBody); err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Posted note to GitLab MR.")
				}
			}

			// --exit-code: non-zero exit when AI verdict is FAIL.
			if exitCodeFlag && verdict == "FAIL" {
				return exitCodeError(2)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&diffFilePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&prURL, "pr", "", "GitHub PR or GitLab MR URL (e.g. https://github.com/owner/repo/pull/123 or https://gitlab.com/group/project/-/merge_requests/42)")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai, anthropic, gemini, ollama)")
	rootCmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run (e.g. gpt-5.5, claude-opus-4-6, gemini-2.5-flash)")
	rootCmd.Flags().StringVarP(&templateName, "template", "t", "default", "Prompt template to use (e.g., default, conventional, technical)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Show token/cost estimate and exit without calling the API")
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "Enable verbose debug logging to stderr (provider, model, timing, errors)")
	rootCmd.Flags().BoolVar(&staged, "staged", false, "Diff staged changes only (git diff --cached)")
	rootCmd.Flags().StringVar(&clipboardFlag, "clipboard", "", "Copy to clipboard: title, description, or all")
	rootCmd.Flags().StringArrayVar(&exclude, "exclude", nil, "Exclude files matching pattern (e.g. vendor/**, *.sum). Can be repeated.")
	rootCmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	rootCmd.Flags().StringVar(&inputFormat, "input", "text", "Input format: text or json")
	rootCmd.Flags().StringVar(&streamMode, "stream", "", "Structured stream mode: jsonl")
	rootCmd.Flags().StringVar(&presetName, "preset", "", "Preset defaults: ci, local-fast, security, release-notes")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without calling the provider or writing side effects")
	rootCmd.Flags().BoolVar(&changedFilesOnly, "changed-files", false, "Print changed file paths and exit without calling the provider")
	rootCmd.Flags().BoolVar(&summaryOnly, "summary-only", false, "Print diff stats and changed files and exit without calling the provider")
	rootCmd.Flags().BoolVar(&quiet, "quiet", false, "Machine mode: emit JSON on stdout and route diagnostics to stderr")
	rootCmd.Flags().BoolVar(&plain, "plain", false, "Suppress text section headers and decorations")
	rootCmd.Flags().BoolVar(&plain, "no-decorate", false, "Alias for --plain")
	rootCmd.Flags().BoolVar(&printPrompt, "print-prompt", false, "Print the resolved system prompt and exit without calling the provider")
	rootCmd.Flags().BoolVar(&printRequest, "print-request", false, "Print the resolved provider request as JSON and exit without calling the provider")
	rootCmd.Flags().BoolVar(&verdictOnly, "verdict-only", false, "Print only the parsed verdict when used with --exit-code")
	_ = rootCmd.Flags().MarkHidden("verdict-only")
	rootCmd.Flags().BoolVar(&titleOnly, "title-only", false, "Print only a generated title")
	_ = rootCmd.Flags().MarkHidden("title-only")
	rootCmd.Flags().BoolVar(&smartChunk, "smart-chunk", false, "Split large diffs by file, summarize each, then combine")
	rootCmd.Flags().BoolVar(&generateTitle, "title", false, "Generate a concise MR/PR title in addition to the comment")
	rootCmd.Flags().BoolVar(&generateCommitMsg, "commit-msg", false, "Generate a git commit message instead of a full MR/PR description")
	rootCmd.Flags().BoolVar(&multiLine, "multi-line", false, "Generate a multi-line commit message (subject + body) when used with --commit-msg; body pre-fills the PR/MR description")
	rootCmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false, "Exit with code 2 if the AI detects critical issues in the diff")
	rootCmd.Flags().BoolVar(&postFlag, "post", false, "Post the generated comment back to the GitHub PR or GitLab MR (requires --pr)")
	rootCmd.Flags().BoolVar(&updateTitleFlag, "update-title", false, "Update the GitHub PR title or GitLab MR title with the generated title (requires --pr)")
	rootCmd.Flags().BoolVar(&updateDescriptionFlag, "update-description", false, "Update the GitHub PR body or GitLab MR description with the generated description (requires --pr)")
	rootCmd.Flags().StringVar(&systemPromptFlag, "system-prompt", "", `Override the system prompt for this run. Use @path to read from a file (e.g. --system-prompt=@review.txt). Mutually exclusive with --template.`)
	rootCmd.Flags().BoolVar(&estimate, "estimate", false, "Show token/cost estimate and prompt for confirmation before calling the API")
	rootCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Auto-confirm the cost estimate prompt (use with --estimate)")
	rootCmd.Flags().BoolVar(&versionFlag, "version", false, "Print version and exit")
	rootCmd.Flags().StringVar(&profileName, "profile", "", "Named config profile to activate (defined in ~/.ai-mr-comment.toml under [profile.<name>])")
	rootCmd.Flags().BoolVar(&mrChaos, "chaos", false, "Generate a chaotic, dramatically over-the-top MR/PR description (still technically accurate)")
	rootCmd.Flags().BoolVar(&mrHaiku, "haiku", false, "Generate the entire MR/PR description as a sequence of haikus")
	rootCmd.Flags().BoolVar(&mrRoast, "roast", false, "Generate a technically accurate but sardonically judgmental MR/PR description")
	rootCmd.Flags().BoolVar(&mrIntern, "intern", false, "Generate an overly enthusiastic junior-developer MR/PR description")
	rootCmd.Flags().BoolVar(&mrShakespeare, "shakespeare", false, "Generate the MR/PR description in Shakespearean Early Modern English")
	rootCmd.Flags().BoolVar(&mrManager, "manager", false, "Generate the MR/PR description in passive-aggressive corporate non-speak")
	rootCmd.Flags().BoolVar(&mrYoda, "yoda", false, "Generate the MR/PR description in Yoda's inverted syntax")
	rootCmd.Flags().BoolVar(&mrExcuse, "excuse", false, "Generate a technically accurate MR/PR description with built-in excuses")
	_ = rootCmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = rootCmd.RegisterFlagCompletionFunc("template", completeValues(templateNames))
	_ = rootCmd.RegisterFlagCompletionFunc("preset", completeValues(presetNames))
	_ = rootCmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	_ = rootCmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	_ = rootCmd.RegisterFlagCompletionFunc("input", completeValues([]string{"text", "json"}))
	_ = rootCmd.RegisterFlagCompletionFunc("stream", completeValues([]string{"jsonl"}))
	_ = rootCmd.RegisterFlagCompletionFunc("clipboard", completeValues([]string{"title", "description", "comment", "commit-msg", "all"}))

	rootCmd.AddCommand(newInitConfigCmd())
	rootCmd.AddCommand(newModelsCmd())
	rootCmd.AddCommand(newCheckCmd(chatFn))
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(newQuickCommitCmd(chatFn))
	rootCmd.AddCommand(newPublishCmd(chatFn))
	rootCmd.AddCommand(newChangelogCmd(chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("review", "Generate a review from a diff", nil, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("title", "Generate only a PR/MR title", []string{"--title-only", "--plain"}, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("commit-message", "Generate only a commit message", []string{"--commit-msg"}, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("verdict", "Generate only a PASS/FAIL verdict", []string{"--exit-code", "--verdict-only", "--plain"}, chatFn))
	rootCmd.AddCommand(newAgentAliasCmd("estimate", "Estimate prompt tokens and input cost", []string{"--debug"}, chatFn))
	rootCmd.AddCommand(newGenAliasesCmd())
	rootCmd.AddCommand(newGenWorkflowCmd())

	rootCmd.AddCommand(&cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(out, true)
			case "zsh":
				return rootCmd.GenZshCompletion(out)
			case "fish":
				return rootCmd.GenFishCompletion(out, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(out)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	})

	return rootCmd
}

func newAgentAliasCmd(name, short string, prefix []string, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              short,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := newRootCmd(chatFn)
			root.SetIn(cmd.InOrStdin())
			root.SetOut(cmd.OutOrStdout())
			root.SetErr(cmd.ErrOrStderr())
			root.SilenceErrors = true
			root.SilenceUsage = true
			translated := make([]string, 0, len(prefix)+len(args))
			translated = append(translated, prefix...)
			translated = append(translated, args...)
			root.SetArgs(translated)
			return root.Execute()
		},
	}
}
