package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteOperationInvalidURLsAndNoOps(t *testing.T) {
	ctx := context.Background()
	gh := newRemoteTestGitHubClient(t, http.NewServeMux())
	gl := newRemoteTestGitLabClient(t, http.NewServeMux())
	badPR := "https://example.com/not-a-pr"
	badMR := "https://example.com/not-an-mr"

	githubCalls := []struct {
		name string
		err  func() error
	}{
		{"GetPRDiffWithClient", func() error { _, err := GetPRDiffWithClient(ctx, gh, badPR); return err }},
		{"PostGitHubPRCommentWithClient", func() error { return PostGitHubPRCommentWithClient(ctx, gh, badPR, "body") }},
		{"GetGitHubPRMetadataWithClient", func() error { _, err := GetGitHubPRMetadataWithClient(ctx, gh, badPR); return err }},
		{"UpdateGitHubPRMetadataWithClient", func() error { title := "title"; return UpdateGitHubPRMetadataWithClient(ctx, gh, badPR, &title, nil) }},
		{"UpsertGitHubPRCommentWithClient", func() error { return UpsertGitHubPRCommentWithClient(ctx, gh, badPR, "marker", "body") }},
	}
	for _, tc := range githubCalls {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.err(); err == nil {
				t.Fatal("expected invalid PR URL error")
			}
		})
	}

	gitlabCalls := []struct {
		name string
		err  func() error
	}{
		{"GetMRDiffWithClient", func() error { _, err := GetMRDiffWithClient(ctx, gl, badMR); return err }},
		{"PostGitLabMRNoteWithClient", func() error { return PostGitLabMRNoteWithClient(ctx, gl, badMR, "body") }},
		{"GetGitLabMRMetadataWithClient", func() error { _, err := GetGitLabMRMetadataWithClient(ctx, gl, badMR); return err }},
		{"UpdateGitLabMRMetadataWithClient", func() error { title := "title"; return UpdateGitLabMRMetadataWithClient(ctx, gl, badMR, &title, nil) }},
		{"UpsertGitLabMRNoteWithClient", func() error { return UpsertGitLabMRNoteWithClient(ctx, gl, badMR, "marker", "body") }},
	}
	for _, tc := range gitlabCalls {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.err(); err == nil {
				t.Fatal("expected invalid MR URL error")
			}
		})
	}

	if err := UpdateGitHubPRMetadataWithClient(ctx, gh, "https://github.com/owner/repo/pull/1", nil, nil); err != nil {
		t.Fatalf("GitHub nil metadata update should be a no-op: %v", err)
	}
	if err := UpdateGitLabMRMetadataWithClient(ctx, gl, "https://gitlab.com/group/repo/-/merge_requests/1", nil, nil); err != nil {
		t.Fatalf("GitLab nil metadata update should be a no-op: %v", err)
	}
	if err := AddGitHubPRLabels(ctx, "https://github.com/owner/repo/pull/1", "", "", []string{"", " , "}); err != nil {
		t.Fatalf("empty GitHub labels should be a no-op: %v", err)
	}
	if err := RequestGitHubPRReviewers(ctx, "https://github.com/owner/repo/pull/1", "", "", []string{"", " , "}); err != nil {
		t.Fatalf("empty GitHub reviewers should be a no-op: %v", err)
	}
	if err := AddGitLabMRLabels(ctx, "https://gitlab.com/group/repo/-/merge_requests/1", "", "", []string{"", " , "}); err != nil {
		t.Fatalf("empty GitLab labels should be a no-op: %v", err)
	}
	if err := RequestGitLabMRReviewers(ctx, "https://gitlab.com/group/repo/-/merge_requests/1", "", "", []string{"", " , "}); err != nil {
		t.Fatalf("empty GitLab reviewers should be a no-op: %v", err)
	}
}

