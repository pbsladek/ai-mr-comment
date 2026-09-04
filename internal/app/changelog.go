package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// changelogArgs holds the parsed flag values for the changelog subcommand.
type changelogArgs struct {
	commit           string
	diffFilePath     string
	outputPath       string
	provider         string
	modelOverride    string
	format           string
	systemPromptFlag string
	profile          string
	preset           string
	estimate         bool
	autoYes          bool
	dryRun           bool
}

func runChangelogWithDeps(cmd *cobra.Command, a changelogArgs, chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), deps commandDeps) error {
	cfg, err := deps.loadConfig(a.profile)
	if err != nil {
		return err
	}
	presetPromptSuffix, err := applyChangelogPreset(cmd, a.preset, cfg, &a.format)
	if err != nil {
		return withExitCode(4, err)
	}
	if cmd.Flags().Changed("provider") {
		cfg.Provider = ApiProvider(a.provider)
	}
	if cmd.Flags().Changed("model") {
		setModelOverride(cfg, a.modelOverride)
	}

	if !isSupportedProvider(cfg.Provider) {
		return withExitCode(4, fmt.Errorf("unsupported provider: %s", cfg.Provider))
	}
	if !a.dryRun {
		if cfgErr := validateProviderConfig(cfg); cfgErr != nil {
			return cfgErr
		}
	}
	if cancel := applyRequestTimeout(cmd, cfg); cancel != nil {
		defer cancel()
	}

	if a.format != "text" && a.format != "json" {
		return withExitCode(4, fmt.Errorf("unsupported format %q: must be text or json", a.format))
	}
	if a.dryRun && a.estimate {
		return withExitCode(4, errors.New("changelog --dry-run cannot be combined with --estimate"))
	}

	diffContent, err := resolveDiffWithDeps(cmd, a.commit, a.diffFilePath, deps)
	if err != nil {
		return err
	}
	rawSummary := summarizeDiff(diffContent, "changelog", getModelName(cfg), len(strings.Split(diffContent, "\n")) > 4000)
	diffContent = processDiff(diffContent, 4000)

	prompt, err := resolveChangelogPrompt(a.systemPromptFlag)
	if err != nil {
		return err
	}
	if presetPromptSuffix != "" && a.systemPromptFlag == "" {
		prompt += presetPromptSuffix
	}

	if a.dryRun {
		return writeChangelogDryRun(cmd, cfg, a, rawSummary, prompt)
	}

	if a.estimate {
		showCostEstimate(cmd.Context(), cfg, prompt, diffContent, cmd.OutOrStdout())
		if !promptConfirm(cmd.ErrOrStderr(), os.Stdin, a.autoYes) {
			return nil
		}
	}

	entry, err := timedCall(cfg, "changelog", func() (string, error) {
		return chatFn(cmd.Context(), cfg, cfg.Provider, prompt, diffContent)
	})
	if err != nil {
		if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
			return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
		}
		return err
	}
	entry = strings.TrimSpace(entry)

	return writeChangelogOutputWithDeps(cmd, cfg, a.outputPath, a.format, entry, deps)
}

func writeChangelogDryRun(cmd *cobra.Command, cfg *Config, a changelogArgs, summary diffSummary, prompt string) error {
	out := cmd.OutOrStdout()
	if a.format == "json" {
		return json.NewEncoder(out).Encode(struct {
			DryRun            bool        `json:"dry_run"`
			Provider          string      `json:"provider"`
			Model             string      `json:"model"`
			Preset            string      `json:"preset,omitempty"`
			Summary           diffSummary `json:"summary"`
			WouldCallProvider bool        `json:"would_call_provider"`
			WouldWriteOutput  bool        `json:"would_write_output"`
			PromptBytes       int         `json:"prompt_bytes"`
		}{
			DryRun:            true,
			Provider:          string(cfg.Provider),
			Model:             getModelName(cfg),
			Preset:            a.preset,
			Summary:           summary,
			WouldCallProvider: true,
			WouldWriteOutput:  a.outputPath != "",
			PromptBytes:       len(prompt),
		})
	}
	_, _ = fmt.Fprintln(out, "Dry run: no provider call or file write will be performed.")
	_, _ = fmt.Fprintf(out, "- Provider: %s\n", cfg.Provider)
	_, _ = fmt.Fprintf(out, "- Model: %s\n", getModelName(cfg))
	if a.preset != "" {
		_, _ = fmt.Fprintf(out, "- Preset: %s\n", a.preset)
	}
	_, _ = fmt.Fprintf(out, "- Files: %d\n", summary.FileCount)
	_, _ = fmt.Fprintf(out, "- Additions: %d\n", summary.Additions)
	_, _ = fmt.Fprintf(out, "- Deletions: %d\n", summary.Deletions)
	if a.outputPath != "" {
		_, _ = fmt.Fprintf(out, "- Would write output: %s\n", a.outputPath)
	}
	return nil
}

