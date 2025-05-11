package main

import (
	"strings"
	"testing"
)

func TestPromptTemplateGeneration(t *testing.T) {
	tt := []struct {
		host     GitHost
		expected string
	}{
		{GitHub, "GitHub PR comment"},
		{GitLab, "GitLab MR comment"},
		{Unknown, "MR/PR comment"},
	}
	for _, tc := range tt {
		pt := NewPromptTemplate(tc.host)
		if !strings.Contains(pt.Purpose, tc.expected) {
			t.Errorf("Prompt purpose mismatch: got %s", pt.Purpose)
		}
		if !strings.Contains(pt.Instructions, "Title:") {
			t.Error("Instructions should include formatting rules")
		}
	}
}
