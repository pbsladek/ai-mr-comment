package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
)

func newRemoteTestGitHubClient(t *testing.T, mux *http.ServeMux) *gogithub.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gh := gogithub.NewClient(nil)
	baseURL, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	gh.BaseURL = baseURL
	gh.UploadURL = baseURL
	return gh
}

func newRemoteTestGitLabClient(t *testing.T, mux *http.ServeMux) *gogitlab.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gl, err := gogitlab.NewClient("", gogitlab.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("creating test GitLab client: %v", err)
	}
	return gl
}

func TestGitHubOperationsWithClient(t *testing.T) {
	const rawDiff = "diff --git a/foo.go b/foo.go\n+++ b/foo.go\n+fmt.Println(\"hello\")\n"
	var postedComment string
	var editedTitle string
	var editedBody string
	var upsertedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept"), "diff") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(rawDiff))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"title": "My PR", "body": "Body"})
		case http.MethodPatch:
			var payload struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			editedTitle = payload.Title
			editedBody = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 42, "title": payload.Title, "body": payload.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 99, "body": "<!-- marker -->\nold"}})
		case http.MethodPost:
			var payload struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			postedComment = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 100, "body": payload.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/repos/owner/repo/issues/comments/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		upsertedBody = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "body": payload.Body})
	})

	gh := newRemoteTestGitHubClient(t, mux)
	diff, err := GetPRDiffWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42")
	if err != nil {
		t.Fatalf("GetPRDiffWithClient failed: %v", err)
	}
	for _, want := range []string{"PR Title: My PR", "PR Description: Body", rawDiff} {
		if !strings.Contains(diff, want) {
			t.Fatalf("expected %q in diff content:\n%s", want, diff)
		}
	}
	meta, err := GetGitHubPRMetadataWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42")
	if err != nil || meta.Title != "My PR" || meta.Description != "Body" {
		t.Fatalf("metadata = %+v, %v", meta, err)
	}
	if err := PostGitHubPRCommentWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42", "new comment"); err != nil {
		t.Fatalf("PostGitHubPRCommentWithClient failed: %v", err)
	}
	if postedComment != "new comment" {
		t.Fatalf("posted comment = %q", postedComment)
	}
	title := "New title"
	body := "New body"
	if err := UpdateGitHubPRMetadataWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42", &title, &body); err != nil {
		t.Fatalf("UpdateGitHubPRMetadataWithClient failed: %v", err)
	}
	if editedTitle != title || editedBody != body {
		t.Fatalf("edited title/body = %q/%q", editedTitle, editedBody)
	}
	if err := UpsertGitHubPRCommentWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42", "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertGitHubPRCommentWithClient failed: %v", err)
	}
	if upsertedBody != "replacement" {
		t.Fatalf("upserted body = %q", upsertedBody)
	}
}

func TestGitHubUpsertCommentChecksAllPages(t *testing.T) {
	var updatedBody string
	var postedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("page") == "2" {
				_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 123, "body": "<!-- marker -->\nold"}})
				return
			}
			w.Header().Set("Link", `</repos/owner/repo/issues/42/comments?page=2>; rel="next"`)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "body": "unmanaged"}})
		case http.MethodPost:
			var payload struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			postedBody = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "body": payload.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/repos/owner/repo/issues/comments/123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		updatedBody = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 123, "body": payload.Body})
	})

	gh := newRemoteTestGitHubClient(t, mux)
	if err := UpsertGitHubPRCommentWithClient(context.Background(), gh, "https://github.com/owner/repo/pull/42", "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertGitHubPRCommentWithClient failed: %v", err)
	}
	if updatedBody != "replacement" {
		t.Fatalf("expected page-2 comment update, got %q", updatedBody)
	}
	if postedBody != "" {
		t.Fatalf("expected no duplicate post, got %q", postedBody)
	}
}

