package commit

import (
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

func TestEnforceBreaking(t *testing.T) {
	got := EnforceBreaking("feat(api): add v2\n\nBody")
	want := "feat!(api): add v2\n\nBody"
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
