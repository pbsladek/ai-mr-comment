//go:build e2e

package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestE2ESmoke_BinaryCommands(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "ai-mr-comment")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, buildErr := exec.Command("go", "build", "-o", bin, "./cmd/ai-mr-comment").CombinedOutput(); buildErr != nil {
		t.Fatalf("go build failed: %v\n%s", buildErr, out)
	}

	fakeOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "resp_smoke",
			"object":     "response",
			"created_at": 0,
			"status":     "completed",
			"model":      "test",
			"output": []map[string]any{
				{
					"id":     "msg_smoke",
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"content": []map[string]any{
						{
							"type":        "output_text",
							"text":        "feat: smoke generated message",
							"annotations": []any{},
						},
					},
				},
			},
		})
	}))
	defer fakeOpenAI.Close()

	baseEnv := append(os.Environ(),
		"HOME="+t.TempDir(),
		"OPENAI_API_KEY=dummy",
		"AI_MR_COMMENT_OPENAI_ENDPOINT="+fakeOpenAI.URL+"/v1/",
	)
	run := func(dir string, stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = baseEnv
		cmd.Stdin = strings.NewReader(stdin)
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("%s %s failed: %v\n%s", bin, strings.Join(args, " "), runErr, out)
		}
		return string(out)
	}

	if out := run(repoRoot, "", "--version"); !strings.Contains(out, "repo=https://github.com/pbsladek/ai-mr-comment") {
		t.Fatalf("unexpected --version output: %s", out)
	}

	if out := run(repoRoot, "", "models", "--provider=openai"); !strings.Contains(out, "gpt-5.5") {
		t.Fatalf("expected OpenAI model smoke output to include gpt-5.5, got:\n%s", out)
	}

	requestOut := run(repoRoot, "diff --git a/smoke.go b/smoke.go\n+smoke\n", "--print-request", "--file=-", "--provider=openai")
	var requestPayload struct {
		Provider   string `json:"provider"`
		DiffSource string `json:"diff_source"`
		Diff       string `json:"diff"`
	}
	if err := json.Unmarshal([]byte(requestOut), &requestPayload); err != nil {
		t.Fatalf("invalid --print-request JSON: %v\n%s", err, requestOut)
	}
	if requestPayload.Provider != "openai" || requestPayload.DiffSource != "stdin" || !strings.Contains(requestPayload.Diff, "+smoke") {
		t.Fatalf("unexpected --print-request payload: %+v", requestPayload)
	}

	commitOut := run(repoRoot, `{"branch":"feat/E2E-1","diff":"diff --git a/a.go b/a.go\n+binary smoke\n"}`, "commit-message", "--input=json", "--provider=openai")
	if strings.TrimSpace(commitOut) != "feat: smoke generated message" {
		t.Fatalf("unexpected commit-message output: %q", commitOut)
	}

	smokeRepo := initSmokeGitRepo(t)
	quickOut := run(smokeRepo, "", "quick-commit", "--dry-run", "--format=json", "--provider=openai")
	var quickPayload struct {
		CommitMessage string `json:"commit_message"`
		Provider      string `json:"provider"`
	}
	if err := json.Unmarshal([]byte(quickOut), &quickPayload); err != nil {
		t.Fatalf("invalid quick-commit JSON: %v\n%s", err, quickOut)
	}
	if quickPayload.CommitMessage != "feat: smoke generated message" || quickPayload.Provider != "openai" {
		t.Fatalf("unexpected quick-commit payload: %+v", quickPayload)
	}
}

func initSmokeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "config", "user.email", "smoke@example.com"},
		{"-C", dir, "config", "user.name", "Smoke Test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", dir, "add", "README.md"},
		{"-C", dir, "commit", "-m", "initial"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(readme, []byte(fmt.Sprintf("after %s\n", t.Name())), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
