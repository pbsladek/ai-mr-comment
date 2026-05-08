package remote

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		url     string
		owner   string
		repo    string
		number  int
		wantErr bool
	}{
		{"https://github.com/org/repo/pull/1", "org", "repo", 1, false},
		{"https://github.com/org/repo/pull/1/", "org", "repo", 1, false},
		{"https://github.com/org/repo/pull/1?tab=files", "org", "repo", 1, false},
		{"https://github.myco.com/org/repo/pull/5", "org", "repo", 5, false},
		{"https://github.com/org/repo/issues/1", "", "", 0, true},
		{"https://github.com/org/repo/pull/12/files", "", "", 0, true},
		{"ssh://github.com/org/repo/pull/12", "", "", 0, true},
	}

	for _, tc := range tests {
		owner, repo, number, err := ParsePRURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParsePRURL(%q): expected error", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePRURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if owner != tc.owner || repo != tc.repo || number != tc.number {
			t.Errorf("ParsePRURL(%q) = (%s, %s, %d), want (%s, %s, %d)", tc.url, owner, repo, number, tc.owner, tc.repo, tc.number)
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
		{"https://gitlab.com/group/project/-/merge_requests/42", "group", "project", 42, false},
		{"https://gitlab.com/group/sub/project/-/merge_requests/1?tab=changes", "group/sub", "project", 1, false},
		{"https://gitlab.myco.com/ns/proj/-/merge_requests/3", "ns", "proj", 3, false},
		{"https://gitlab.com/g/p/merge_requests/1", "", "", 0, true},
		{"https://gitlab.com/project/-/merge_requests/1", "", "", 0, true},
		{"https://gitlab.com/g/p/-/merge_requests/1/changes", "", "", 0, true},
	}

	for _, tc := range tests {
		namespace, project, iid, err := ParseMRURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseMRURL(%q): expected error", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMRURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if namespace != tc.namespace || project != tc.project || iid != tc.iid {
			t.Errorf("ParseMRURL(%q) = (%s, %s, %d), want (%s, %s, %d)", tc.url, namespace, project, iid, tc.namespace, tc.project, tc.iid)
		}
	}
}

func TestResolveGitHubBaseURL(t *testing.T) {
	tests := []struct {
		name       string
		prURL      string
		configured string
		want       string
		wantErr    bool
	}{
		{"public", "https://github.com/o/r/pull/1", "", "", false},
		{"enterprise inferred", "https://github.myco.com/o/r/pull/1", "", "https://github.myco.com", false},
		{"enterprise configured", "https://github.myco.com/o/r/pull/1", "https://github.myco.com/api/v3/", "https://github.myco.com", false},
		{"mismatch", "https://github.myco.com/o/r/pull/1", "https://github.com", "", true},
	}

	for _, tc := range tests {
		got, err := ResolveGitHubBaseURL(tc.prURL, tc.configured)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got (%q, %v), want (%q, nil)", tc.name, got, err, tc.want)
		}
	}
}

func TestResolveGitLabBaseURL(t *testing.T) {
	tests := []struct {
		name       string
		mrURL      string
		configured string
		want       string
		wantErr    bool
	}{
		{"public", "https://gitlab.com/g/p/-/merge_requests/1", "", "", false},
		{"self hosted inferred", "https://gitlab.myco.com/g/p/-/merge_requests/1", "", "https://gitlab.myco.com", false},
		{"self hosted configured", "https://gitlab.myco.com/g/p/-/merge_requests/1", "https://gitlab.myco.com/api/v4/", "https://gitlab.myco.com", false},
		{"mismatch", "https://gitlab.myco.com/g/p/-/merge_requests/1", "https://gitlab.com", "", true},
	}

	for _, tc := range tests {
		got, err := ResolveGitLabBaseURL(tc.mrURL, tc.configured)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got (%q, %v), want (%q, nil)", tc.name, got, err, tc.want)
		}
	}
}

func TestCreateURL(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		branch    string
		want      string
	}{
		{"github https", "https://github.com/owner/repo.git", "feat/add-login", "https://github.com/owner/repo/compare/feat%2Fadd-login?expand=1"},
		{"github ssh", "git@github.com:owner/repo.git", "fix/auth", "https://github.com/owner/repo/compare/fix%2Fauth?expand=1"},
		{"gitlab https", "https://gitlab.com/group/project.git", "feat/new", "https://gitlab.com/group/project/-/merge_requests/new?merge_request%5Bsource_branch%5D=feat%2Fnew"},
		{"unknown", "https://bitbucket.org/owner/repo.git", "main", ""},
	}

	for _, tc := range tests {
		if got := CreateURL(tc.remoteURL, tc.branch); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseInfo(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      Info
		wantErr   bool
	}{
		{"ssh github", "git@github.com:owner/repo.git", Info{Host: "github.com", PathParts: []string{"owner", "repo"}}, false},
		{"https nested", "https://gitlab.com/group/sub/project.git", Info{Host: "gitlab.com", PathParts: []string{"group", "sub", "project"}}, false},
		{"too short", "https://github.com/onlyone", Info{}, true},
	}

	for _, tc := range tests {
		got, err := ParseInfo(tc.remoteURL)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got (%#v, %v), want (%#v, nil)", tc.name, got, err, tc.want)
		}
	}
}

func TestHostAndURLDetection(t *testing.T) {
	if !IsGitHubHost("github.com", "") || !IsGitHubHost("git.myco.com", "https://git.myco.com") {
		t.Fatal("expected GitHub host detection")
	}
	if IsGitHubHost("git.myco.com", "") {
		t.Fatal("unexpected GitHub host detection without configured base")
	}
	if !IsGitLabHost("gitlab.com", "") || !IsGitLabHost("git.myco.com", "https://git.myco.com") {
		t.Fatal("expected GitLab host detection")
	}
	if !IsGitHubURL("https://github.com/o/r/pull/1") || IsGitHubURL("https://github.com/o/r/issues/1") {
		t.Fatal("unexpected GitHub URL detection")
	}
	if !IsGitLabURL("https://gitlab.com/g/p/-/merge_requests/1") || IsGitLabURL("https://gitlab.com/g/p/-/merge_requests/1/changes") {
		t.Fatal("unexpected GitLab URL detection")
	}
}

func TestCleanStringList(t *testing.T) {
	got := CleanStringList([]string{"bug, docs", "bug", "  ", "enhancement"})
	want := []string{"bug", "docs", "enhancement"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFormatPRContent(t *testing.T) {
	got := FormatPRContent("My Title", "  Body text  ", "diff content")
	for _, want := range []string{"PR Title: My Title", "PR Description: Body text", "diff content"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestFormatPRContentEmptyBody(t *testing.T) {
	got := FormatPRContent("Title Only", "", "diff")
	if strings.Contains(got, "PR Description:") {
		t.Fatalf("expected no description header, got %q", got)
	}
}
