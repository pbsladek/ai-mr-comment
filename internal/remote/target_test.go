package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		raw  string
		kind TargetKind
	}{
		{"https://github.com/owner/repo/pull/1", TargetGitHubPR},
		{"https://gitlab.com/group/repo/-/merge_requests/2", TargetGitLabMR},
	}
	for _, tc := range tests {
		got, err := ParseTarget(tc.raw)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", tc.raw, err)
		}
		if got.URL != tc.raw || got.Kind != tc.kind {
			t.Fatalf("ParseTarget(%q) = %+v", tc.raw, got)
		}
	}
	if _, err := ParseTarget("https://example.com/owner/repo/issues/1"); err == nil {
		t.Fatal("expected unsupported target error")
	}
}

func TestFindOrCreateTargetFromRemoteGitHub(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 9, "html_url": "https://github.example/owner/repo/pull/9"})
	})
	mux.HandleFunc("/api/v3/repos/owner/repo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"default_branch": "main"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	target, err := FindOrCreateTargetFromRemote(context.Background(), Credentials{
		GitHubBaseURL: srv.URL,
	}, Info{Host: parsed.Hostname(), PathParts: []string{"owner", "repo"}}, "feat/test", "Title")
	if err != nil {
		t.Fatalf("FindOrCreateTargetFromRemote failed: %v", err)
	}
	if target.Kind != TargetGitHubPR || target.URL != "https://github.example/owner/repo/pull/9" {
		t.Fatalf("target = %+v", target)
	}
}

func TestTargetGitLabMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "MR", "description": "Body"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	target := Target{URL: srv.URL + "/group/repo/-/merge_requests/5", Kind: TargetGitLabMR}
	meta, err := target.Metadata(context.Background(), Credentials{GitLabBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("Metadata failed: %v", err)
	}
	if meta.Title != "MR" || meta.Description != "Body" {
		t.Fatalf("metadata = %+v", meta)
	}
}
