package app

import (
	internalcommit "github.com/pbsladek/ai-mr-comment/internal/commit"
)

// normalizeCommitMessage reduces model output to a single-line commit message.
// Some smaller models may return multiple lines or small preambles despite the prompt.
func normalizeCommitMessage(raw string) string {
	return internalcommit.NormalizeMessage(raw)
}

var conventionalCommitTypes = internalcommit.ConventionalTypes
var quickCommitMessageTemplateNames = internalcommit.QuickMessageTemplateNames

func isValidCommitType(typ string) bool {
	return internalcommit.IsValidType(typ)
}

func isValidQuickCommitMessageTemplate(name string) bool {
	return internalcommit.IsValidQuickMessageTemplate(name)
}

func quickCommitMessageTemplateImpliesBody(name string) bool {
	return internalcommit.QuickMessageTemplateImpliesBody(name)
}

func validateCommitScope(scope string) error {
	return internalcommit.ValidateScope(scope)
}

func applyCommitTypeScope(msg, forcedType, forcedScope string) string {
	return internalcommit.ApplyTypeScope(msg, forcedType, forcedScope)
}

// appendCommitEmoji appends a type-matched gitmoji to the subject line of msg.
// The body (everything after the first newline) is left untouched.
// Breaking changes (subject contains "!") always get 💥.
func appendCommitEmoji(msg string) string {
	return internalcommit.AppendEmoji(msg)
}

// enforceBreakingChange ensures a commit message (single- or multi-line) uses
// the feat! type to signal a breaking change. Only the subject (first line) is
// rewritten; the body (everything after the first newline) is preserved as-is.
func enforceBreakingChange(msg string) string {
	return internalcommit.EnforceBreaking(msg)
}

// normalizeCommitBody lightly normalises a multi-line commit message returned
// by the AI when --multi-line is set. Unlike normalizeCommitMessage it does NOT
// collapse the output to a single line — the subject + body structure is kept.
// It strips surrounding whitespace, normalises line endings, unwraps a single
// fenced-code block if the model wrapped the whole output in one, and ensures
// the subject line is a valid conventional commit (prepending "feat: " if not).
func normalizeCommitBody(raw string) string {
	return internalcommit.NormalizeBody(raw)
}

func longCommitBodyPromptSuffix(bodyLines int) string {
	return internalcommit.LongBodyPromptSuffix(bodyLines)
}

func appendCommitGuidance(prompt, commitType, commitScope, messageTemplate string) string {
	return internalcommit.AppendGuidance(prompt, commitType, commitScope, messageTemplate)
}

func appendSignedOffBy(message, identity string) string {
	return internalcommit.AppendSignedOffBy(message, identity)
}

func editCommitMessage(message string) (string, error) {
	return internalcommit.EditMessage(message)
}

func editCommitMessageWithEditor(message, editor string) (string, error) {
	return internalcommit.EditMessageWithEditor(message, editor)
}

func stageQuickCommitChanges(trackedOnly bool) error {
	if trackedOnly {
		return gitAddTracked()
	}
	return gitAdd()
}
