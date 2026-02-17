package main

import (
	_ "embed"
)

type PromptTemplate struct {
	Purpose      string
	Instructions string
}

//go:embed templates/prompt.tmpl
var defaultPromptTemplate string

func NewPromptTemplate() PromptTemplate {
	return PromptTemplate{
		Purpose:      "MR/PR comment",
		Instructions: defaultPromptTemplate,
	}
}

func (p PromptTemplate) SystemMessage() string {
	return p.Purpose + "\n\n" + p.Instructions
}
