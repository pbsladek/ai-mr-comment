package main

import (
	"os"
	"strings"
	"testing"
)

func TestDockerfileUsesRefreshedDHIBaseDigestsAndPackageUpgrades(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")

	required := []string{
		"dhi.io/golang:1.26-debian13-dev@sha256:086c893153f92793f3a1541793cd4a8e8b23bfd4ccaf70c8f4261f496080fb0e",
		"dhi.io/debian-base:trixie-debian13-dev@sha256:9415967aa0ed8adea8b5c048994259d1982026dca143d0303c7bbe0e11ed67d3",
		"apt-get upgrade -y --no-install-recommends",
	}
	for _, want := range required {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}

	staleDigests := []string{
		"sha256:7c7ee6a2db0fa9a332ba1c96f2cc11b53dc7535a899ce66e45391db4dfa26350",
		"sha256:2166e2eaef0651c9ad21de6ab5a34fda12541d89bccf7bcb0a94afceb1b1541b",
	}
	for _, stale := range staleDigests {
		if strings.Contains(dockerfile, stale) {
			t.Fatalf("Dockerfile still references stale base digest %q", stale)
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
		"Build Docker image for vulnerability scan",
		"Build Docker FIPS image for vulnerability scan",
		"uses: docker/scout-action@v1.18.2",
		"image: local://pwbsladek/ai-mr-comment:scan",
		"image: local://pwbsladek/ai-mr-comment:scan-fips",
		"only-fixed: true",
		"only-severities: critical,high",
		"exit-code: true",
	}
	for _, want := range required {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}

	assertBefore(t, workflow, "Scan Docker image vulnerabilities", "Build and push Docker image")
	assertBefore(t, workflow, "Scan Docker FIPS image vulnerabilities", "Build and push Docker FIPS image")
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
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