func TestGitHubFindOrCreatePR(t *testing.T) {
	t.Run("finds existing", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{"number": 7, "html_url": "https://github.com/owner/repo/pull/7"}})
		})
		got, err := FindOrCreateGitHubPR(context.Background(), newRemoteTestGitHubClient(t, mux), "owner", "repo", "feat/branch", "Title")
		if err != nil || got != "https://github.com/owner/repo/pull/7" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("creates new", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 8, "html_url": "https://github.com/owner/repo/pull/8"})
		})
		mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": "main"})
		})
		got, err := FindOrCreateGitHubPR(context.Background(), newRemoteTestGitHubClient(t, mux), "owner", "repo", "feat/new", "Title")
		if err != nil || got != "https://github.com/owner/repo/pull/8" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestGitLabOperationsWithClient(t *testing.T) {
	var postedNote string
	var updatedTitle string
	var updatedDescription string
	var upsertedNote string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"title": "MR title", "description": "MR body"})
		case http.MethodPut:
			var payload struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			updatedTitle = payload.Title
			updatedDescription = payload.Description
			_ = json.NewEncoder(w).Encode(map[string]any{"iid": 5, "title": payload.Title, "description": payload.Description})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/diffs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("X-Next-Page", "2")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"old_path": "old name.go",
				"new_path": "new name.go",
				"diff":     "@@ -1 +1 @@\n-old\n+new\n",
			}})
		case "2":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"diff": "diff --git a/two.go b/two.go\n+two\n"}})
		default:
			http.Error(w, "bad page", http.StatusBadRequest)
		}
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/notes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 11, "body": "<!-- marker -->\nold"}})
		case http.MethodPost:
			var payload struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			postedNote = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12, "body": payload.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/notes/11", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		upsertedNote = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 11, "body": payload.Body})
	})

	gl := newRemoteTestGitLabClient(t, mux)
	targetURL := "https://gitlab.com/group/repo/-/merge_requests/5"
	diff, err := GetMRDiffWithClient(context.Background(), gl, targetURL)
	if err != nil {
		t.Fatalf("GetMRDiffWithClient failed: %v", err)
	}
	for _, want := range []string{"PR Title: MR title", `diff --git "a/old name.go" "b/new name.go"`, "two.go"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("expected %q in MR diff:\n%s", want, diff)
		}
	}
	meta, err := GetGitLabMRMetadataWithClient(context.Background(), gl, targetURL)
	if err != nil || meta.Title != "MR title" || meta.Description != "MR body" {
		t.Fatalf("metadata = %+v, %v", meta, err)
	}
	title := "Updated title"
	description := "Updated body"
	if err := UpdateGitLabMRMetadataWithClient(context.Background(), gl, targetURL, &title, &description); err != nil {
		t.Fatalf("UpdateGitLabMRMetadataWithClient failed: %v", err)
	}
	if updatedTitle != title || updatedDescription != description {
		t.Fatalf("updated title/body = %q/%q", updatedTitle, updatedDescription)
	}
	if err := PostGitLabMRNoteWithClient(context.Background(), gl, targetURL, "new note"); err != nil {
		t.Fatalf("PostGitLabMRNoteWithClient failed: %v", err)
	}
	if postedNote != "new note" {
		t.Fatalf("posted note = %q", postedNote)
	}
	if err := UpsertGitLabMRNoteWithClient(context.Background(), gl, targetURL, "<!-- marker -->", "replacement note"); err != nil {
		t.Fatalf("UpsertGitLabMRNoteWithClient failed: %v", err)
	}
	if upsertedNote != "replacement note" {
		t.Fatalf("upserted note = %q", upsertedNote)
	}
}

