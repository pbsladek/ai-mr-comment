// Package main is the entry point for ai-mr-comment, a CLI tool that generates
// MR/PR comments from git diffs using AI providers.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

var debugWriterMu sync.Mutex

func main() {
	if err := newRootCmd(chatCompletions).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// parseVerdict extracts the VERDICT line prepended by the AI when --exit-code is
// active. Returns the verdict token ("PASS", "FAIL", or "UNKNOWN") and the body
// with the verdict line stripped. "UNKNOWN" indicates a missing/invalid verdict
// line and should be handled as fail-closed by callers.
func parseVerdict(comment string) (verdict, body string) {
	if strings.HasPrefix(comment, "VERDICT: ") {
		lines := strings.SplitN(comment, "\n", 2)
		verdict = strings.TrimSpace(strings.TrimPrefix(lines[0], "VERDICT: "))
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		return verdict, body
	}
	return "UNKNOWN", comment
}

// newRootCmd builds the root cobra command, wiring flags to the provided chatFn.
// Accepting chatFn as a parameter allows tests to inject a mock without real API calls.
func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider, modelOverride, templateName, format, prURL, clipboardFlag, systemPromptFlag string
	var debug, staged, smartChunk, generateTitle, generateCommitMsg, verbose, exitCodeFlag, postFlag, estimate, autoYes bool
	var exclude []string

	rootCmd := &cobra.Command{
		Use:           "ai-mr-comment",
		Short:         "Generate MR/PR comments using AI",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runStart := time.Now()
			cfg, err := loadConfig()
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

			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}
			if staged && commit != "" {
				return errors.New("--staged and --commit are mutually exclusive")
			}
			if prURL != "" && (staged || commit != "" || diffFilePath != "") {
				return errors.New("--pr cannot be combined with --staged, --commit, or --file")
			}
			if generateCommitMsg && generateTitle {
				return errors.New("--commit-msg and --title cannot be used together")
			}
			if exitCodeFlag && generateCommitMsg {
				return errors.New("--exit-code cannot be used with --commit-msg")
			}
			if postFlag && prURL == "" {
				return errors.New("--post requires --pr to specify a GitHub PR or GitLab MR URL")
			}
			if cmd.Flags().Changed("system-prompt") && cmd.Flags().Changed("template") {
				return errors.New("--system-prompt and --template are mutually exclusive")
			}

			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: must be text or json", format)
			}

			var diffContent string
			var diffSource string
			diffFetchStart := time.Now()
			err = nil
			if prURL != "" {
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
				diffSource = "file: " + diffFilePath
				diffContent, err = readDiffFromFile(diffFilePath)
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
					return fmt.Errorf("no staged changes found. Stage your changes with 'git add' first")
				}
				return fmt.Errorf("no diff found. Make sure you have uncommitted changes or specify a commit range with --commit")
			}
			debugLog(cfg, "diff: source=%s bytes=%d", diffSource, len(diffContent))

			// Prepend the current branch name when diffing a local git repo.
			// This lets the AI and templates reference the branch/ticket number
			// (e.g. "feat/ABC-123-add-login") for linking in systems like Jira.
			// Skipped for --file and --pr since those have no local branch context.
			if prURL == "" && diffFilePath == "" {
				if branch, branchErr := getCurrentBranch(); branchErr == nil && branch != "" {
					diffContent = "Branch: " + branch + "\n\n" + diffContent
					debugLog(cfg, "branch: name=%s", branch)
				}
			}

			out := cmd.OutOrStdout()
			// Split once; reuse the slice for line-count logging and truncation.
			diffLines := strings.Split(diffContent, "\n")
			rawLines := len(diffLines)
			diffContent = truncateDiff(diffLines, 4000)
			debugLog(cfg, "diff: lines before truncation=%d after=%d (max=4000)", rawLines, strings.Count(diffContent, "\n")+1)

			systemPrompt, templateErr := NewPromptTemplate(cfg.Template)
			if templateErr != nil {
				_, _ = fmt.Fprintln(os.Stderr, "Warning:", templateErr)
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

			// When --exit-code is set, prepend a verdict instruction so the AI starts
			// its response with "VERDICT: PASS" or "VERDICT: FAIL".
			const exitCodePreamble = "Before your review, output a verdict on the very first line in exactly this format:\nVERDICT: PASS\nor\nVERDICT: FAIL\nUse FAIL if the diff contains critical bugs, security vulnerabilities, data loss risks, or broken public APIs. Use PASS for everything else. Then continue with your normal review on the next line.\n\n"
			if exitCodeFlag {
				systemPrompt = exitCodePreamble + systemPrompt
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
			isTTY := term.IsTerminal(int(os.Stdout.Fd()))
			shouldStream := isTTY && format == "text" && !smartChunk && outputPath == ""
			debugLog(cfg, "streaming: tty=%v format=%s smart-chunk=%v output-file=%q → enabled=%v",
				isTTY, format, smartChunk, outputPath, shouldStream)
			// streamedOK is set to true only when streaming completes successfully.
			// The output block uses it to decide whether body was already written.
			var streamedOK bool

			var comment string
			var title string
			var commitMessage string
			if generateCommitMsg {
				// Skip description generation; produce only a commit message.
				debugLog(cfg, "commit-msg: generating commit message with separate API call")
				commitMessage, err = timedCall(cfg, "commit-msg", func() (string, error) {
					return chatFn(cmd.Context(), cfg, cfg.Provider, commitMsgPrompt, diffContent)
				})
				if err != nil {
					if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
						return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
					}
					return err
				}
				commitMessage = strings.TrimSpace(commitMessage)
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
				needsTitle := (generateTitle || format == "json") && !generateCommitMsg
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
			if !generateCommitMsg && err != nil {
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
			if (generateTitle || format == "json") && !generateCommitMsg && title == "" {
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
			}
			var payload outputJSON
			if generateCommitMsg {
				payload = outputJSON{
					CommitMessage: commitMessage,
					Provider:      string(cfg.Provider),
					Model:         getModelName(cfg),
				}
			} else {
				payload = outputJSON{
					Title:       title,
					Description: comment,
					Comment:     comment,
					Verdict:     verdict,
					Provider:    string(cfg.Provider),
					Model:       getModelName(cfg),
				}
			}

			if format == "json" {
				if err := json.NewEncoder(out).Encode(payload); err != nil {
					return err
				}
			} else if generateCommitMsg {
				// --commit-msg text output: just the message, no headers, clean for shell piping.
				_, _ = fmt.Fprintln(out, commitMessage)
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
				} else if generateCommitMsg {
					fileContent = []byte(commitMessage + "\n")
				} else {
					fileContent = []byte(comment)
				}
				return os.WriteFile(outputPath, fileContent, 0600)
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
				os.Exit(2)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&commit, "commit", "", "Commit or commit range")
	rootCmd.Flags().StringVar(&diffFilePath, "file", "", "Path to diff file")
	rootCmd.Flags().StringVar(&prURL, "pr", "", "GitHub PR or GitLab MR URL (e.g. https://github.com/owner/repo/pull/123 or https://gitlab.com/group/project/-/merge_requests/42)")
	rootCmd.Flags().StringVar(&outputPath, "output", "", "Output file path")
	rootCmd.Flags().StringVar(&provider, "provider", "openai", "API provider (openai, anthropic, gemini, ollama)")
	rootCmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run (e.g. gpt-4o, claude-opus-4-6, gemini-2.5-flash)")
	rootCmd.Flags().StringVarP(&templateName, "template", "t", "default", "Prompt template to use (e.g., default, conventional, technical)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Estimate token usage")
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "Enable verbose debug logging to stderr")
	rootCmd.Flags().BoolVar(&staged, "staged", false, "Diff staged changes only (git diff --cached)")
	rootCmd.Flags().StringVar(&clipboardFlag, "clipboard", "", "Copy to clipboard: title, description, or all")
	rootCmd.Flags().StringArrayVar(&exclude, "exclude", nil, "Exclude files matching pattern (e.g. vendor/**, *.sum). Can be repeated.")
	rootCmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	rootCmd.Flags().BoolVar(&smartChunk, "smart-chunk", false, "Split large diffs by file, summarize each, then combine")
	rootCmd.Flags().BoolVar(&generateTitle, "title", false, "Generate a concise MR/PR title in addition to the comment")
	rootCmd.Flags().BoolVar(&generateCommitMsg, "commit-msg", false, "Generate a git commit message instead of a full MR/PR description")
	rootCmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false, "Exit with code 2 if the AI detects critical issues in the diff")
	rootCmd.Flags().BoolVar(&postFlag, "post", false, "Post the generated comment back to the GitHub PR or GitLab MR (requires --pr)")
	rootCmd.Flags().StringVar(&systemPromptFlag, "system-prompt", "", `Override the system prompt for this run. Use @path to read from a file (e.g. --system-prompt=@review.txt). Mutually exclusive with --template.`)
	rootCmd.Flags().BoolVar(&estimate, "estimate", false, "Show token/cost estimate and prompt for confirmation before calling the API")
	rootCmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Auto-confirm the cost estimate prompt (use with --estimate)")

	rootCmd.AddCommand(newInitConfigCmd())
	rootCmd.AddCommand(newModelsCmd())
	rootCmd.AddCommand(newQuickCommitCmd(chatFn))
	rootCmd.AddCommand(newChangelogCmd(chatFn))
	rootCmd.AddCommand(newGenAliasesCmd())

	rootCmd.AddCommand(&cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	})

	return rootCmd
}