func TestRemoteWrapperInvalidURLs(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		err  func() error
	}{
		{"GetPRDiff", func() error { _, err := GetPRDiff(ctx, "://bad", "", ""); return err }},
		{"PostGitHubPRComment", func() error { return PostGitHubPRComment(ctx, "://bad", "", "", "body") }},
		{"GetGitHubPRMetadata", func() error { _, err := GetGitHubPRMetadata(ctx, "://bad", "", ""); return err }},
		{"UpdateGitHubPRMetadata", func() error { title := "title"; return UpdateGitHubPRMetadata(ctx, "://bad", "", "", &title, nil) }},
		{"UpsertGitHubPRComment", func() error { return UpsertGitHubPRComment(ctx, "://bad", "", "", "marker", "body") }},
		{"AddGitHubPRLabels", func() error { return AddGitHubPRLabels(ctx, "://bad", "", "", []string{"bug"}) }},
		{"RequestGitHubPRReviewers", func() error { return RequestGitHubPRReviewers(ctx, "://bad", "", "", []string{"alice"}) }},
		{"GetMRDiff", func() error { _, err := GetMRDiff(ctx, "://bad", "", ""); return err }},
		{"PostGitLabMRNote", func() error { return PostGitLabMRNote(ctx, "://bad", "", "", "body") }},
		{"GetGitLabMRMetadata", func() error { _, err := GetGitLabMRMetadata(ctx, "://bad", "", ""); return err }},
		{"UpdateGitLabMRMetadata", func() error { title := "title"; return UpdateGitLabMRMetadata(ctx, "://bad", "", "", &title, nil) }},
		{"UpsertGitLabMRNote", func() error { return UpsertGitLabMRNote(ctx, "://bad", "", "", "marker", "body") }},
		{"AddGitLabMRLabels", func() error { return AddGitLabMRLabels(ctx, "://bad", "", "", []string{"bug"}) }},
		{"RequestGitLabMRReviewers", func() error { return RequestGitLabMRReviewers(ctx, "://bad", "", "", []string{"12"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.err(); err == nil {
				t.Fatal("expected invalid URL error")
			}
		})
	}
}

func TestGitHubOperationHTTPErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("find list error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		})
		_, err := FindOrCreateGitHubPR(ctx, newRemoteTestGitHubClient(t, mux), "owner", "repo", "branch", "Title")
		if err == nil || !strings.Contains(err.Error(), "listing GitHub PRs") {
			t.Fatalf("expected list error, got %v", err)
		}
	})

	t.Run("repo error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		})
		mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusForbidden)
		})
		_, err := FindOrCreateGitHubPR(ctx, newRemoteTestGitHubClient(t, mux), "owner", "repo", "branch", "Title")
		if err == nil || !strings.Contains(err.Error(), "getting GitHub repo info") {
			t.Fatalf("expected repo error, got %v", err)
		}
	})

	t.Run("create error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			http.Error(w, "nope", http.StatusNotFound)
		})
		mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{})
		})
		_, err := FindOrCreateGitHubPR(ctx, newRemoteTestGitHubClient(t, mux), "owner", "repo", "branch", "Title")
		if err == nil || !strings.Contains(err.Error(), "creating GitHub PR") {
			t.Fatalf("expected create error, got %v", err)
		}
	})
}

func TestGitLabOperationHTTPErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("find list error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		})
		_, err := FindOrCreateGitLabMR(ctx, newRemoteTestGitLabClient(t, mux), "group/repo", "branch", "Title")
		if err == nil || !strings.Contains(err.Error(), "listing GitLab MRs") {
			t.Fatalf("expected list error, got %v", err)
		}
	})

	t.Run("project error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		})
		mux.HandleFunc("/api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusForbidden)
		})
		_, err := FindOrCreateGitLabMR(ctx, newRemoteTestGitLabClient(t, mux), "group/repo", "branch", "Title")
		if err == nil || !strings.Contains(err.Error(), "getting GitLab project info") {
			t.Fatalf("expected project error, got %v", err)
		}
	})

	t.Run("create error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			http.Error(w, "nope", http.StatusNotFound)
		})
		mux.HandleFunc("/api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{})
		})
		_, err := FindOrCreateGitLabMR(ctx, newRemoteTestGitLabClient(t, mux), "group/repo", "branch", "Title")
		if err == nil || !strings.Contains(err.Error(), "creating GitLab MR") {
			t.Fatalf("expected create error, got %v", err)
		}
	})
}
