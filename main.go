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
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

// Version is set at build time via -ldflags "-X 'main.Version=...'"
// Falls back to VCS info embedded by the Go toolchain (go install / go build).
var Version = "dev"

// Commit is the short (7-char) commit SHA, set at build time via -ldflags "-X 'main.Commit=...'"
// Falls back to VCS info embedded by the Go toolchain (go install / go build).
var Commit = "unknown"

// CommitFull is the full commit SHA, set at build time via -ldflags "-X 'main.CommitFull=...'"
// Falls back to VCS info embedded by the Go toolchain (go install / go build).
var CommitFull = "unknown"

func init() {
	if Version != "dev" || Commit != "unknown" {
		return
	}
	// Attempt to read VCS metadata that `go build` embeds automatically (Go 1.18+).
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 7 {
					CommitFull = s.Value
					Commit = s.Value[:7]
				}
			case "vcs.version":
				if s.Value != "" {
					Version = s.Value
				}
			}
		}
	}
}

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

// normalizeCommitMessage reduces model output to a single-line commit message.
// Some smaller models may return multiple lines or small preambles despite the prompt.
func normalizeCommitMessage(raw string) string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	candidates := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" || strings.HasPrefix(clean, "```") {
			continue
		}

		// Strip common list markers and quote wrappers.
		switch {
		case strings.HasPrefix(clean, "- "):
			clean = strings.TrimSpace(strings.TrimPrefix(clean, "- "))
		case strings.HasPrefix(clean, "* "):
			clean = strings.TrimSpace(strings.TrimPrefix(clean, "* "))
		case strings.HasPrefix(clean, "+ "):
			clean = strings.TrimSpace(strings.TrimPrefix(clean, "+ "))
		}
		clean = strings.Trim(clean, "\"'`")

		// Strip common labels like "Commit message: ...".
		if idx := strings.Index(clean, ":"); idx > 0 {
			label := strings.ToLower(strings.TrimSpace(clean[:idx]))
			if label == "commit message" || label == "message" {
				clean = strings.TrimSpace(clean[idx+1:])
			}
		}

		clean = strings.Join(strings.Fields(clean), " ")
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}

	if len(candidates) == 0 {
		return ""
	}
	for _, c := range candidates {
		if isConventionalCommitLine(c) {
			return c
		}
	}
	return candidates[0]
}

func isConventionalCommitLine(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	types := []string{"feat", "fix", "docs", "style", "refactor", "test", "chore", "perf", "ci", "build", "revert"}
	for _, typ := range types {
		if strings.HasPrefix(l, typ+":") {
			return true
		}
		prefix := typ + "("
		if strings.HasPrefix(l, prefix) {
			rest := l[len(prefix):]
			if close := strings.Index(rest, ")"); close > 0 && close+1 < len(rest) && rest[close+1] == ':' {
				return true
			}
		}
	}
	return false
}

// commitTypeEmoji maps a conventional commit type to a trailing gitmoji.
// Breaking changes (type containing "!") map to 💥.
// Unknown types fall back to 🚀.
var commitTypeEmoji = map[string]string{
	"feat":     "✨",
	"fix":      "🐛",
	"docs":     "📝",
	"style":    "💄",
	"refactor": "♻️",
	"test":     "🧪",
	"chore":    "🔧",
	"perf":     "⚡",
	"ci":       "👷",
	"build":    "🏗️",
}

// appendCommitEmoji appends a type-matched gitmoji to the subject line of msg.
// The body (everything after the first newline) is left untouched.
// Breaking changes (subject contains "!") always get 💥.
func appendCommitEmoji(msg string) string {
	subject, rest, hasRest := strings.Cut(msg, "\n")
	emoji := "🚀"
	if strings.Contains(subject, "!") {
		emoji = "💥"
	} else {
		for t, e := range commitTypeEmoji {
			if strings.HasPrefix(subject, t+":") || strings.HasPrefix(subject, t+"(") {
				emoji = e
				break
			}
		}
	}
	subject = subject + " " + emoji
	if hasRest {
		return subject + "\n" + rest
	}
	return subject
}

