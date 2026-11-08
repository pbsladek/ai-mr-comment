package app

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestDockerfilePinsHardenedBaseImagesAndPackageUpgrades(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")
	for _, pattern := range []string{
		`(?m)^FROM dhi\.io/golang:[0-9]+\.[0-9]+-debian[0-9]+-dev@sha256:[0-9a-f]{64} AS builder$`,
		`(?m)^FROM dhi\.io/debian-base:[a-z]+-debian[0-9]+-dev@sha256:[0-9a-f]{64}$`,
	} {
		if !regexp.MustCompile(pattern).MatchString(dockerfile) {
			t.Fatalf("Dockerfile is missing a pinned hardened base image matching %s", pattern)
		}
	}
	for _, want := range []string{"DEBIAN_FRONTEND=noninteractive", `Dpkg::Options::="--force-confdef"`, `Dpkg::Options::="--force-confold"`, "upgrade -y --no-install-recommends"} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
}

func TestDockerScoutTargetsGateFixableHighCriticalCVEs(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")

	required := []string{
		"docker-scout: docker-build ## Scan Docker image for fixable critical/high CVEs",
		"docker-scout-fips: docker-build-fips ## Scan FIPS Docker image for fixable critical/high CVEs",
		"docker scout cves --only-fixed --only-severity critical,high --exit-code local://$(DOCKER_IMAGE):$(DOCKER_TAG)",
		"docker scout cves --only-fixed --only-severity critical,high --exit-code local://$(DOCKER_IMAGE):$(DOCKER_TAG)-fips",
	}
	for _, want := range required {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing %q", want)
		}
	}
}

func TestReleaseWorkflowScansImagesBeforePublish(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/release.yml")

	required := []string{
		"platforms: linux/amd64,linux/arm64",
		"Build Docker amd64 image for vulnerability scan",
		"Build Docker arm64 image for vulnerability scan",
		"Build Docker FIPS amd64 image for vulnerability scan",
		"Build Docker FIPS arm64 image for vulnerability scan",
		"uses: docker/scout-action@",
		"image: local://pwbsladek/ai-mr-comment:scan-amd64",
		"image: local://pwbsladek/ai-mr-comment:scan-arm64",
		"image: local://pwbsladek/ai-mr-comment:scan-fips-amd64",
		"image: local://pwbsladek/ai-mr-comment:scan-fips-arm64",
		"only-fixed: true",
		"only-severities: critical,high",
		"exit-code: true",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}

	assertBefore(t, workflow, "Scan Docker amd64 image vulnerabilities", "Build and push Docker image")
	assertBefore(t, workflow, "Scan Docker arm64 image vulnerabilities", "Build and push Docker image")
	assertBefore(t, workflow, "Scan Docker FIPS amd64 image vulnerabilities", "Build and push Docker FIPS image")
	assertBefore(t, workflow, "Scan Docker FIPS arm64 image vulnerabilities", "Build and push Docker FIPS image")
}

func TestReleaseArtifactsCoverOSAndArchitectureMatrix(t *testing.T) {
	goreleaser := readRepoFile(t, ".goreleaser.yaml")
	makefile := readRepoFile(t, "Makefile")
	verifyScript := readRepoFile(t, ".github/scripts/verify-release-assets.sh")

	for _, want := range []string{"- linux", "- windows", "- darwin", "- amd64", "- arm64"} {
		if !strings.Contains(goreleaser, want) {
			t.Fatalf(".goreleaser.yaml missing %q", want)
		}
	}

	if !strings.Contains(makefile, "PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64") {
		t.Fatal("Makefile release matrix must include linux, darwin, and windows for amd64 and arm64")
	}

	requiredArchives := []string{
		"ai-mr-comment_Linux_x86_64.tar.gz",
		"ai-mr-comment_Linux_arm64.tar.gz",
		"ai-mr-comment_Darwin_x86_64.tar.gz",
		"ai-mr-comment_Darwin_arm64.tar.gz",
		"ai-mr-comment_Windows_x86_64.zip",
		"ai-mr-comment_Windows_arm64.zip",
		"ai-mr-comment_linux_amd64.sbom.json",
		"ai-mr-comment_linux_arm64.sbom.json",
		"ai-mr-comment_darwin_amd64.sbom.json",
		"ai-mr-comment_darwin_arm64.sbom.json",
		"ai-mr-comment_windows_amd64.sbom.json",
		"ai-mr-comment_windows_arm64.sbom.json",
	}
	for _, want := range requiredArchives {
		if !strings.Contains(verifyScript, want) {
			t.Fatalf("release asset verifier missing %q", want)
		}
	}
	if strings.Contains(verifyScript, "at least 4 build archives") {
		t.Fatal("release asset verifier still allows the old four-archive minimum")
	}
}

