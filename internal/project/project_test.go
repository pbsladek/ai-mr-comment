package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var githubActionSHAPinPattern = regexp.MustCompile(`@[0-9a-fA-F]{40}$`)

func repoPath(t *testing.T, parts ...string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return filepath.Join(append([]string{wd}, parts...)...)
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("go.mod not found above %s", wd)
		}
		wd = parent
	}
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	content, err := os.ReadFile(repoPath(t, parts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestGoLayoutBoundaries(t *testing.T) {
	root := repoPath(t)
	var rootGoFiles []string
	var mainPackages []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.Dir(rel) == "." {
			rootGoFiles = append(rootGoFiles, rel)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "\npackage main\n") || strings.HasPrefix(string(content), "package main\n") {
			mainPackages = append(mainPackages, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rootGoFiles) != 0 {
		t.Fatalf("root-level Go files are not allowed after cmd/internal migration: %v", rootGoFiles)
	}
	if len(mainPackages) != 1 || mainPackages[0] != "cmd/ai-mr-comment/main.go" {
		t.Fatalf("expected only cmd/ai-mr-comment/main.go to be package main, got %v", mainPackages)
	}
}

func TestBuildFilesReferenceCommandPackage(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	dockerfile := readRepoFile(t, "Dockerfile")
	dockerignore := readRepoFile(t, ".dockerignore")
	goreleaser := readRepoFile(t, ".goreleaser.yaml")
	installDoc := readRepoFile(t, "docs", "tool", "installation.md")

	required := map[string][]string{
		"Makefile": {
			"MAIN_PKG  := ./cmd/ai-mr-comment",
			"verify: ## Run the standard local validation gate",
			"release-check: ## Validate GoReleaser configuration",
			"release-snapshot: ## Build GoReleaser snapshot artifacts without publishing",
			"go test -fuzz=FuzzSplitDiffByFile -fuzztime=30s ./internal/app",
		},
		"Dockerfile": {
			"COPY cmd/ ./cmd/",
			"COPY internal/ ./internal/",
			"-o /out/ai-mr-comment ./cmd/ai-mr-comment",
		},
		".dockerignore": {
			"!cmd/**",
			"!internal/**",
		},
		".goreleaser.yaml": {
			"main: ./cmd/ai-mr-comment",
			"-X main.CommitFull={{.FullCommit}}",
		},
		"docs/tool/installation.md": {
			"go install github.com/pbsladek/ai-mr-comment/cmd/ai-mr-comment@latest",
		},
	}
	contents := map[string]string{
		"Makefile":                  makefile,
		"Dockerfile":                dockerfile,
		".dockerignore":             dockerignore,
		".goreleaser.yaml":          goreleaser,
		"docs/tool/installation.md": installDoc,
	}
	for file, wants := range required {
		for _, want := range wants {
			if !strings.Contains(contents[file], want) {
				t.Fatalf("%s missing %q", file, want)
			}
		}
	}
	for file, content := range contents {
		for _, stale := range []string{
			"\tgo build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .\n",
			"\tgo install $(LDFLAGS) .\n",
			"main: .\n",
			"COPY templates/ ./templates/",
			"!api.go\n",
			"!templates/\n",
			"go install github.com/pbsladek/ai-mr-comment@latest",
			"github.com/pwbsladek/ai-mr-comment",
		} {
			if strings.Contains(content, stale) {
				t.Fatalf("%s still contains stale root-layout content %q", file, stale)
			}
		}
	}
}

func TestWorkflowAndReleaseYAMLParse(t *testing.T) {
	files, err := filepath.Glob(repoPath(t, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, repoPath(t, ".goreleaser.yaml"))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err := yaml.Unmarshal(content, &parsed); err != nil {
			t.Fatalf("%s did not parse as YAML: %v", file, err)
		}
		if len(parsed) == 0 {
			t.Fatalf("%s parsed as empty YAML", file)
		}
	}
}

func TestWorkflowActionsArePinnedBySHA(t *testing.T) {
	files, err := filepath.Glob(repoPath(t, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected workflow files")
	}

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "uses:") {
				continue
			}
			ref := strings.TrimSpace(strings.TrimPrefix(trimmed, "uses:"))
			if beforeComment, _, ok := strings.Cut(ref, "#"); ok {
				ref = strings.TrimSpace(beforeComment)
			}
			ref = strings.Trim(ref, `"'`)
			if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "docker://") {
				continue
			}
			if !githubActionSHAPinPattern.MatchString(ref) {
				rel, relErr := filepath.Rel(repoPath(t), file)
				if relErr != nil {
					rel = file
				}
				t.Fatalf("%s:%d uses %q; external workflow actions must be pinned to a 40-character commit SHA", rel, i+1, ref)
			}
		}
	}
}