// enforceBreakingChangeSubject rewrites a single-line conventional commit
// subject to include the breaking-change marker (e.g. "feat" → "feat!").
// If the subject already contains "!" it is returned unchanged.
// Non-conventional subjects are prefixed with "feat!: ".
func enforceBreakingChangeSubject(subject string) string {
	if strings.Contains(subject, "!") {
		return subject
	}
	types := []string{"feat", "fix", "chore", "refactor", "perf", "docs", "style", "test", "ci", "build"}
	for _, t := range types {
		// type(scope): description
		if strings.HasPrefix(subject, t+"(") {
			return t + "!" + subject[len(t):]
		}
		// type: description
		if strings.HasPrefix(subject, t+":") {
			return t + "!" + subject[len(t):]
		}
	}
	return "feat!: " + subject
}

// enforceBreakingChange ensures a commit message (single- or multi-line) uses
// the feat! type to signal a breaking change. Only the subject (first line) is
// rewritten; the body (everything after the first newline) is preserved as-is.
func enforceBreakingChange(msg string) string {
	subject, rest, hasRest := strings.Cut(msg, "\n")
	enforced := enforceBreakingChangeSubject(subject)
	if hasRest {
		return enforced + "\n" + rest
	}
	return enforced
}

// normalizeCommitBody lightly normalises a multi-line commit message returned
// by the AI when --multi-line is set. Unlike normalizeCommitMessage it does NOT
// collapse the output to a single line — the subject + body structure is kept.
// It strips surrounding whitespace, normalises line endings, unwraps a single
// fenced-code block if the model wrapped the whole output in one, and ensures
// the subject line is a valid conventional commit (prepending "feat: " if not).
func normalizeCommitBody(raw string) string {
	// Normalise line endings.
	out := strings.ReplaceAll(raw, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)

	// Strip a surrounding fenced code block if the model wrapped the output.
	if strings.HasPrefix(out, "```") {
		lines := strings.Split(out, "\n")
		// Remove first line (``` or ```markdown etc.) and last line (```).
		if len(lines) >= 2 && strings.HasPrefix(lines[len(lines)-1], "```") {
			out = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	subject, rest, hasRest := strings.Cut(out, "\n")
	subject = strings.TrimSpace(subject)
	if hasRest {
		return subject + "\n" + rest
	}
	return subject
}

// newRootCmd builds the root cobra command, wiring flags to the provided chatFn.
// Accepting chatFn as a parameter allows tests to inject a mock without real API calls.
func newRootCmd(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error)) *cobra.Command {
	var commit, diffFilePath, outputPath, provider, modelOverride, templateName, format, prURL, clipboardFlag, systemPromptFlag, profileName string
	var debug, staged, smartChunk, generateTitle, generateCommitMsg, multiLine, verbose, exitCodeFlag, postFlag, estimate, autoYes, versionFlag bool
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
			if multiLine && !generateCommitMsg {
				return errors.New("--multi-line requires --commit-msg")
			}
			// validTemplates is the explicit allowlist of user-facing --template values.
			// Internal prompts (internal-commit-msg, internal-quick-commit-*, etc.) are
			// not listed here and cannot be selected via --template.
			validTemplates := map[string]bool{
				"default":             true,
				"conventional":        true,
				"technical":           true,
				"user-focused":        true,
				"emoji":               true,
				"sassy":               true,
				"monday":              true,
				"jira":                true,
				"commit":              true,
				"commit-emoji":        true,
				"commit-conventional": true,
				"chaos":               true,
				"haiku":               true,
				"roast":               true,
				"intern":              true,
				"shakespeare":         true,
				"manager":             true,
				"yoda":                true,
				"excuse":              true,
			}
			if !validTemplates[templateName] {
				return fmt.Errorf("unknown template %q — valid templates: default, conventional, technical, user-focused, emoji, sassy, monday, jira, commit, commit-emoji, commit-conventional, chaos, haiku, roast, intern, shakespeare, manager, yoda, excuse", templateName)
			}
			commitOnlyTemplates := map[string]bool{"commit": true, "commit-emoji": true, "commit-conventional": true}
			if commitOnlyTemplates[templateName] && !generateCommitMsg {
				return fmt.Errorf("--template %s requires --commit-msg", templateName)
			}
			mrOnlyTemplates := map[string]bool{
				"technical": true, "user-focused": true, "emoji": true, "sassy": true,
				"monday": true, "jira": true, "conventional": true,
				"chaos": true, "haiku": true, "roast": true, "intern": true,
				"shakespeare": true, "manager": true, "yoda": true, "excuse": true,
			}
			if mrOnlyTemplates[templateName] && generateCommitMsg {
				return fmt.Errorf("--template %s cannot be combined with --commit-msg", templateName)
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
			mrStyleFlags := []bool{mrChaos, mrHaiku, mrRoast, mrIntern, mrShakespeare, mrManager, mrYoda, mrExcuse}
			funStyleCount := 0
			for _, f := range mrStyleFlags {
				if f {
					funStyleCount++
				}
			}
			if funStyleCount > 1 {
				return errors.New("--chaos, --haiku, --roast, --intern, --shakespeare, --manager, --yoda, and --excuse are mutually exclusive")
			}
			if funStyleCount > 0 && (cmd.Flags().Changed("template") || cmd.Flags().Changed("system-prompt")) {
				return errors.New("style flags cannot be combined with --template or --system-prompt")
			}
			if funStyleCount > 0 && generateCommitMsg {
				return errors.New("style flags cannot be combined with --commit-msg")
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
			// When writing to a file, suppress all text output to the terminal.
			if outputPath != "" {
				out = io.Discard
			}
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
				debugLog(cfg, "commit-msg: generating commit message with separate API call (multi-line=%v)", multiLine)
				prompt := commitMsgPrompt
				if multiLine {
					prompt = commitMsgBodyPrompt
				} else if commitOnlyTemplates[templateName] {
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
	rootCmd.Flags().BoolVar(&multiLine, "multi-line", false, "Generate a multi-line commit message (subject + body) when used with --commit-msg; body pre-fills the PR/MR description")
	rootCmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false, "Exit with code 2 if the AI detects critical issues in the diff")
	rootCmd.Flags().BoolVar(&postFlag, "post", false, "Post the generated comment back to the GitHub PR or GitLab MR (requires --pr)")
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
# Other OpenAI models: gpt-4.1, gpt-4.1-nano, o3, o3-mini, gpt-4o, gpt-4o-mini

# --- Anthropic ---
# anthropic_api_key = ""   # or set ANTHROPIC_API_KEY env var
anthropic_model    = "claude-sonnet-4-6"
anthropic_endpoint = "https://api.anthropic.com/"
# Other Anthropic models: claude-opus-4-6, claude-haiku-4-5-20251001

# --- Google Gemini ---
# gemini_api_key = ""   # or set GEMINI_API_KEY env var
gemini_model = "gemini-2.5-flash"
# Other Gemini models: gemini-2.5-pro, gemini-2.5-flash-lite, gemini-3-flash-preview, gemini-3-pro-preview

# --- Ollama (local) ---
ollama_model    = "llama3.2"
ollama_endpoint = "http://localhost:11434/api/generate"
# Other Ollama models: llama3.1, llama3, mistral, codellama, phi3

# --- GitHub / GitHub Enterprise ---
# github_token = ""    # or set GITHUB_TOKEN env var
# github_base_url = "" # GitHub Enterprise host, e.g. https://github.mycompany.com

# --- GitLab / Self-Hosted GitLab ---
# gitlab_token = ""    # or set GITLAB_TOKEN env var
# gitlab_base_url = "" # Self-hosted GitLab host, e.g. https://gitlab.mycompany.com

# ---------------------------------------------------------------------------
# Named Profiles
# Switch profiles with: ai-mr-comment --profile <name>
# A profile overrides any top-level setting for that invocation only.
# ---------------------------------------------------------------------------

# Fast / cheap — gpt-4.1-nano for quick reviews and commit messages
[profile.fast]
provider     = "openai"
openai_model = "gpt-4.1-nano"
template     = "conventional"

# OpenAI — gpt-4.1 with technical template for thorough reviews
[profile.openai]
provider     = "openai"
openai_model = "gpt-4.1"
template     = "technical"

# Anthropic — claude-opus-4-6 with technical template
[profile.anthropic]
provider        = "anthropic"
anthropic_model = "claude-opus-4-6"
template        = "technical"

# Gemini — gemini-3-pro-preview with technical template
[profile.gemini]
provider     = "gemini"
gemini_model = "gemini-3-pro-preview"
template     = "technical"

# Local / offline — Ollama, no API key required
[profile.local]
provider     = "ollama"
ollama_model = "llama3.2"
template     = "default"
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
	var provider, modelOverride, format, profileName string
	var dryRun, noPush, breaking, multiLine, emoji, noConventional bool
	var chaos, haiku, roast, fortune bool
	var qcMonday, qcJira, qcEmoji, qcSassy, qcTechnical bool
	var qcIntern, qcShakespeare, qcManager, qcYoda, qcExcuse bool

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

			cfg, err := loadConfigForProfile(profileName)
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
			case noConventional:
				prompt = quickCommitFreePrompt
			default:
				prompt = quickCommitPrompt
			}
			if breaking {
				prompt += "\n\nThis is a BREAKING CHANGE release. You MUST use the 'feat!' type (with an exclamation mark) to signal a breaking change, e.g. \"feat!(scope): description\" or \"feat!: description\"."
				diffContent += "\n\nBREAKING CHANGE: this release introduces a breaking change and must use the feat! conventional commit type."
			}
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
			if format == "json" {
				if err := json.NewEncoder(out).Encode(struct {
					CommitMessage string `json:"commit_message"`
				}{CommitMessage: jsonMsg}); err != nil {
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
			if err := gitCommit(commitMessage, fortuneBody); err != nil {
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
	cmd.Flags().BoolVar(&breaking, "breaking", false, "Mark as a breaking change: forces feat! conventional commit type for a major version bump")
	cmd.Flags().BoolVar(&multiLine, "multi-line", false, "Generate a multi-line commit message (subject + body) that pre-fills the PR/MR title and description")
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
	return cmd
}

// aliasBlock is the shell snippet printed by gen-aliases.
// It is a Go constant so tests can verify the exact output.
const aliasBlock = `# ai-mr-comment v1 aliases
# Generated by: ai-mr-comment gen-aliases
# Add to your ~/.bashrc or ~/.zshrc, then reload with: source ~/.bashrc

alias amc='ai-mr-comment'                                      # main command
alias amc-staged='ai-mr-comment --staged'                      # review staged changes
alias amc-commit='ai-mr-comment --commit-msg --staged'         # generate commit message
alias amc-commit-multi='ai-mr-comment --commit-msg --multi-line --staged'  # multi-line commit (pre-fills PR/MR)
alias amc-title='ai-mr-comment --title'                        # include a PR/MR title
alias amc-json='ai-mr-comment --format=json'                   # JSON output
alias amc-debug='ai-mr-comment --debug'                        # token/cost estimate
alias amc-chaos='ai-mr-comment --chaos'                        # chaotic but accurate MR/PR description
alias amc-haiku='ai-mr-comment --haiku'                        # MR/PR description as haikus
alias amc-roast='ai-mr-comment --roast'                        # sardonically judgmental MR/PR description
alias amc-intern='ai-mr-comment --intern'                      # overly enthusiastic junior-dev MR/PR description
alias amc-shakespeare='ai-mr-comment --shakespeare'            # MR/PR description in Shakespearean English
alias amc-manager='ai-mr-comment --manager'                    # passive-aggressive corporate MR/PR description
alias amc-yoda='ai-mr-comment --yoda'                          # MR/PR description in Yoda syntax
alias amc-excuse='ai-mr-comment --excuse'                      # technically accurate MR/PR description with excuses
alias amc-conventional='ai-mr-comment --template=conventional' # conventional commits style MR/PR description
alias amc-emoji='ai-mr-comment --template=emoji'               # emoji-rich MR/PR description
alias amc-jira='ai-mr-comment --template=jira'                 # Jira-friendly MR/PR description
alias amc-monday='ai-mr-comment --template=monday'             # Monday.com task-style MR/PR description
alias amc-sassy='ai-mr-comment --template=sassy'               # sassy MR/PR description
alias amc-technical='ai-mr-comment --template=technical'       # deeply technical MR/PR description
alias amc-user='ai-mr-comment --template=user-focused'         # user-impact focused MR/PR description
alias amc-qc='ai-mr-comment quick-commit'                      # stage + AI commit + push
alias amc-qc-dry='ai-mr-comment quick-commit --dry-run'        # preview commit msg
alias amc-qc-breaking='ai-mr-comment quick-commit --breaking'  # breaking change commit (feat!)
alias amc-qc-chaos='ai-mr-comment quick-commit --chaos'              # funny/absurd conventional commit
alias amc-qc-haiku='ai-mr-comment quick-commit --haiku'              # commit description as a haiku
alias amc-qc-roast='ai-mr-comment quick-commit --roast'              # passive-aggressive accurate commit
alias amc-qc-fortune='ai-mr-comment quick-commit --fortune'          # commit + dev-wisdom fortune trailer
alias amc-qc-monday='ai-mr-comment quick-commit --monday'            # low-energy Monday-morning commit
alias amc-qc-jira='ai-mr-comment quick-commit --jira'                # commit prefixed with Jira ticket key
alias amc-qc-emoji='ai-mr-comment quick-commit --emoji-commit'       # commit with type-matched gitmoji
alias amc-qc-sassy='ai-mr-comment quick-commit --sassy'              # sassy but accurate commit
alias amc-qc-technical='ai-mr-comment quick-commit --technical'      # maximum technical precision commit
alias amc-qc-intern='ai-mr-comment quick-commit --intern'            # overly enthusiastic junior-dev commit
alias amc-qc-shakespeare='ai-mr-comment quick-commit --shakespeare'  # commit description in Shakespearean English
alias amc-qc-manager='ai-mr-comment quick-commit --manager'          # passive-aggressive corporate commit
alias amc-qc-yoda='ai-mr-comment quick-commit --yoda'                # commit description in Yoda syntax
alias amc-qc-excuse='ai-mr-comment quick-commit --excuse'            # accurate commit with built-in excuse
alias amc-cl='ai-mr-comment changelog'                         # generate changelog entry
alias amc-models='ai-mr-comment models'                        # list available models
alias amc-init='ai-mr-comment init-config'                     # write default config
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
  amc                — shorthand for ai-mr-comment
  amc-staged         — review staged changes
  amc-commit         — generate a commit message (--commit-msg --staged)
  amc-commit-multi   — multi-line commit message that pre-fills PR/MR title+description
  amc-title          — generate comment + PR/MR title
  amc-json           — output as JSON
  amc-debug          — show token/cost estimate
  amc-chaos          — chaotic but accurate MR/PR description
  amc-haiku          — MR/PR description as haikus
  amc-roast          — sardonically judgmental MR/PR description
  amc-intern         — overly enthusiastic junior-dev MR/PR description
  amc-shakespeare    — MR/PR description in Shakespearean English
  amc-manager        — passive-aggressive corporate MR/PR description
  amc-yoda           — MR/PR description in Yoda syntax
  amc-excuse         — technically accurate MR/PR description with excuses
  amc-conventional   — conventional commits style MR/PR description
  amc-emoji          — emoji-rich MR/PR description
  amc-jira           — Jira-friendly MR/PR description
  amc-monday         — Monday.com task-style MR/PR description
  amc-sassy          — sassy MR/PR description
  amc-technical      — deeply technical MR/PR description
  amc-user           — user-impact focused MR/PR description
  amc-qc             — quick-commit (stage + AI commit + push)
  amc-qc-dry         — quick-commit dry-run (preview only)
  amc-qc-breaking    — quick-commit with breaking change (feat!)
  amc-qc-chaos       — quick-commit with funny/absurd conventional commit
  amc-qc-haiku       — quick-commit with commit description as a haiku
  amc-qc-roast       — quick-commit with passive-aggressive accurate commit
  amc-qc-fortune     — quick-commit with dev-wisdom fortune trailer
  amc-qc-monday      — quick-commit with casual Monday-morning tone
  amc-qc-jira        — quick-commit prefixed with Jira ticket key from branch
  amc-qc-emoji       — quick-commit with type-matched gitmoji appended
  amc-qc-sassy       — quick-commit with sassy but accurate message
  amc-qc-technical   — quick-commit with maximum technical precision
  amc-qc-intern      — quick-commit as an overly enthusiastic junior dev
  amc-qc-shakespeare — quick-commit description in Shakespearean English
  amc-qc-manager     — quick-commit in passive-aggressive corporate speak
  amc-qc-yoda        — quick-commit description in Yoda syntax
  amc-qc-excuse      — quick-commit with a built-in excuse
  amc-cl             — changelog subcommand
  amc-models         — list available models
  amc-init           — write default config file`,
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
