package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestFindOrCreateTargetFromRemoteGitLab(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"iid": 3, "web_url": "https://gitlab.example/group/repo/-/merge_requests/3"})
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
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
		GitLabBaseURL: srv.URL,
	}, Info{Host: parsed.Hostname(), PathParts: []string{"group", "repo"}}, "feat/test", "Title")
	if err != nil {
		t.Fatalf("FindOrCreateTargetFromRemote failed: %v", err)
	}
	if target.Kind != TargetGitLabMR || target.URL != "https://gitlab.example/group/repo/-/merge_requests/3" {
		t.Fatalf("target = %+v", target)
	}
}

func TestFindOrCreateTargetFromRemoteErrors(t *testing.T) {
	_, err := FindOrCreateTargetFromRemote(context.Background(), Credentials{GitHubBaseURL: "https://github.example"}, Info{
		Host:      "github.example",
		PathParts: []string{"owner"},
	}, "feat/test", "Title")
	if err == nil || !strings.Contains(err.Error(), "owner/repo") {
		t.Fatalf("expected owner/repo parse error, got %v", err)
	}

	_, err = FindOrCreateTargetFromRemote(context.Background(), Credentials{}, Info{
		Host:      "example.com",
		PathParts: []string{"owner", "repo"},
	}, "feat/test", "Title")
	if err == nil || !strings.Contains(err.Error(), "unrecognised remote host") {
		t.Fatalf("expected unknown host error, got %v", err)
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

func TestTargetGitHubOperations(t *testing.T) {
	var labels []string
	var reviewers []string
	var posted string
	var updatedComment string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/v3")
		switch path {
		case "/repos/owner/repo/pulls/1":
			if strings.Contains(r.Header.Get("Accept"), "diff") {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("diff --git a/a.go b/a.go\n+one\n"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"title": "Title", "body": "Body"})
		case "/repos/owner/repo/issues/1/comments":
			switch r.Method {
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 5, "body": "<!-- marker --> old"}})
			case http.MethodPost:
				var payload struct {
					Body string `json:"body"`
				}
				_ = json.NewDecoder(r.Body).Decode(&payload)
				posted = payload.Body
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 6, "body": payload.Body})
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		case "/repos/owner/repo/issues/comments/5":
			var payload struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			updatedComment = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 5, "body": payload.Body})
		case "/repos/owner/repo/issues/1/labels":
			_ = json.NewDecoder(r.Body).Decode(&labels)
			_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "bug"}})
		case "/repos/owner/repo/pulls/1/requested_reviewers":
			var payload struct {
				Reviewers []string `json:"reviewers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			reviewers = payload.Reviewers
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 1})
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	target := Target{URL: srv.URL + "/owner/repo/pull/1", Kind: TargetGitHubPR}
	creds := Credentials{GitHubToken: "token", GitHubBaseURL: srv.URL}
	if diff, err := target.Diff(context.Background(), creds); err != nil || !strings.Contains(diff, "diff --git") {
		t.Fatalf("Diff = %q, %v", diff, err)
	}
	if meta, err := target.Metadata(context.Background(), creds); err != nil || meta.Title != "Title" {
		t.Fatalf("Metadata = %+v, %v", meta, err)
	}
	title := "Updated"
	body := "Updated body"
	if err := target.UpdateMetadata(context.Background(), creds, &title, &body); err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}
	if err := target.PostComment(context.Background(), creds, "comment body"); err != nil {
		t.Fatalf("PostComment failed: %v", err)
	}
	if posted != "comment body" {
		t.Fatalf("posted = %q", posted)
	}
	if err := target.UpsertManagedComment(context.Background(), creds, "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertManagedComment failed: %v", err)
	}
	if updatedComment != "replacement" {
		t.Fatalf("updated comment = %q", updatedComment)
	}
	if err := target.AddLabels(context.Background(), creds, []string{"bug,docs", "bug"}); err != nil {
		t.Fatalf("AddLabels failed: %v", err)
	}
	if strings.Join(labels, ",") != "bug,docs" {
		t.Fatalf("labels = %v", labels)
	}
	if err := target.RequestReviewers(context.Background(), creds, []string{"alice", "bob"}); err != nil {
		t.Fatalf("RequestReviewers failed: %v", err)
	}
	if strings.Join(reviewers, ",") != "alice,bob" {
		t.Fatalf("reviewers = %v", reviewers)
	}
}

func TestTargetGitLabOperations(t *testing.T) {
	var labels []string
	var reviewerIDs []int64
	var posted string
	var updatedNote string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			var payload struct {
				AddLabels   json.RawMessage `json:"add_labels"`
				ReviewerIDs *[]int64        `json:"reviewer_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if len(payload.AddLabels) > 0 {
				var asList []string
				if err := json.Unmarshal(payload.AddLabels, &asList); err == nil {
					labels = asList
				} else {
					var asString string
					_ = json.Unmarshal(payload.AddLabels, &asString)
					labels = strings.Split(asString, ",")
				}
			}
			if payload.ReviewerIDs != nil {
				reviewerIDs = *payload.ReviewerIDs
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"title": "MR", "description": "Body"})
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/diffs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{{"diff": "diff --git a/a.go b/a.go\n+one\n"}})
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/notes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "body": "<!-- marker --> old"}})
		case http.MethodPost:
			var payload struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			posted = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 8, "body": payload.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/notes/7", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		updatedNote = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "body": payload.Body})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	target := Target{URL: srv.URL + "/group/repo/-/merge_requests/5", Kind: TargetGitLabMR}
	creds := Credentials{GitLabToken: "token", GitLabBaseURL: srv.URL}
	if diff, err := target.Diff(context.Background(), creds); err != nil || !strings.Contains(diff, "diff --git") {
		t.Fatalf("Diff = %q, %v", diff, err)
	}
	if meta, err := target.Metadata(context.Background(), creds); err != nil || meta.Title != "MR" {
		t.Fatalf("Metadata = %+v, %v", meta, err)
	}
	title := "Updated"
	description := "Updated body"
	if err := target.UpdateMetadata(context.Background(), creds, &title, &description); err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}
	if err := target.PostComment(context.Background(), creds, "note body"); err != nil {
		t.Fatalf("PostComment failed: %v", err)
	}
	if posted != "note body" {
		t.Fatalf("posted = %q", posted)
	}
	if err := target.UpsertManagedComment(context.Background(), creds, "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertManagedComment failed: %v", err)
	}
	if updatedNote != "replacement" {
		t.Fatalf("updated note = %q", updatedNote)
	}
	if err := target.AddLabels(context.Background(), creds, []string{"bug,docs", "bug"}); err != nil {
		t.Fatalf("AddLabels failed: %v", err)
	}
	if strings.Join(labels, ",") != "bug,docs" {
		t.Fatalf("labels = %v", labels)
	}
	if err := target.RequestReviewers(context.Background(), creds, []string{"12", "34"}); err != nil {
		t.Fatalf("RequestReviewers failed: %v", err)
	}
	if len(reviewerIDs) != 2 || reviewerIDs[0] != 12 || reviewerIDs[1] != 34 {
		t.Fatalf("reviewer IDs = %v", reviewerIDs)
	}
}

func TestTargetUnsupportedKind(t *testing.T) {
	target := Target{URL: "https://example.com/change/1", Kind: TargetKind("unknown")}
	creds := Credentials{}
	ctx := context.Background()

	if _, err := target.Diff(ctx, creds); err == nil {
		t.Fatal("expected Diff error")
	}
	if _, err := target.Metadata(ctx, creds); err == nil {
		t.Fatal("expected Metadata error")
	}
	if err := target.UpdateMetadata(ctx, creds, nil, nil); err == nil {
		t.Fatal("expected UpdateMetadata error")
	}
	if err := target.PostComment(ctx, creds, "body"); err == nil {
		t.Fatal("expected PostComment error")
	}
	if err := target.UpsertManagedComment(ctx, creds, "marker", "body"); err == nil {
		t.Fatal("expected UpsertManagedComment error")
	}
	if err := target.AddLabels(ctx, creds, []string{"bug"}); err == nil {
		t.Fatal("expected AddLabels error")
	}
	if err := target.RequestReviewers(ctx, creds, []string{"alice"}); err == nil {
		t.Fatal("expected RequestReviewers error")
	}
}