// defaultConfigTOML is the template written by the init-config subcommand.
// It documents every supported key with its default value.
const defaultConfigTOML = `# ai-mr-comment configuration
# Place this file at ~/.ai-mr-comment.toml or in the project root.

# Default AI provider: openai | anthropic | gemini | ollama
provider = "openai"

# Default prompt template.
# Built-in options: default | conventional | technical | user-focused | emoji | sassy | monday
# You can also create custom templates in ~/.config/ai-mr-comment/templates/<name>.tmpl
template = "default"

# --- OpenAI ---
# openai_api_key = ""   # or set OPENAI_API_KEY env var
openai_model    = "gpt-4.1-mini"
openai_endpoint = "https://api.openai.com/v1/"
# Other OpenAI models: gpt-4.1, o3, o3-mini, gpt-4o, gpt-4o-mini

# --- Anthropic ---
# anthropic_api_key = ""   # or set ANTHROPIC_API_KEY env var
anthropic_model    = "claude-sonnet-4-6"
anthropic_endpoint = "https://api.anthropic.com"
# Other Anthropic models: claude-opus-4-6, claude-haiku-4-5-20251001

# --- Google Gemini ---
# gemini_api_key = ""   # or set GEMINI_API_KEY env var
gemini_model = "gemini-2.5-flash"
# Other Gemini models: gemini-2.5-pro, gemini-3-flash-preview, gemini-3-pro-preview

# --- Ollama (local) ---
ollama_model    = "llama3"
ollama_endpoint = "http://localhost:11434/api/generate"
# Other Ollama models: llama3.1, llama3.2, mistral, codellama, phi3

# --- GitHub / GitHub Enterprise ---
# github_token = ""    # or set GITHUB_TOKEN env var
# github_base_url = "" # GitHub Enterprise host, e.g. https://github.mycompany.com

# --- GitLab / Self-Hosted GitLab ---
# gitlab_token = ""    # or set GITLAB_TOKEN env var
# gitlab_base_url = "" # Self-hosted GitLab host, e.g. https://gitlab.mycompany.com
`

