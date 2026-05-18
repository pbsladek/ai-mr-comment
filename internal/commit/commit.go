package commit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var ConventionalTypes = []string{"feat", "fix", "docs", "style", "refactor", "test", "chore", "perf", "ci", "build", "revert"}
var QuickMessageTemplateNames = []string{"short", "detailed", "release", "ticket"}

func NormalizeMessage(raw string) string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	candidates := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" || strings.HasPrefix(clean, "```") {
			continue
		}

		if stripped, ok := strings.CutPrefix(clean, "- "); ok {
			clean = strings.TrimSpace(stripped)
		} else if stripped, ok := strings.CutPrefix(clean, "* "); ok {
			clean = strings.TrimSpace(stripped)
		} else if stripped, ok := strings.CutPrefix(clean, "+ "); ok {
			clean = strings.TrimSpace(stripped)
		}
		clean = strings.Trim(clean, "\"'`")

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
		if IsConventionalLine(c) {
			return c
		}
	}
	return candidates[0]
}

func IsConventionalLine(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	for _, typ := range ConventionalTypes {
		if strings.HasPrefix(l, typ+":") {
			return true
		}
		if strings.HasPrefix(l, typ+"!:") {
			return true
		}
		prefix := typ + "("
		if strings.HasPrefix(l, prefix) {
			rest := l[len(prefix):]
			if close := strings.Index(rest, ")"); close > 0 && close+1 < len(rest) && rest[close+1] == ':' {
				return true
			}
			if close := strings.Index(rest, ")"); close > 0 && close+2 < len(rest) && rest[close+1] == '!' && rest[close+2] == ':' {
				return true
			}
		}
	}
	return false
}

func IsValidType(typ string) bool {
	for _, valid := range ConventionalTypes {
		if typ == valid {
			return true
		}
	}
	return false
}

func IsValidQuickMessageTemplate(name string) bool {
	for _, valid := range QuickMessageTemplateNames {
		if name == valid {
			return true
		}
	}
	return false
}

func QuickMessageTemplateImpliesBody(name string) bool {
	switch name {
	case "detailed", "release", "ticket":
		return true
	default:
		return false
	}
}

func ValidateScope(scope string) error {
	if scope == "" {
		return errors.New("--scope cannot be empty")
	}
	for _, r := range scope {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-', '/':
			continue
		}
		return fmt.Errorf("--scope contains invalid character %q", r)
	}
	return nil
}

func ParseConventionalSubject(subject string) (typ, scope, description string, breaking, ok bool) {
	head, desc, hasColon := strings.Cut(subject, ":")
	if !hasColon {
		return "", "", strings.TrimSpace(subject), false, false
	}
	head = strings.TrimSpace(head)
	desc = strings.TrimSpace(desc)
	if stripped, ok := strings.CutSuffix(head, "!"); ok {
		breaking = true
		head = stripped
	}
	if strings.HasSuffix(head, ")") {
		open := strings.Index(head, "(")
		if open <= 0 {
			return "", "", strings.TrimSpace(subject), false, false
		}
		typ = head[:open]
		scope = head[open+1 : len(head)-1]
		if scope == "" {
			return "", "", strings.TrimSpace(subject), false, false
		}
	} else {
		typ = head
	}
	if stripped, ok := strings.CutSuffix(typ, "!"); ok {
		breaking = true
		typ = stripped
	}
	if !IsValidType(typ) {
		return "", "", strings.TrimSpace(subject), false, false
	}
	return typ, scope, desc, breaking, true
}

func ApplyTypeScope(msg, forcedType, forcedScope string) string {
	if forcedType == "" && forcedScope == "" {
		return msg
	}
	subject, rest, hasRest := strings.Cut(msg, "\n")
	typ, scope, description, breaking, ok := ParseConventionalSubject(strings.TrimSpace(subject))
	if !ok {
		typ = "feat"
		description = strings.TrimSpace(subject)
	}
	if forcedType != "" {
		typ = forcedType
	}
	if forcedScope != "" {
		scope = forcedScope
	}
	subject = FormatConventionalSubject(typ, scope, description, breaking)
	if hasRest {
		return subject + "\n" + rest
	}
	return subject
}

func FormatConventionalSubject(typ, scope, description string, breaking bool) string {
	prefix := typ
	if scope != "" {
		prefix += "(" + scope + ")"
	}
	if breaking {
		prefix += "!"
	}
	return prefix + ": " + strings.TrimSpace(description)
}

