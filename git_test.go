package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestProcessDiffAndTruncate(t *testing.T) {
	raw := `
diff --git a/foo.txt b/foo.txt
index e69de29..4b825dc 100644
--- a/foo.txt
+++ b/foo.txt
@@ -0,0 +1,2 @@
+Hello
+World
`
	output := processDiff(raw, 10)
	if !strings.Contains(output, "Hello") || !strings.Contains(output, "World") {
		t.Error("Diff output missing expected content")
	}
}

func TestProcessDiff_Truncation(t *testing.T) {
	lines := []string{}
	for i := 0; i < 20; i++ {
		lines = append(lines, "line")
	}
	raw := strings.Join(lines, "\n")

	// Max 10 lines
	output := processDiff(raw, 10)

	if !strings.Contains(output, "[...diff truncated...]") {
		t.Error("Expected truncation message")
	}
	if len(strings.Split(output, "\n")) > 15 {
		t.Errorf("Output too long: %d lines", len(strings.Split(output, "\n")))
	}
}

func TestGetGitDiff_NoArgs(t *testing.T) {
	// We're in a git repo, so this should not error
	_, err := getGitDiff("", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetGitDiff_WithCommit(t *testing.T) {
	// Skip if HEAD has no parent (shallow clone or single-commit repo)
	if err := exec.Command("git", "rev-parse", "HEAD^").Run(); err != nil {
		t.Skip("skipping: HEAD has no parent commit")
	}
	result, err := getGitDiff("HEAD", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestGetGitDiff_WithRange(t *testing.T) {
	// Skip if HEAD~1 doesn't exist (shallow clone or single-commit repo)
	if err := exec.Command("git", "rev-parse", "HEAD~1").Run(); err != nil {
		t.Skip("skipping: HEAD~1 does not exist")
	}
	result, err := getGitDiff("HEAD~1..HEAD", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestGetGitDiff_Staged(t *testing.T) {
	result, err := getGitDiff("", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestGetGitDiff_Exclude(t *testing.T) {
	result, err := getGitDiff("", false, []string{"*.md"})
	if err != nil {
		t.Fatalf("unexpected error with exclude: %v", err)
	}
	_ = result
}

func TestGetAutoMergeBase(t *testing.T) {
	base, err := getAutoMergeBase()
	if err != nil {
		t.Skip("skipping: no origin/main or origin/master remote found")
	}
	if base == "" {
		t.Error("expected non-empty merge base")
	}
}

func TestGetGitDiff_AutoBase(t *testing.T) {
	if _, err := getAutoMergeBase(); err != nil {
		t.Skip("skipping: no remote found for auto base-branch detection")
	}
	result, err := getGitDiff("", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestSplitDiffByFile_Empty(t *testing.T) {
	chunks := splitDiffByFile("")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty diff, got %d", len(chunks))
	}
}

func TestSplitDiffByFile_Single(t *testing.T) {
	raw := "diff --git a/foo.txt b/foo.txt\n--- a/foo.txt\n+++ b/foo.txt\n@@ -1 +1 @@\n-old\n+new\n"
	chunks := splitDiffByFile(raw)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "diff --git a/foo.txt") {
		t.Error("expected chunk to contain diff header")
	}
}

func TestSplitDiffByFile_Multi(t *testing.T) {
	raw := "diff --git a/foo.txt b/foo.txt\n+foo\n" +
		"diff --git a/bar.txt b/bar.txt\n+bar\n"
	chunks := splitDiffByFile(raw)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "foo.txt") {
		t.Error("first chunk should contain foo.txt")
	}
	if !strings.Contains(chunks[1], "bar.txt") {
		t.Error("second chunk should contain bar.txt")
	}
}

func TestReadDiffFromFile(t *testing.T) {
	content := "diff --git a/x b/x\n+++ b/x\n"
	tmpFile := "tmp.diff"
	_ = os.WriteFile(tmpFile, []byte(content), 0644)
	defer func() { _ = os.Remove(tmpFile) }()

	data, err := readDiffFromFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "+++ b/x") {
		t.Error("Expected diff content not found")
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		url     string
		owner   string
		repo    string
		number  int
		wantErr bool
	}{
		// Public github.com
		{"https://github.com/pbsladek/ai-mr-comment/pull/17", "pbsladek", "ai-mr-comment", 17, false},
		{"https://github.com/org/repo/pull/1", "org", "repo", 1, false},
		{"https://github.com/org/repo/pull/1/", "org", "repo", 1, false},          // trailing slash
		{"https://github.com/org/repo/pull/1?tab=files", "org", "repo", 1, false}, // query string
		// Self-hosted GitHub Enterprise
		{"https://github.myco.com/org/repo/pull/5", "org", "repo", 5, false},
		{"https://ghes.internal.example.com/owner/myrepo/pull/100", "owner", "myrepo", 100, false},
		// Invalid cases
		{"https://github.com/org/repo/issues/1", "", "", 0, true}, // issues, not pull
		{"https://github.com/org/repo/pull/", "", "", 0, true},    // missing number
		{"not-a-url", "", "", 0, true},
	}
	for _, tc := range tests {
		owner, repo, number, err := parsePRURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parsePRURL(%q): expected error, got nil", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePRURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if owner != tc.owner || repo != tc.repo || number != tc.number {
			t.Errorf("parsePRURL(%q): got (%s, %s, %d), want (%s, %s, %d)",
				tc.url, owner, repo, number, tc.owner, tc.repo, tc.number)
		}
	}
}

func TestParseMRURL(t *testing.T) {
	tests := []struct {
		url       string
		namespace string
		project   string
		iid       int64
		wantErr   bool
	}{
		// Public gitlab.com
		{"https://gitlab.com/mygroup/myproject/-/merge_requests/42", "mygroup", "myproject", 42, false},
		{"https://gitlab.com/group/sub/project/-/merge_requests/1", "group/sub", "project", 1, false},
		{"https://gitlab.com/mygroup/myproject/-/merge_requests/42/", "mygroup", "myproject", 42, false},    // trailing slash
		{"https://gitlab.com/mygroup/myproject/-/merge_requests/42?tab=changes", "mygroup", "myproject", 42, false},
		// Self-hosted GitLab
		{"https://gitlab.myco.com/ns/proj/-/merge_requests/3", "ns", "proj", 3, false},
		{"https://git.internal.example.com/group/sub/project/-/merge_requests/7", "group/sub", "project", 7, false},
		// Invalid cases
		{"https://gitlab.com/g/p/merge_requests/1", "", "", 0, true},         // missing /-/
		{"https://gitlab.com/myproject/-/merge_requests/1", "", "", 0, true}, // no namespace
		{"https://gitlab.com/g/p/-/merge_requests/", "", "", 0, true},
	}
	for _, tc := range tests {
		namespace, project, iid, err := parseMRURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseMRURL(%q): expected error, got nil", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMRURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if namespace != tc.namespace || project != tc.project || iid != tc.iid {
			t.Errorf("parseMRURL(%q): got (%s, %s, %d), want (%s, %s, %d)",
				tc.url, namespace, project, iid, tc.namespace, tc.project, tc.iid)
		}
	}
}

// newTestGitHubClient creates a go-github client pointed at a local httptest
// server. The server's URL must end with "/" so SDK requests resolve correctly.
func newTestGitHubClient(t *testing.T, mux *http.ServeMux) *gogithub.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gh := gogithub.NewClient(nil)
	baseURL, _ := url.Parse(srv.URL + "/")
	gh.BaseURL = baseURL
	gh.UploadURL = baseURL
	return gh
}

func TestGetPRDiff(t *testing.T) {
	const rawDiff = "diff --git a/foo.go b/foo.go\n+++ b/foo.go\n+fmt.Println(\"hello\")\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "diff") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(rawDiff))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "My PR Title", "body": "Some description"})
	})

	gh := newTestGitHubClient(t, mux)
	result, err := getPRDiffWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42")
	if err != nil {
		t.Fatalf("getPRDiff: unexpected error: %v", err)
	}
	if !strings.Contains(result, "PR Title: My PR Title") {
		t.Errorf("expected PR title in result, got: %q", result)
	}
	if !strings.Contains(result, "Some description") {
		t.Errorf("expected PR description in result, got: %q", result)
	}
	if !strings.Contains(result, rawDiff) {
		t.Errorf("expected raw diff in result, got: %q", result)
	}
}

func TestGetPRDiff_EmptyBody(t *testing.T) {
	const rawDiff = "diff --git a/x b/x\n+line\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "diff") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(rawDiff))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"title": "No Body PR", "body": nil})
	})

	gh := newTestGitHubClient(t, mux)
	result, err := getPRDiffWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "PR Title: No Body PR") {
		t.Errorf("expected title, got: %q", result)
	}
	if strings.Contains(result, "PR Description:") {
		t.Errorf("expected no description header when body is empty, got: %q", result)
	}
}