func TestGoReleaserBuildsCommandPackageWithFullMetadata(t *testing.T) {
	goreleaser := readRepoFile(t, ".goreleaser.yaml")

	required := []string{
		"main: ./cmd/ai-mr-comment",
		"-X main.Commit={{.ShortCommit}}",
		"-X main.CommitFull={{.FullCommit}}",
	}
	for _, want := range required {
		if !strings.Contains(goreleaser, want) {
			t.Fatalf(".goreleaser.yaml missing %q", want)
		}
	}
	if strings.Contains(goreleaser, "-X main.Commit={{.Commit}}") {
		t.Fatal(".goreleaser.yaml still uses deprecated .Commit template field")
	}
}

func TestSourceInstallDocsUseCommandPackage(t *testing.T) {
	installDoc := readRepoFile(t, "docs/tool/installation.md")

	want := "go install github.com/pbsladek/ai-mr-comment/cmd/ai-mr-comment@latest"
	if !strings.Contains(installDoc, want) {
		t.Fatalf("installation docs missing %q", want)
	}
	if strings.Contains(installDoc, "github.com/pwbsladek/ai-mr-comment") {
		t.Fatal("installation docs still reference misspelled module path")
	}
	if strings.Contains(installDoc, "go install github.com/pbsladek/ai-mr-comment@latest") {
		t.Fatal("installation docs still reference unsupported root package install")
	}
}

func TestWorkflowAndMakefileUseCommandPackageLayout(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	testWorkflow := readRepoFile(t, ".github/workflows/test.yml")
	releaseWorkflow := readRepoFile(t, ".github/workflows/release.yml")
	goreleaser := readRepoFile(t, ".goreleaser.yaml")
	dockerfile := readRepoFile(t, "Dockerfile")
	dockerignore := readRepoFile(t, ".dockerignore")

	required := map[string][]string{
		"Makefile": {
			"MAIN_PKG  := ./cmd/ai-mr-comment",
			"go build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) $(MAIN_PKG)",
			"go install $(LDFLAGS) $(MAIN_PKG)",
			"go test -fuzz=FuzzSplitDiffByFile -fuzztime=30s ./internal/app",
		},
		".github/workflows/test.yml": {
			"run: make test",
			"run: make test-e2e-smoke",
			"#   run: make test-fuzz",
		},
		".github/workflows/release.yml": {
			"file: Dockerfile",
			"COMMIT_FULL=${{ needs.validate.outputs.tag_commit }}",
		},
		".goreleaser.yaml": {
			"main: ./cmd/ai-mr-comment",
		},
		"Dockerfile": {
			"COPY cmd/ ./cmd/",
			"COPY internal/ ./internal/",
			"-o /out/ai-mr-comment ./cmd/ai-mr-comment",
		},
		".dockerignore": {
			"!cmd/",
			"!cmd/**",
			"!internal/",
			"!internal/**",
		},
	}
	contents := map[string]string{
		"Makefile":                      makefile,
		".github/workflows/test.yml":    testWorkflow,
		".github/workflows/release.yml": releaseWorkflow,
		".goreleaser.yaml":              goreleaser,
		"Dockerfile":                    dockerfile,
		".dockerignore":                 dockerignore,
	}
	for file, wants := range required {
		for _, want := range wants {
			if !strings.Contains(contents[file], want) {
				t.Fatalf("%s missing %q", file, want)
			}
		}
	}

	staleRootCommands := []string{
		"\tgo build $(LDFLAGS) -o $(BUILD_DIR)/$(APP) .\n",
		"\tgo install $(LDFLAGS) .\n",
		"\tgo test -fuzz=FuzzSplitDiffByFile -fuzztime=30s .\n",
		"\tgo test -fuzz=FuzzProcessDiff -fuzztime=30s .\n",
		"\tgo test -fuzz=FuzzEstimateCost -fuzztime=30s .\n",
		"main: .\n",
		"COPY templates/ ./templates/",
		"!api.go\n",
		"!main.go\n",
		"!templates/\n",
	}
	for _, stale := range staleRootCommands {
		for file, content := range contents {
			if strings.Contains(content, stale) {
				t.Fatalf("%s still contains stale root-layout command %q", file, stale)
			}
		}
	}
}

func assertBefore(t *testing.T, haystack, first, second string) {
	t.Helper()
	firstIndex := strings.Index(haystack, first)
	if firstIndex == -1 {
		t.Fatalf("missing %q", first)
	}
	secondIndex := strings.Index(haystack, second)
	if secondIndex == -1 {
		t.Fatalf("missing %q", second)
	}
	if firstIndex > secondIndex {
		t.Fatalf("expected %q before %q", first, second)
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(repoPath(t, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