func resolveDiffWithDeps(cmd *cobra.Command, commit, diffFilePath string, deps commandDeps) (string, error) {
	var diffContent string
	var err error
	if diffFilePath != "" {
		diffContent, err = readCommandInput(cmd, diffFilePath)
	} else if commandStdinIsPiped(cmd) {
		diffContent, err = readCommandInput(cmd, "-")
	} else {
		if !deps.isGitRepo() {
			return "", fmt.Errorf("not a git repository. Run from inside a git repo or use --file to provide a diff")
		}
		diffContent, err = deps.getGitDiff(commit, false, nil)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diffContent) == "" {
		if commit != "" {
			return "", withExitCode(3, fmt.Errorf("no diff found for commit range %q", commit))
		}
		return "", withExitCode(3, fmt.Errorf("no diff found. Specify a commit range with --commit or a file with --file"))
	}
	return diffContent, nil
}

// resolveChangelogPrompt returns the system prompt, optionally overridden by the flag value.
func resolveChangelogPrompt(systemPromptFlag string) (string, error) {
	if systemPromptFlag == "" {
		return changelogPrompt, nil
	}
	return resolveSystemPrompt(systemPromptFlag)
}

func writeChangelogOutputWithDeps(cmd *cobra.Command, cfg *Config, outputPath, format, entry string, deps commandDeps) error {
	if outputPath != "" {
		return writeChangelogFileWithDeps(cfg, outputPath, format, entry, deps)
	}
	return writeChangelogStdout(cmd, cfg, format, entry)
}

func writeChangelogFileWithDeps(cfg *Config, outputPath, format, entry string, deps commandDeps) error {
	var fileContent []byte
	if format == "json" {
		var buf strings.Builder
		if err := json.NewEncoder(&buf).Encode(changelogPayload(cfg, entry)); err != nil {
			return err
		}
		fileContent = []byte(buf.String())
	} else {
		fileContent = []byte(entry + "\n")
	}
	return deps.writeFile(outputPath, fileContent, 0600) //nolint:gosec // G306: 0600 is intentional for user-owned output
}

// writeChangelogStdout writes the changelog entry to the command's stdout.
func writeChangelogStdout(cmd *cobra.Command, cfg *Config, format, entry string) error {
	out := cmd.OutOrStdout()
	if format == "json" {
		return json.NewEncoder(out).Encode(changelogPayload(cfg, entry))
	}
	_, _ = fmt.Fprintln(out, entry)
	return nil
}

// changelogPayload builds the JSON payload struct for changelog output.
func changelogPayload(cfg *Config, entry string) any {
	return struct {
		Changelog string `json:"changelog"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
	}{
		Changelog: entry,
		Provider:  string(cfg.Provider),
		Model:     getModelName(cfg),
	}
}

// newChangelogCmd returns the changelog subcommand, which generates a
// user-facing changelog entry from a commit range using AI.
func newChangelogCmdWithDeps(chatFn func(context.Context, *Config, ApiProvider, string, string) (string, error), deps commandDeps) *cobra.Command {
	var a changelogArgs

	cmd := &cobra.Command{
		Use:   "changelog",
		Short: "Generate a user-facing changelog entry from a commit range",
		Long: `Analyses a git diff (commit range, staged changes, or diff file) and
produces a user-facing changelog entry in Keep a Changelog markdown
format, grouped by Added / Changed / Fixed / Breaking Changes etc.

Examples:
  ai-mr-comment changelog --commit="v1.2.0..HEAD"
  ai-mr-comment changelog --commit="v1.2.0..HEAD" --format=json
  ai-mr-comment changelog --file=my.diff --provider=anthropic`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChangelogWithDeps(cmd, a, chatFn, deps)
		},
	}

	cmd.Flags().StringVar(&a.commit, "commit", "", "Commit or commit range to diff (e.g. v1.2.0..HEAD)")
	cmd.Flags().StringVar(&a.diffFilePath, "file", "", "Path to diff file instead of running git diff")
	cmd.Flags().StringVar(&a.outputPath, "output", "", "Write changelog to this file instead of stdout")
	cmd.Flags().StringVar(&a.provider, "provider", "", "AI provider (openai, anthropic, gemini, ollama, claude-cli, gemini-cli, codex-cli)")
	cmd.Flags().StringVar(&a.modelOverride, "model", "", "Override the model for this run")
	cmd.Flags().StringVar(&a.format, "format", "text", "Output format: text or json")
	cmd.Flags().StringVar(&a.systemPromptFlag, "system-prompt", "", `Override the system prompt for this run. Use @path to read from a file (e.g. --system-prompt=@notes.txt).`)
	cmd.Flags().StringVar(&a.preset, "preset", "", "Preset defaults: ci, local-fast, security, release-notes")
	cmd.Flags().BoolVar(&a.estimate, "estimate", false, "Show token/cost estimate and prompt for confirmation before calling the API")
	cmd.Flags().BoolVarP(&a.autoYes, "yes", "y", false, "Auto-confirm the cost estimate prompt (use with --estimate)")
	cmd.Flags().BoolVar(&a.dryRun, "dry-run", false, "Print what would happen without calling the provider or writing files")
	cmd.Flags().StringVar(&a.profile, "profile", "", "Named config profile to activate (defined in ~/.ai-mr-comment.toml under [profile.<name>])")
	_ = cmd.RegisterFlagCompletionFunc("provider", completeValues(providerNames))
	_ = cmd.RegisterFlagCompletionFunc("format", completeValues([]string{"text", "json"}))
	_ = cmd.RegisterFlagCompletionFunc("preset", completeValues(presetNames))
	_ = cmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	return cmd
}