var typeEmoji = map[string]string{
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

func AppendEmoji(msg string) string {
	subject, rest, hasRest := strings.Cut(msg, "\n")
	emoji := "🚀"
	if _, _, _, breaking, ok := ParseConventionalSubject(strings.TrimSpace(subject)); ok && breaking {
		emoji = "💥"
	} else {
		for t, e := range typeEmoji {
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

func EnforceBreakingSubject(subject string) string {
	typ, scope, description, _, ok := ParseConventionalSubject(strings.TrimSpace(subject))
	if ok {
		return FormatConventionalSubject(typ, scope, description, true)
	}
	return "feat!: " + subject
}

func EnforceBreaking(msg string) string {
	subject, rest, hasRest := strings.Cut(msg, "\n")
	enforced := EnforceBreakingSubject(subject)
	if hasRest {
		return enforced + "\n" + rest
	}
	return enforced
}

func NormalizeBody(raw string) string {
	out := strings.ReplaceAll(raw, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)

	if strings.HasPrefix(out, "```") {
		lines := strings.Split(out, "\n")
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

func LongBodyPromptSuffix(bodyLines int) string {
	if bodyLines <= 0 {
		bodyLines = 25
	}
	return fmt.Sprintf(`

Long-form body mode:
- Generate a detailed multi-line commit body suitable for a PR/MR description.
- Target approximately %d body lines after the subject and blank line.
- Include these sections when relevant: ## Summary, ## Changes, ## Rationale, ## Testing, ## Risks.
- Prefer concrete bullets that reference changed components, behavior, tests, migrations, config, and operational impact.
- Mention backward compatibility, rollout notes, follow-up work, and known limitations when the diff suggests them.
- Keep the subject under 72 characters; the longer detail belongs only in the body.`, bodyLines)
}

func AppendGuidance(prompt, commitType, commitScope, messageTemplate string) string {
	var hints []string
	if commitType != "" {
		hints = append(hints, fmt.Sprintf("- Use conventional commit type `%s`.", commitType))
	}
	if commitScope != "" {
		hints = append(hints, fmt.Sprintf("- Use conventional commit scope `%s`.", commitScope))
	}
	if messageTemplate != "" {
		hints = append(hints, QuickMessageTemplateGuidance(messageTemplate))
	}
	if len(hints) == 0 {
		return prompt
	}
	return prompt + "\n\nQuick-commit overrides:\n" + strings.Join(hints, "\n")
}

func QuickMessageTemplateGuidance(name string) string {
	switch name {
	case "short":
		return "- Use the `short` message template: generate one concise conventional commit subject only; no body unless another flag explicitly requires one."
	case "detailed":
		return "- Use the `detailed` message template: generate a subject plus a markdown body with ## Summary, ## Changes, ## Rationale, and ## Testing sections when relevant."
	case "release":
		return "- Use the `release` message template: generate a subject plus a release-note-ready body focused on user-visible behavior, compatibility, migration notes, risks, and validation."
	case "ticket":
		return "- Use the `ticket` message template: generate a subject plus a body that references the branch ticket key when present and includes issue context, implementation notes, and verification."
	default:
		return ""
	}
}

func AppendSignedOffBy(message, identity string) string {
	trailer := "Signed-off-by: " + identity
	message = strings.TrimSpace(message)
	if message == "" {
		return trailer
	}
	if strings.Contains(message, trailer) {
		return message
	}
	return message + "\n\n" + trailer
}

func SplitEditor(editor string) []string {
	return strings.Fields(editor)
}

func DefaultEditor() string {
	for _, key := range []string{"GIT_EDITOR", "VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "vi"
}

func EditMessage(message string) (string, error) {
	return EditMessageWithEditor(message, DefaultEditor())
}

func EditMessageWithEditor(message, editor string) (string, error) {
	parts := SplitEditor(editor)
	if len(parts) == 0 {
		return "", errors.New("--edit requires a non-empty editor command")
	}
	tmp, err := os.CreateTemp("", "ai-mr-comment-edit-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating editor file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		_ = os.Remove(name)
	}()
	if _, err := tmp.WriteString(strings.TrimSpace(message) + "\n"); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("writing editor file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing editor file: %w", err)
	}
	args := append(parts[1:], name)
	cmd := exec.Command(parts[0], args...) //nolint:gosec // G204: explicit editor command selected by the user via environment
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running editor %q: %w", editor, err)
	}
	edited, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("reading edited commit message: %w", err)
	}
	out := strings.TrimSpace(string(edited))
	if out == "" {
		return "", errors.New("edited commit message is empty")
	}
	return out, nil
}