// newInitConfigCmd returns the init-config subcommand, which writes a commented
// TOML configuration file to the destination path (default: ~/.ai-mr-comment.toml).
func newInitConfigCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "init-config",
		Short: "Write a default config file to ~/.ai-mr-comment.toml",
		Long: `Writes a commented TOML configuration file with all supported settings and
their defaults. Edit the generated file to add your API keys and customise
models, endpoints, or the default provider.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := outputPath
			if dest == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("could not determine home directory: %w", err)
				}
				dest = home + "/.ai-mr-comment.toml"
			}

			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("config file already exists at %s (remove it first or use --output to choose a different path)", dest)
			}

			if err := os.WriteFile(dest, []byte(defaultConfigTOML), 0600); err != nil {
				return fmt.Errorf("could not write config file: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config file written to %s\n", dest)
			return nil
		},
	}

	cmd.Flags().StringVar(&outputPath, "output", "", "Write config to this path instead of ~/.ai-mr-comment.toml")
	return cmd
}

// getModelName returns the configured model name for the active provider.
func getModelName(cfg *Config) string {
	switch cfg.Provider {
	case OpenAI:
		return cfg.OpenAIModel
	case Anthropic:
		return cfg.AnthropicModel
	case Gemini:
		return cfg.GeminiModel
	case Ollama:
		return cfg.OllamaModel
	default:
		return "unknown"
	}
}

func validateProviderConfig(cfg *Config) error {
	if cfg.Provider != OpenAI && cfg.Provider != Anthropic && cfg.Provider != Ollama && cfg.Provider != Gemini {
		return errors.New("unsupported provider: " + string(cfg.Provider))
	}
	if cfg.Provider == OpenAI && cfg.OpenAIAPIKey == "" {
		return fmt.Errorf("missing OpenAI API key.\n\n" +
			"Please set the OPENAI_API_KEY environment variable or configure 'openai_api_key' in ~/.ai-mr-comment.toml")
	}
	if cfg.Provider == Anthropic && cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("missing Anthropic API key.\n\n" +
			"Please set the ANTHROPIC_API_KEY environment variable or configure 'anthropic_api_key' in ~/.ai-mr-comment.toml")
	}
	if cfg.Provider == Gemini && cfg.GeminiAPIKey == "" {
		return fmt.Errorf("missing Gemini API key.\n\n" +
			"Please set the GEMINI_API_KEY environment variable or configure 'gemini_api_key' in ~/.ai-mr-comment.toml")
	}
	return nil
}

// debugLog writes a formatted debug message to cfg.DebugWriter when verbose mode is enabled.
// The message is prefixed with "[debug] " and terminated with a newline.
func debugLog(cfg *Config, format string, args ...any) {
	if cfg.DebugWriter == nil {
		return
	}
	debugWriterMu.Lock()
	defer debugWriterMu.Unlock()
	_, _ = fmt.Fprintf(cfg.DebugWriter, "[debug] "+format+"\n", args...)
}

// timedCall invokes fn, then logs the elapsed time, response size, and any error.
// It is a no-op when verbose mode is disabled (cfg.DebugWriter == nil).
func timedCall(cfg *Config, label string, fn func() (string, error)) (string, error) {
	start := time.Now()
	result, err := fn()
	elapsed := time.Since(start).Milliseconds()
	if err == nil {
		debugLog(cfg, "api: %s completed in %dms chars=%d lines=%d",
			label, elapsed, len(result), len(strings.Split(result, "\n")))
	} else {
		debugLog(cfg, "api: %s failed in %dms: %v", label, elapsed, err)
	}
	return result, err
}

// showCostEstimate prints token and cost estimation to w.
func showCostEstimate(ctx context.Context, cfg *Config, systemPrompt, diffContent string, w io.Writer) {
	model := getModelName(cfg)
	estimator := NewTokenEstimator(cfg)
	totalTokens, err := estimator.CountTokens(ctx, model, systemPrompt, diffContent)
	if err != nil {
		_, _ = fmt.Fprintf(w, "Error estimating tokens: %v\n", err)
		fallback := &HeuristicTokenEstimator{}
		totalTokens, _ = fallback.CountTokens(context.Background(), "", systemPrompt, diffContent)
		_, _ = fmt.Fprintln(w, "Using heuristic fallback.")
	}
	cost := EstimateCost(model, totalTokens)
	_, _ = fmt.Fprintln(w, "Token & Cost Estimation:")
	_, _ = fmt.Fprintf(w, "- Model: %s\n", model)
	_, _ = fmt.Fprintf(w, "- Diff lines: %d\n", strings.Count(diffContent, "\n")+1)
	_, _ = fmt.Fprintf(w, "- Estimated Input Tokens: %d\n", totalTokens)
	_, _ = fmt.Fprintf(w, "- Estimated Input Cost: $%.6f\n", cost)
	_, _ = fmt.Fprintln(w, "\nNote: Output tokens and cost depend on the generated response length.")
}

// promptConfirm writes a "Proceed? [y/N]: " prompt to promptWriter and reads
// one line from stdinReader. Returns true only if the user types "y" or "Y".
// Auto-confirms when autoYes is true. Auto-declines when stdinReader is not
// an interactive terminal (e.g. in CI or piped input).
func promptConfirm(promptWriter io.Writer, stdinReader io.Reader, autoYes bool) bool {
	if autoYes {
		return true
	}
	if f, ok := stdinReader.(*os.File); ok {
		if !term.IsTerminal(int(f.Fd())) {
			_, _ = fmt.Fprintln(promptWriter, "Non-interactive mode: auto-declining. Use --yes to proceed.")
			return false
		}
	} else {
		// Non-*os.File reader (e.g. strings.Reader in tests) is non-interactive.
		_, _ = fmt.Fprintln(promptWriter, "Non-interactive mode: auto-declining. Use --yes to proceed.")
		return false
	}
	_, _ = fmt.Fprint(promptWriter, "Proceed? [y/N]: ")
	var line string
	_, _ = fmt.Fscan(stdinReader, &line)
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// setModelOverride applies a CLI --model value to the correct provider field in cfg.
func setModelOverride(cfg *Config, model string) {
	switch cfg.Provider {
	case OpenAI:
		cfg.OpenAIModel = model
	case Anthropic:
		cfg.AnthropicModel = model
	case Gemini:
		cfg.GeminiModel = model
	case Ollama:
		cfg.OllamaModel = model
	}
}

// providerModels lists known models for each provider, used by the `models` subcommand.
var providerModels = map[ApiProvider][]string{
	OpenAI: {
		"gpt-4.1",
		"gpt-4.1-mini",
		"gpt-4.1-nano",
		"o3",
		"o3-mini",
		"gpt-4o",
		"gpt-4o-mini",
	},
	Anthropic: {
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-5-20251101",
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-20250514",
		"claude-3-7-sonnet-20250219",
		"claude-3-haiku-20240307",
	},
	Gemini: {
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-3-flash-preview",
		"gemini-3-pro-preview",
		"gemini-2.0-flash",
	},
	Ollama: {
		"llama3",
		"llama3.1",
		"llama3.2",
		"mistral",
		"codellama",
		"phi3",
	},
}

// newModelsCmd returns the models subcommand, which lists known models for the active provider.
func newModelsCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models for a provider",
		Long:  `Prints the known model names for the given provider. Use --provider to select a provider (defaults to the configured one).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := ApiProvider(provider)
			models, ok := providerModels[p]
			if !ok {
				return fmt.Errorf("unknown provider %q: choose from openai, anthropic, gemini, ollama", provider)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Models for provider %s:\n\n", p)
			for _, m := range models {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", m)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Use --model <name> to select a model for a run.\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "openai", "Provider to list models for (openai, anthropic, gemini, ollama)")
	return cmd
}

// newQuickCommitCmd returns a command that stages all changes, generates an
// AI commit message, commits, and pushes — all in one step.
func newQuickCommitCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var provider, modelOverride, format string
	var dryRun, noPush bool

	cmd := &cobra.Command{
		Use:   "quick-commit",
		Short: "Stage, AI-commit, and push in one step",
		Long: `Stages all changes (git add .), generates a conventional commit message
using AI, commits with that message, and pushes to the current branch's
remote. Use --dry-run to preview the generated message without committing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isGitRepo() {
				return fmt.Errorf("not a git repository")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("provider") {
				cfg.Provider = ApiProvider(provider)
			}
			if cmd.Flags().Changed("model") {
				setModelOverride(cfg, modelOverride)
			}
			if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
				return cfgErr
			}

			branch, err := getCurrentBranch()
			if err != nil {
				return fmt.Errorf("could not determine current branch: %w", err)
			}
			if branch == "" {
				return fmt.Errorf("cannot quick-commit in detached HEAD state")
			}

			out := cmd.OutOrStdout()

			// Stage everything (skipped in dry-run).
			if !dryRun {
				_, _ = fmt.Fprintln(out, "Staging all changes (git add .)...")
				if err := gitAdd(); err != nil {
					return err
				}
			}

			// Get the diff to feed to the AI.
			// After a real git add the staged diff is the right source.
			// In dry-run mode nothing was staged, so use the full working-tree diff
			// (staged + unstaged) so the preview is still meaningful.
			var diffContent string
			if dryRun {
				diffContent, err = getGitDiff("", false, nil)
			} else {
				diffContent, err = getGitDiff("", true, nil)
			}
			if err != nil {
				return fmt.Errorf("reading diff: %w", err)
			}
			if strings.TrimSpace(diffContent) == "" {
				return fmt.Errorf("no changes found to generate a commit message for")
			}

			// Prepend branch name so the AI can reference the ticket key.
			diffContent = "Branch: " + branch + "\n\n" + diffContent
			diffContent = processDiff(diffContent, 4000)

			// Generate commit message via AI.
			commitMessage, err := chatFn(cmd.Context(), cfg, cfg.Provider, commitMsgPrompt, diffContent)
			if err != nil {
				return fmt.Errorf("generating commit message: %w", err)
			}
			commitMessage = strings.TrimSpace(commitMessage)
			if commitMessage == "" {
				return fmt.Errorf("AI returned an empty commit message")
			}

			if format == "json" {
				if err := json.NewEncoder(out).Encode(struct {
					CommitMessage string `json:"commit_message"`
				}{CommitMessage: commitMessage}); err != nil {
					return err
				}
			} else {
				_, _ = fmt.Fprintf(out, "%s\n\n", commitMessage)
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
			if err := gitCommit(commitMessage); err != nil {
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
			if err := gitPush(branch); err != nil {
				return err
			}

			if format != "json" {
				_, _ = fmt.Fprintln(out, "Done.")
				if remoteURL, remErr := getRemoteURL(); remErr == nil {
					if createURL := prCreateURL(remoteURL, branch); createURL != "" {
						_, _ = fmt.Fprintf(out, "\nOpen PR/MR: %s\n", createURL)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "openai", "AI provider to use (openai, anthropic, gemini, ollama)")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate and print the commit message without staging, committing, or pushing")
	cmd.Flags().BoolVar(&noPush, "no-push", false, "Commit but skip the push step")
	return cmd
}

// aliasBlock is the shell snippet printed by gen-aliases.
// It is a Go constant so tests can verify the exact output.
const aliasBlock = `# ai-mr-comment aliases
# Generated by: ai-mr-comment gen-aliases
# Add to your ~/.bashrc or ~/.zshrc, then reload with: source ~/.bashrc

alias amc='ai-mr-comment'                          # main command
alias amc-review='ai-mr-comment'                   # review current diff
alias amc-staged='ai-mr-comment --staged'          # review staged changes
alias amc-commit='ai-mr-comment --commit-msg'      # generate commit message
alias amc-title='ai-mr-comment --title'            # include a title
alias amc-json='ai-mr-comment --format=json'       # JSON output
alias amc-debug='ai-mr-comment --debug'            # token/cost estimate
alias amc-qc='ai-mr-comment quick-commit'          # stage + AI commit + push
alias amc-qc-dry='ai-mr-comment quick-commit --dry-run'  # preview commit msg
alias amc-cl='ai-mr-comment changelog'             # generate changelog entry
alias amc-models='ai-mr-comment models'            # list available models
alias amc-init='ai-mr-comment init-config'         # write default config
`

// newGenAliasesCmd returns the gen-aliases subcommand, which prints a shell
// alias block for ai-mr-comment to stdout. Users source the output into their
// shell profile to get short amc-* aliases.
func newGenAliasesCmd() *cobra.Command {
	var shell, outputPath string

	cmd := &cobra.Command{
		Use:   "gen-aliases",
		Short: "Print shell aliases for ai-mr-comment (amc and amc-*)",
		Long: `Prints a block of shell alias definitions to stdout.

Add them to your shell profile with one of:

  # Append once:
  ai-mr-comment gen-aliases >> ~/.bashrc   # or ~/.zshrc

  # Re-generate on every shell start (always up-to-date):
  eval "$(ai-mr-comment gen-aliases)"

Aliases defined:
  amc            — shorthand for ai-mr-comment
  amc-review     — review current diff
  amc-staged     — review staged changes
  amc-commit     — generate a commit message (--commit-msg)
  amc-title      — generate comment + title
  amc-json       — output as JSON
  amc-debug      — show token/cost estimate
  amc-qc         — quick-commit (stage + AI commit + push)
  amc-qc-dry     — quick-commit dry-run (preview only)
  amc-cl         — changelog subcommand
  amc-models     — list available models
  amc-init       — write default config file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if shell != "bash" && shell != "zsh" {
				return fmt.Errorf("unsupported shell %q: must be bash or zsh", shell)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprint(out, aliasBlock)

			if outputPath != "" {
				return os.WriteFile(outputPath, []byte(aliasBlock), 0600) //nolint:gosec // G306: user-owned shell config file
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&shell, "shell", "bash", "Target shell: bash or zsh (both use the same alias syntax)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Also write aliases to this file (e.g. ~/.bashrc)")
	return cmd
}
