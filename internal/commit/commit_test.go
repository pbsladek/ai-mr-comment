package commit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeMessagePrefersConventionalLine(t *testing.T) {
	raw := "Commit message:\nrefactor(parser): simplify token handling\nextra note"
	got := NormalizeMessage(raw)
	if got != "refactor(parser): simplify token handling" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeMessagePrefersBreakingConventionalLine(t *testing.T) {
	raw := "Here is the commit:\nfeat(api)!: remove legacy endpoint\nextra note"
	got := NormalizeMessage(raw)
	if got != "feat(api)!: remove legacy endpoint" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyTypeScope(t *testing.T) {
	got := ApplyTypeScope("feat: add endpoint\n\nBody", "fix", "api")
	want := "fix(api): add endpoint\n\nBody"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppendEmojiPreservesBody(t *testing.T) {
	got := AppendEmoji("fix: repair bug\n\nBody")
	want := "fix: repair bug 🐛\n\nBody"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppendEmojiDoesNotTreatDescriptionExclamationAsBreaking(t *testing.T) {
	got := AppendEmoji("fix: handle timeout!")
	want := "fix: handle timeout! 🐛"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppendEmojiUsesBreakingEmojiForConventionalBreakingSubject(t *testing.T) {
	got := AppendEmoji("feat(api)!: remove v1")
	want := "feat(api)!: remove v1 💥"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnforceBreaking(t *testing.T) {
	got := EnforceBreaking("feat(api): add v2\n\nBody")
	want := "feat(api)!: add v2\n\nBody"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeBodyStripsFence(t *testing.T) {
	got := NormalizeBody("```text\nfeat: add thing\n\nBody\n```")
	want := "feat: add thing\n\nBody"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLongBodyPromptSuffixDefault(t *testing.T) {
	got := LongBodyPromptSuffix(0)
	if !strings.Contains(got, "25 body lines") {
		t.Fatalf("expected default body line count, got %q", got)
	}
}

func TestAppendSignedOffBy(t *testing.T) {
	got := AppendSignedOffBy("feat: add thing", "A User <a@example.com>")
	if !strings.Contains(got, "Signed-off-by: A User <a@example.com>") {
		t.Fatalf("expected trailer, got %q", got)
	}
	if AppendSignedOffBy(got, "A User <a@example.com>") != got {
		t.Fatalf("expected duplicate trailer to be ignored")
	}
}

func TestValidateScope(t *testing.T) {
	if err := ValidateScope("api/v2_test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateScope("bad scope"); err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestQuickMessageTemplateHelpers(t *testing.T) {
	if !IsValidQuickMessageTemplate("detailed") {
		t.Fatal("expected detailed template to be valid")
	}
	if IsValidQuickMessageTemplate("verbose") {
		t.Fatal("expected verbose template to be invalid")
	}
	if !QuickMessageTemplateImpliesBody("release") || QuickMessageTemplateImpliesBody("short") {
		t.Fatal("unexpected template body implication")
	}
	for _, name := range QuickMessageTemplateNames {
		if got := QuickMessageTemplateGuidance(name); got == "" {
			t.Fatalf("expected guidance for %s", name)
		}
	}
	if got := QuickMessageTemplateGuidance("unknown"); got != "" {
		t.Fatalf("expected empty unknown guidance, got %q", got)
	}
}

func TestAppendGuidance(t *testing.T) {
	base := "base prompt"
	if got := AppendGuidance(base, "", "", ""); got != base {
		t.Fatalf("expected unchanged prompt, got %q", got)
	}
	got := AppendGuidance(base, "fix", "cli", "ticket")
	for _, want := range []string{"Use conventional commit type `fix`", "Use conventional commit scope `cli`", "`ticket` message template"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in guidance:\n%s", want, got)
		}
	}
}

func TestParseConventionalSubject(t *testing.T) {
	tests := []struct {
		in          string
		typ         string
		scope       string
		description string
		breaking    bool
		ok          bool
	}{
		{in: "feat(api): add endpoint", typ: "feat", scope: "api", description: "add endpoint", ok: true},
		{in: "feat!: remove endpoint", typ: "feat", description: "remove endpoint", breaking: true, ok: true},
		{in: "feat(api)!: remove endpoint", typ: "feat", scope: "api", description: "remove endpoint", breaking: true, ok: true},
		{in: "not conventional", description: "not conventional", ok: false},
		{in: "weird: value", description: "weird: value", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			typ, scope, desc, breaking, ok := ParseConventionalSubject(tc.in)
			if typ != tc.typ || scope != tc.scope || desc != tc.description || breaking != tc.breaking || ok != tc.ok {
				t.Fatalf("got typ=%q scope=%q desc=%q breaking=%v ok=%v", typ, scope, desc, breaking, ok)
			}
		})
	}
}

func TestEditorHelpers(t *testing.T) {
	t.Setenv("GIT_EDITOR", "nano -w")
	if got := SplitEditor(DefaultEditor()); strings.Join(got, " ") != "nano -w" {
		t.Fatalf("unexpected editor split: %v", got)
	}

	_, err := EditMessageWithEditor("message", "")
	if err == nil || !strings.Contains(err.Error(), "non-empty editor") {
		t.Fatalf("expected empty editor error, got %v", err)
	}
}

func TestEditMessageWithEditorEmptyOutput(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\n: > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := EditMessageWithEditor("feat: generated", editor)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty edited message error, got %v", err)
	}
}

func TestEditMessageWithEditorRunError(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := EditMessageWithEditor("feat: generated", editor)
	if err == nil || !strings.Contains(err.Error(), "running editor") {
		t.Fatalf("expected editor run error, got %v", err)
	}
}

func TestDefaultEditorFallback(t *testing.T) {
	t.Setenv("GIT_EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := DefaultEditor(); got != "vi" {
		t.Fatalf("expected vi fallback, got %q", got)
	}
}

func TestValidateScopeEmpty(t *testing.T) {
	if err := ValidateScope(""); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Fatalf("expected empty scope error, got %v", err)
	}
}

func TestCommitFormattingEdgeCases(t *testing.T) {
	if got := NormalizeMessage("```\n- message: fix: labeled value\n```"); got != "fix: labeled value" {
		t.Fatalf("NormalizeMessage = %q", got)
	}
	if got := NormalizeMessage("* chore: from bullet"); got != "chore: from bullet" {
		t.Fatalf("NormalizeMessage bullet = %q", got)
	}
	if got := NormalizeMessage(""); got != "" {
		t.Fatalf("NormalizeMessage empty = %q", got)
	}

	for _, line := range []string{"docs: update", "feat!: break", "fix(api)!: break"} {
		if !IsConventionalLine(line) {
			t.Fatalf("expected conventional line %q", line)
		}
	}
	if IsConventionalLine("feature: no") {
		t.Fatal("unexpected conventional line")
	}

	if got := ApplyTypeScope("plain subject", "", "cli"); got != "feat(cli): plain subject" {
		t.Fatalf("ApplyTypeScope non-conventional = %q", got)
	}
	if got := FormatConventionalSubject("feat", "", " add thing ", false); got != "feat: add thing" {
		t.Fatalf("FormatConventionalSubject = %q", got)
	}
	if got := AppendEmoji("docs: update docs"); !strings.Contains(got, "📝") {
		t.Fatalf("AppendEmoji docs = %q", got)
	}
	if got := AppendEmoji("unknown: change"); !strings.Contains(got, "🚀") {
		t.Fatalf("AppendEmoji default = %q", got)
	}
	if got := EnforceBreakingSubject("plain subject"); got != "feat!: plain subject" {
		t.Fatalf("EnforceBreakingSubject = %q", got)
	}
	if got := EnforceBreaking("plain subject"); got != "feat!: plain subject" {
		t.Fatalf("EnforceBreaking = %q", got)
	}
	if got := NormalizeBody("feat: only subject"); got != "feat: only subject" {
		t.Fatalf("NormalizeBody subject = %q", got)
	}
	if got := AppendSignedOffBy("", "A User <a@example.com>"); got != "Signed-off-by: A User <a@example.com>" {
		t.Fatalf("AppendSignedOffBy empty = %q", got)
	}
}
