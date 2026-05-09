package app

import (
	"errors"
	"fmt"
)

type RootOptions struct {
	Commit              string
	DiffFilePath        string
	OutputPath          string
	PRURL               string
	Format              string
	InputFormat         string
	StreamMode          string
	Clipboard           string
	EffectiveTemplate   string
	Staged              bool
	Debug               bool
	Estimate            bool
	DryRun              bool
	ChangedFilesOnly    bool
	SummaryOnly         bool
	PrintPrompt         bool
	PrintRequest        bool
	GenerateTitle       bool
	GenerateCommitMsg   bool
	MultiLine           bool
	ExitCode            bool
	Post                bool
	UpdateTitle         bool
	UpdateDescription   bool
	TitleOnly           bool
	SystemPromptChanged bool
	TemplateChanged     bool
	MRStyles            []string
}

func validateRootOptions(o RootOptions) error {
	if o.Post && o.PRURL == "" && !o.DryRun {
		return errors.New("--post requires --pr to specify a GitHub PR or GitLab MR URL")
	}
	if (o.UpdateTitle || o.UpdateDescription) && o.PRURL == "" && !o.DryRun {
		return errors.New("--update-title and --update-description require --pr to specify a GitHub PR or GitLab MR URL")
	}
	if (o.UpdateTitle || o.UpdateDescription) && o.GenerateCommitMsg {
		return errors.New("--update-title and --update-description cannot be used with --commit-msg")
	}
	if o.UpdateDescription && o.TitleOnly {
		return errors.New("--update-description cannot be used with --title-only")
	}
	if o.Staged && o.Commit != "" {
		return errors.New("--staged and --commit are mutually exclusive")
	}
	if o.PRURL != "" && (o.Staged || o.Commit != "" || o.DiffFilePath != "") {
		return errors.New("--pr cannot be combined with --staged, --commit, or --file")
	}
	if o.MultiLine && !o.GenerateCommitMsg {
		return errors.New("--multi-line requires --commit-msg")
	}
	if isCommitOnlyTemplate(o.EffectiveTemplate) && !o.GenerateCommitMsg {
		return fmt.Errorf("--template %s requires --commit-msg", o.EffectiveTemplate)
	}
	if isMROnlyTemplate(o.EffectiveTemplate) && o.GenerateCommitMsg {
		return fmt.Errorf("--template %s cannot be combined with --commit-msg", o.EffectiveTemplate)
	}
	if o.GenerateCommitMsg && o.GenerateTitle {
		return errors.New("--commit-msg and --title cannot be used together")
	}
	if o.TitleOnly && o.GenerateCommitMsg {
		return errors.New("--title-only cannot be used with --commit-msg")
	}
	if o.ExitCode && o.GenerateCommitMsg {
		return errors.New("--exit-code cannot be used with --commit-msg")
	}
	if o.SystemPromptChanged && o.TemplateChanged {
		return errors.New("--system-prompt and --template are mutually exclusive")
	}
	if len(o.MRStyles) > 1 {
		return errors.New("--chaos, --haiku, --roast, --intern, --shakespeare, --manager, --yoda, and --excuse are mutually exclusive")
	}
	if len(o.MRStyles) > 0 && (o.TemplateChanged || o.SystemPromptChanged) {
		return errors.New("style flags cannot be combined with --template or --system-prompt")
	}
	if len(o.MRStyles) > 0 && o.GenerateCommitMsg {
		return errors.New("style flags cannot be combined with --commit-msg")
	}
	if o.Format != "text" && o.Format != "json" {
		return fmt.Errorf("unsupported format %q: must be text or json", o.Format)
	}
	if o.InputFormat != "text" && o.InputFormat != "json" {
		return fmt.Errorf("unsupported input format %q: must be text or json", o.InputFormat)
	}
	if o.InputFormat == "json" && (o.PRURL != "" || o.Staged || o.Commit != "") {
		return errors.New("--input=json cannot be combined with --pr, --staged, or --commit")
	}
	if o.StreamMode != "" && o.StreamMode != "jsonl" {
		return fmt.Errorf("unsupported stream mode %q: must be jsonl", o.StreamMode)
	}
	if o.StreamMode == "jsonl" && o.OutputPath != "" {
		return errors.New("--stream=jsonl cannot be combined with --output")
	}
	if o.DryRun && o.StreamMode == "jsonl" {
		return errors.New("--dry-run cannot be combined with --stream=jsonl")
	}
	if o.DryRun && (o.Debug || o.Estimate || o.PrintPrompt || o.PrintRequest || o.ChangedFilesOnly || o.SummaryOnly) {
		return errors.New("--dry-run cannot be combined with --debug, --estimate, --print-prompt, --print-request, --changed-files, or --summary-only")
	}
	if (o.ChangedFilesOnly || o.SummaryOnly) && (o.Post || o.Clipboard != "") {
		return errors.New("--changed-files and --summary-only cannot be combined with --post or --clipboard")
	}
	return nil
}

func enabledRootStyleFlags(flags map[string]bool) []string {
	out := make([]string, 0, len(flags))
	for name, enabled := range flags {
		if enabled {
			out = append(out, name)
		}
	}
	return out
}
