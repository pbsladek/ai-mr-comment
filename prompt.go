package main

import (
	_ "embed"
	"strings"
)

type PromptTemplate struct {
	Purpose      string
	Instructions string
}

//go:embed prompt_template.tmpl
var defaultPromptTemplate string

func NewPromptTemplate(host GitHost) PromptTemplate {
	var purpose, platform, artifact string
	switch host {
	case GitHub:
		purpose, platform, artifact = "GitHub PR comment", "GitHub", "PR"
	case GitLab:
		purpose, platform, artifact = "GitLab MR comment", "GitLab", "MR"
	default:
		purpose, platform, artifact = "MR/PR comment", "version control system", "MR/PR"
	}

	instructions := strings.ReplaceAll(defaultPromptTemplate, "{{artifact}}", artifact)
	instructions = strings.ReplaceAll(instructions, "{{platform}}", platform)

	return PromptTemplate{Purpose: purpose, Instructions: instructions}
}

func (p PromptTemplate) SystemMessage() string {
	return p.Purpose + "\n\n" + p.Instructions
}