func TestGitLabUpsertNoteChecksAllPages(t *testing.T) {
	var updatedNote string
	var postedNote string
	targetURL := "https://gitlab.com/group/repo/-/merge_requests/5"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/notes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("page") == "2" {
				_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 88, "body": "<!-- marker -->\nold"}})
				return
			}
			w.Header().Set("X-Next-Page", "2")
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "body": "unmanaged"}})
		case http.MethodPost:
			var payload struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			postedNote = payload.Body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 999, "body": payload.Body})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/5/notes/88", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		updatedNote = payload.Body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 88, "body": payload.Body})
	})

	gl := newRemoteTestGitLabClient(t, mux)
	if err := UpsertGitLabMRNoteWithClient(context.Background(), gl, targetURL, "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertGitLabMRNoteWithClient failed: %v", err)
	}
	if updatedNote != "replacement" {
		t.Fatalf("expected page-2 note update, got %q", updatedNote)
	}
	if postedNote != "" {
		t.Fatalf("expected no duplicate post, got %q", postedNote)
	}
}

func TestGitLabFindOrCreateMR(t *testing.T) {
	t.Run("finds existing", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{"iid": 3, "web_url": "https://gitlab.com/group/repo/-/merge_requests/3"}})
		})
		got, err := FindOrCreateGitLabMR(context.Background(), newRemoteTestGitLabClient(t, mux), "group/repo", "feat/branch", "Title")
		if err != nil || got != "https://gitlab.com/group/repo/-/merge_requests/3" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("creates new", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"iid": 4, "web_url": "https://gitlab.com/group/repo/-/merge_requests/4"})
		})
		mux.HandleFunc("/api/v4/projects/group%2Frepo", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": "main"})
		})
		got, err := FindOrCreateGitLabMR(context.Background(), newRemoteTestGitLabClient(t, mux), "group/repo", "feat/new", "Title")
		if err != nil || got != "https://gitlab.com/group/repo/-/merge_requests/4" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestRemoteAuthErrorWrappingAndFormatting(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/resource", nil)
	if err != nil {
		t.Fatal(err)
	}
	ghErr := wrapGitHubAuthError("listing GitHub PRs", &gogithub.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound, Request: req}})
	if !strings.Contains(ghErr.Error(), "set GITHUB_TOKEN") {
		t.Fatalf("expected GitHub auth guidance, got %v", ghErr)
	}
	glErr := wrapGitLabAuthError("listing GitLab MRs", &gogitlab.ErrorResponse{Response: &http.Response{StatusCode: http.StatusForbidden, Request: req}})
	if !strings.Contains(glErr.Error(), "set GITLAB_TOKEN") {
		t.Fatalf("expected GitLab auth guidance, got %v", glErr)
	}
	if got := formatGitLabMRDiff(nil); got != "" {
		t.Fatalf("nil GitLab diff = %q", got)
	}
	if got := formatGitLabMRDiff(&gogitlab.MergeRequestDiff{NewPath: "added.go", NewFile: true, Diff: "+new"}); !strings.Contains(got, "--- /dev/null") || !strings.HasSuffix(got, "+new\n") {
		t.Fatalf("unexpected new-file diff:\n%s", got)
	}
	if got := formatGitLabMRDiff(&gogitlab.MergeRequestDiff{OldPath: "deleted.go", DeletedFile: true, Diff: "-old"}); !strings.Contains(got, "+++ /dev/null") || !strings.HasSuffix(got, "-old\n") {
		t.Fatalf("unexpected deleted-file diff:\n%s", got)
	}
}