func TestReleaseSafetyWorkflowConfiguration(t *testing.T) {
	tagWorkflow := readRepoFile(t, ".github", "workflows", "tag.yml")
	releaseWorkflow := readRepoFile(t, ".github", "workflows", "release.yml")
	manifestScript := readRepoFile(t, ".github", "scripts", "generate-release-manifest.sh")
	verifyScript := readRepoFile(t, ".github", "scripts", "verify-release-assets.sh")
	packageJSON := readRepoFile(t, "package.json")
	packageLock := readRepoFile(t, "package-lock.json")

	required := map[string][]string{
		".github/workflows/tag.yml": {
			"validate:",
			"name: Validate Release Candidate",
			"run: make verify",
			"needs: validate",
			`node-version: "24.10.0"`,
			"run: npm ci",
			"run: npm run semantic-release -- --ci",
		},
		".github/workflows/release.yml": {
			"version: v2.15.4",
			"DOCKER_FIPS_TAGS: ${{ needs.docker_fips.outputs.docker_fips_tags }}",
			"DOCKER_FIPS_DIGEST: ${{ needs.docker_fips.outputs.docker_fips_digest }}",
			`"${REQUIRE_DOCKER_PROVENANCE}" "${REQUIRE_DOCKER_FIPS_PROVENANCE}"`,
		},
		".github/scripts/generate-release-manifest.sh": {
			"docker_fips_image=",
			"docker_fips_tags_json=",
			"docker_fips_published=false",
			"docker_fips: {",
		},
		".github/scripts/verify-release-assets.sh": {
			"require_docker_fips_provenance=",
			"ai-mr-comment_linux_amd64.sbom.json",
			".docker_fips.published == true",
			".docker_fips.digest",
		},
		"package.json": {
			`"node": "24.10.0"`,
			`"semantic-release": "25.0.3"`,
			`"@semantic-release/github": "12.0.8"`,
		},
		"package-lock.json": {
			`"semantic-release": "25.0.3"`,
			`"@semantic-release/commit-analyzer": "13.0.1"`,
			`"@semantic-release/release-notes-generator": "14.1.1"`,
			`"@semantic-release/github": "12.0.8"`,
		},
	}
	contents := map[string]string{
		".github/workflows/tag.yml":                    tagWorkflow,
		".github/workflows/release.yml":                releaseWorkflow,
		".github/scripts/generate-release-manifest.sh": manifestScript,
		".github/scripts/verify-release-assets.sh":     verifyScript,
		"package.json":      packageJSON,
		"package-lock.json": packageLock,
	}
	for file, wants := range required {
		for _, want := range wants {
			if !strings.Contains(contents[file], want) {
				t.Fatalf("%s missing %q", file, want)
			}
		}
	}
	for file, content := range contents {
		for _, stale := range []string{
			"node-version: lts/*",
			"npx --yes semantic-release",
			"version: \"~> v2\"",
		} {
			if strings.Contains(content, stale) {
				t.Fatalf("%s still contains floating release tool reference %q", file, stale)
			}
		}
	}
}
