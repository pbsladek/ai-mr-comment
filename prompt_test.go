package main

import (
	"strings"
	"testing"
)

func TestPromptTemplateGeneration(t *testing.T) {
	pt := NewPromptTemplate()
	if pt.Purpose != "MR/PR comment" {
		t.Errorf("Expected generic purpose, got %s", pt.Purpose)
	}
	if !strings.Contains(pt.Instructions, "Title:") {
		t.Error("Instructions should include formatting rules")
	}
}