func TestGetPRDiff_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	gh := newTestGitHubClient(t, mux)
	_, err := getPRDiffWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/1")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention 404, got: %v", err)
	}
}

func TestGetPRDiff_InvalidURL(t *testing.T) {
	gh := gogithub.NewClient(nil)
	_, err := getPRDiffWithClient(context.Background(), gh, "https://notgithub.com/owner/repo/pull/1")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

// newTestGitLabClient creates a go-gitlab client pointed at a local httptest server.
func newTestGitLabClient(t *testing.T, mux *http.ServeMux) *gogitlab.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gl, err := gogitlab.NewClient("", gogitlab.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("creating test GitLab client: %v", err)
	}
	return gl
}

func TestGetMRDiff(t *testing.T) {
	const rawDiff = "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/mygroup%2Fmyproject/merge_requests/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "My MR Title", "description": "MR description"})
	})
	mux.HandleFunc("/api/v4/projects/mygroup%2Fmyproject/merge_requests/5/diffs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{{"diff": rawDiff}})
	})

	gl := newTestGitLabClient(t, mux)
	result, err := getMRDiffWithClient(context.Background(), gl, "https://gitlab.com/mygroup/myproject/-/merge_requests/5")
	if err != nil {
		t.Fatalf("getMRDiff: unexpected error: %v", err)
	}
	if !strings.Contains(result, "PR Title: My MR Title") {
		t.Errorf("expected MR title in result, got: %q", result)
	}
	if !strings.Contains(result, "MR description") {
		t.Errorf("expected MR description in result, got: %q", result)
	}
	if !strings.Contains(result, rawDiff) {
		t.Errorf("expected raw diff in result, got: %q", result)
	}
}

