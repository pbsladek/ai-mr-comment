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

	"github.com/spf13/cobra"
)

// newRootCmd builds the root cobra command, wiring flags to the provided chatFn.
// Accepting chatFn as a parameter allows tests to inject a mock without real API calls.
func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	return newRootCmdWithDeps(chatFn, defaultCommandDeps())
}

func newRootCmdWithDeps(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), deps commandDeps) *cobra.Command {
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
			runStart := deps.now()
			cfg, err := deps.loadConfig(profileName)
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
					debugLog(cfg, "total elapsed: %dms", deps.since(runStart).Milliseconds())
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
			if quiet {
				format = "json"
			}
			if inputFormat == "" {
				inputFormat = "text"
			}
			effectiveTemplate := cfg.Template
			rootOpts := RootOptions{
				Commit:              commit,
				DiffFilePath:        diffFilePath,
				OutputPath:          outputPath,
				PRURL:               prURL,
				Format:              format,
				InputFormat:         inputFormat,
				StreamMode:          streamMode,
				Clipboard:           clipboardFlag,
				EffectiveTemplate:   effectiveTemplate,
				Staged:              staged,
				Debug:               debug,
				Estimate:            estimate,
				DryRun:              dryRun,
				ChangedFilesOnly:    changedFilesOnly,
				SummaryOnly:         summaryOnly,
				PrintPrompt:         printPrompt,
				PrintRequest:        printRequest,
				GenerateTitle:       generateTitle,
				GenerateCommitMsg:   generateCommitMsg,
				MultiLine:           multiLine,
				ExitCode:            exitCodeFlag,
				Post:                postFlag,
				UpdateTitle:         updateTitleFlag,
				UpdateDescription:   updateDescriptionFlag,
				TitleOnly:           titleOnly,
				SystemPromptChanged: cmd.Flags().Changed("system-prompt"),
				TemplateChanged:     cmd.Flags().Changed("template"),
				MRStyles: enabledRootStyleFlags(map[string]bool{
					"chaos":       mrChaos,
					"haiku":       mrHaiku,
					"roast":       mrRoast,
					"intern":      mrIntern,
					"shakespeare": mrShakespeare,
					"manager":     mrManager,
					"yoda":        mrYoda,
					"excuse":      mrExcuse,
				}),
			}
			if err := validateRootOptions(rootOpts); err != nil {
				return withExitCode(4, err)
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

			diffFetchStart := deps.now()
			diffContent, diffSource, err := resolveRootDiffInput(cmd, cfg, rootOpts, exclude, deps)
			debugLog(cfg, "diff fetch: elapsed=%dms", deps.since(diffFetchStart).Milliseconds())
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

			diffContent = prependLocalBranchContext(diffContent, diffSource, rootOpts, deps, cfg)

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

			promptResult, err := resolveRootPrompt(rootPromptRequest{
				Template:             cfg.Template,
				SystemPromptOverride: systemPromptFlag,
				PresetSuffix:         presetPromptSuffix,
				ExitCode:             exitCodeFlag,
				MRStyles:             rootOpts.MRStyles,
			})
			if err != nil {
				return err
			}
			if promptResult.TemplateWarning != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", promptResult.TemplateWarning)
			}
			systemPrompt := promptResult.SystemPrompt
			debugLog(cfg, "template: name=%q source=%s length=%d", cfg.Template, promptResult.TemplateSource, len(systemPrompt))
			if systemPromptFlag != "" {
				debugLog(cfg, "system-prompt: override applied length=%d", len(systemPrompt))
			}
			if len(rootOpts.MRStyles) > 0 {
				debugLog(cfg, "style: %s mode enabled", rootOpts.MRStyles[0])
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
			generation, err := generateRootProviderOutput(rootGenerationRequest{
				Context:      cmd.Context(),
				Config:       cfg,
				Options:      rootOpts,
				Chat:         chatFn,
				SystemPrompt: systemPrompt,
				DiffContent:  diffContent,
				SmartChunk:   smartChunk,
				ShouldStream: shouldStream,
				Out:          out,
			})
			if err != nil {
				return err
			}
			comment := generation.Comment
			title := generation.Title
			commitMessage := generation.CommitMessage
			streamedOK := generation.StreamedOK

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

			payload := buildRootOutputPayload(cfg, titleOnly, generateCommitMsg, title, comment, commitMessage, verdict, diffSource, diffTruncated)

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
					if err := deps.clipboard(clipContent); err != nil {
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
				if err := deps.writeFile(outputPath, fileContent, 0600); err != nil {
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
					metadata, metaErr := deps.getRemoteMetadata(cmd.Context(), cfg, prURL)
					if metaErr != nil {
						return metaErr
					}
					body := mergeManagedSection(metadata.Description, comment)
					updateDescription = &body
				}
				if err := deps.updateRemoteMetadata(cmd.Context(), cfg, prURL, updateTitle, updateDescription); err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Updated PR/MR metadata.")
			}

			// --post: publish the generated comment back to the GitHub PR or GitLab MR.
			if postFlag {
				postBody := comment
				if title != "" {
					postBody = "**" + title + "**\n\n" + comment
				}
				if err := deps.postRemoteComment(cmd.Context(), cfg, prURL, postBody); err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Posted comment to PR/MR.")
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
	rootCmd.AddCommand(newQuickCommitCmdWithDeps(chatFn, deps))
	rootCmd.AddCommand(newPublishCmdWithDeps(chatFn, deps))
	rootCmd.AddCommand(newChangelogCmdWithDeps(chatFn, deps))
	rootCmd.AddCommand(newAgentAliasCmdWithDeps("review", "Generate a review from a diff", nil, chatFn, deps))
	rootCmd.AddCommand(newAgentAliasCmdWithDeps("title", "Generate only a PR/MR title", []string{"--title-only", "--plain"}, chatFn, deps))
	rootCmd.AddCommand(newAgentAliasCmdWithDeps("commit-message", "Generate only a commit message", []string{"--commit-msg"}, chatFn, deps))
	rootCmd.AddCommand(newAgentAliasCmdWithDeps("verdict", "Generate only a PASS/FAIL verdict", []string{"--exit-code", "--verdict-only", "--plain"}, chatFn, deps))
	rootCmd.AddCommand(newAgentAliasCmdWithDeps("estimate", "Estimate prompt tokens and input cost", []string{"--debug"}, chatFn, deps))
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

func newAgentAliasCmdWithDeps(name, short string, prefix []string, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), deps commandDeps) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              short,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := newRootCmdWithDeps(chatFn, deps)
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