func TestGitHubRemoteWrapperOperations(t *testing.T) {
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
	defer srv.Close()
	prURL := srv.URL + "/owner/repo/pull/1"

	if diff, err := GetPRDiff(context.Background(), prURL, "token", srv.URL); err != nil || !strings.Contains(diff, "diff --git") {
		t.Fatalf("GetPRDiff = %q, %v", diff, err)
	}
	if meta, err := GetGitHubPRMetadata(context.Background(), prURL, "token", srv.URL); err != nil || meta.Title != "Title" {
		t.Fatalf("GetGitHubPRMetadata = %+v, %v", meta, err)
	}
	title := "Updated"
	body := "Updated body"
	if err := UpdateGitHubPRMetadata(context.Background(), prURL, "token", srv.URL, &title, &body); err != nil {
		t.Fatalf("UpdateGitHubPRMetadata failed: %v", err)
	}
	if err := PostGitHubPRComment(context.Background(), prURL, "token", srv.URL, "comment body"); err != nil {
		t.Fatalf("PostGitHubPRComment failed: %v", err)
	}
	if posted != "comment body" {
		t.Fatalf("posted = %q", posted)
	}
	if err := UpsertGitHubPRComment(context.Background(), prURL, "token", srv.URL, "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertGitHubPRComment failed: %v", err)
	}
	if updatedComment != "replacement" {
		t.Fatalf("updated comment = %q", updatedComment)
	}
	if err := AddGitHubPRLabels(context.Background(), prURL, "token", srv.URL, []string{"bug,docs", "bug"}); err != nil {
		t.Fatalf("AddGitHubPRLabels failed: %v", err)
	}
	if strings.Join(labels, ",") != "bug,docs" {
		t.Fatalf("labels = %v", labels)
	}
	if err := RequestGitHubPRReviewers(context.Background(), prURL, "token", srv.URL, []string{"alice", "bob"}); err != nil {
		t.Fatalf("RequestGitHubPRReviewers failed: %v", err)
	}
	if strings.Join(reviewers, ",") != "alice,bob" {
		t.Fatalf("reviewers = %v", reviewers)
	}
}

func TestGitLabRemoteWrapperOperations(t *testing.T) {
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
	defer srv.Close()
	mrURL := srv.URL + "/group/repo/-/merge_requests/5"

	if diff, err := GetMRDiff(context.Background(), mrURL, "token", srv.URL); err != nil || !strings.Contains(diff, "diff --git") {
		t.Fatalf("GetMRDiff = %q, %v", diff, err)
	}
	if meta, err := GetGitLabMRMetadata(context.Background(), mrURL, "token", srv.URL); err != nil || meta.Title != "MR" {
		t.Fatalf("GetGitLabMRMetadata = %+v, %v", meta, err)
	}
	title := "Updated"
	description := "Updated body"
	if err := UpdateGitLabMRMetadata(context.Background(), mrURL, "token", srv.URL, &title, &description); err != nil {
		t.Fatalf("UpdateGitLabMRMetadata failed: %v", err)
	}
	if err := PostGitLabMRNote(context.Background(), mrURL, "token", srv.URL, "note body"); err != nil {
		t.Fatalf("PostGitLabMRNote failed: %v", err)
	}
	if posted != "note body" {
		t.Fatalf("posted = %q", posted)
	}
	if err := UpsertGitLabMRNote(context.Background(), mrURL, "token", srv.URL, "<!-- marker -->", "replacement"); err != nil {
		t.Fatalf("UpsertGitLabMRNote failed: %v", err)
	}
	if updatedNote != "replacement" {
		t.Fatalf("updated note = %q", updatedNote)
	}
	if err := AddGitLabMRLabels(context.Background(), mrURL, "token", srv.URL, []string{"bug,docs", "bug"}); err != nil {
		t.Fatalf("AddGitLabMRLabels failed: %v", err)
	}
	if strings.Join(labels, ",") != "bug,docs" {
		t.Fatalf("labels = %v", labels)
	}
	if err := RequestGitLabMRReviewers(context.Background(), mrURL, "token", srv.URL, []string{"12", "34"}); err != nil {
		t.Fatalf("RequestGitLabMRReviewers failed: %v", err)
	}
	if len(reviewerIDs) != 2 || reviewerIDs[0] != 12 || reviewerIDs[1] != 34 {
		t.Fatalf("reviewer IDs = %v", reviewerIDs)
	}
	if err := RequestGitLabMRReviewers(context.Background(), mrURL, "token", srv.URL, []string{"alice"}); err == nil || !strings.Contains(err.Error(), "numeric user ID") {
		t.Fatalf("expected invalid reviewer error, got %v", err)
	}
}