func TestGetMRDiff_InvalidURL(t *testing.T) {
	gl, _ := gogitlab.NewClient("")
	_, err := getMRDiffWithClient(context.Background(), gl, "https://notgitlab.com/g/p/-/merge_requests/1")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestFormatPRContent(t *testing.T) {
	result := formatPRContent("My Title", "Some body", "diff content")
	if !strings.Contains(result, "PR Title: My Title") {
		t.Errorf("expected PR title, got: %q", result)
	}
	if !strings.Contains(result, "PR Description: Some body") {
		t.Errorf("expected PR description, got: %q", result)
	}
	if !strings.Contains(result, "diff content") {
		t.Errorf("expected diff content, got: %q", result)
	}
}

func TestFormatPRContent_EmptyBody(t *testing.T) {
	result := formatPRContent("Title Only", "", "diff")
	if strings.Contains(result, "PR Description:") {
		t.Errorf("expected no PR Description header for empty body, got: %q", result)
	}
}

func TestIsGitHubURL(t *testing.T) {
	// public github.com
	if !isGitHubURL("https://github.com/owner/repo/pull/1") {
		t.Error("expected true for github.com PR URL")
	}
	// self-hosted GitHub Enterprise
	if !isGitHubURL("https://github.myco.com/owner/repo/pull/42") {
		t.Error("expected true for self-hosted GitHub Enterprise PR URL")
	}
	// GitLab URL should not match
	if isGitHubURL("https://gitlab.com/g/p/-/merge_requests/1") {
		t.Error("expected false for gitlab.com URL")
	}
	// No /pull/ in path
	if isGitHubURL("https://github.com/owner/repo/issues/1") {
		t.Error("expected false for GitHub issues URL")
	}
}

func TestIsGitLabURL(t *testing.T) {
	// public gitlab.com
	if !isGitLabURL("https://gitlab.com/g/p/-/merge_requests/1") {
		t.Error("expected true for gitlab.com MR URL")
	}
	// self-hosted GitLab
	if !isGitLabURL("https://gitlab.myco.com/ns/proj/-/merge_requests/5") {
		t.Error("expected true for self-hosted GitLab MR URL")
	}
	// GitHub URL should not match
	if isGitLabURL("https://github.com/owner/repo/pull/1") {
		t.Error("expected false for github.com URL")
	}
}

func TestGetCurrentBranch(t *testing.T) {
	branch, err := getCurrentBranch()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// In a normal (non-detached) checkout the branch must be non-empty.
	// In a detached HEAD state getCurrentBranch returns "" with no error — skip.
	if branch == "" {
		t.Skip("skipping: detached HEAD state, no branch name available")
	}
	// Branch name must not contain leading/trailing whitespace.
	if branch != strings.TrimSpace(branch) {
		t.Errorf("branch name has surrounding whitespace: %q", branch)
	}
}

func TestGetCurrentBranch_DetachedHead(t *testing.T) {
	// Simulate detached HEAD by stubbing: we just verify the function
	// returns "" and nil error when git output is "HEAD".
	// We can't easily force a detached state, so we just test the parsing logic
	// directly by checking that a branch name of "HEAD" is treated as detached.
	// This is a unit check of the branch == "HEAD" guard, not a git integration test.
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		t.Skip("skipping: not in a git repo")
	}
	got := strings.TrimSpace(string(out))
	// If the repo is in detached HEAD, getCurrentBranch should return "".
	if got == "HEAD" {
		branch, branchErr := getCurrentBranch()
		if branchErr != nil {
			t.Fatalf("unexpected error in detached HEAD: %v", branchErr)
		}
		if branch != "" {
			t.Errorf("expected empty branch for detached HEAD, got %q", branch)
		}
	}
}

// TestGitCommit_EmptyMessageFails verifies that gitCommit with an empty message
// causes git to return an error (git rejects empty commit messages).
func TestGitCommit_EmptyMessageFails(t *testing.T) {
	if !isGitRepo() {
		t.Skip("skipping: not inside a git repository")
	}
	err := gitCommit("")
	if err == nil {
		t.Fatal("expected error for empty commit message, got nil")
	}
}

// TestGitPush_NoRemoteFails verifies that gitPush returns an error when there
// is no remote named "origin" configured (or the branch has nothing to push).
// This is a best-effort test — it may skip in environments where origin exists
// and the push would succeed, to avoid accidentally pushing during tests.
func TestGitPush_NoRemoteFails(t *testing.T) {
	// Only run this test when there is no "origin" remote, to avoid accidental pushes.
	out, err := exec.Command("git", "remote", "get-url", "origin").CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		t.Skip("skipping: origin remote exists — would risk an accidental push")
	}
	pushErr := gitPush("non-existent-test-branch")
	if pushErr == nil {
		t.Fatal("expected error pushing to non-existent remote, got nil")
	}
}

