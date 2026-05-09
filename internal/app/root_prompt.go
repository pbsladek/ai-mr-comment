package app

import "fmt"

type rootPromptRequest struct {
	Template             string
	SystemPromptOverride string
	PresetSuffix         string
	ExitCode             bool
	MRStyles             []string
}

type rootPromptResult struct {
	SystemPrompt    string
	TemplateSource  string
	TemplateWarning error
}

const exitCodePreamble = "Before your review, output a verdict on the very first line in exactly this format:\nVERDICT: PASS\nor\nVERDICT: FAIL\nUse FAIL if the diff contains critical bugs, security vulnerabilities, data loss risks, or broken public APIs. Use PASS for everything else. Then continue with your normal review on the next line.\n\n"

func resolveRootPrompt(req rootPromptRequest) (rootPromptResult, error) {
	systemPrompt, templateErr := NewPromptTemplate(req.Template)
	result := rootPromptResult{
		SystemPrompt:    systemPrompt,
		TemplateSource:  rootTemplateSource(req.Template, templateErr),
		TemplateWarning: templateErr,
	}

	if req.SystemPromptOverride != "" {
		override, err := resolveSystemPrompt(req.SystemPromptOverride)
		if err != nil {
			return result, err
		}
		result.SystemPrompt = override
	}

	if len(req.MRStyles) > 0 {
		stylePrompt, ok := rootStylePrompt(req.MRStyles[0])
		if !ok {
			return result, fmt.Errorf("unknown style %q", req.MRStyles[0])
		}
		result.SystemPrompt = stylePrompt
	}

	if req.PresetSuffix != "" && req.SystemPromptOverride == "" {
		result.SystemPrompt += req.PresetSuffix
	}
	if req.ExitCode {
		result.SystemPrompt = exitCodePreamble + result.SystemPrompt
	}
	return result, nil
}

func rootTemplateSource(template string, err error) string {
	if template == "default" {
		return "embedded"
	}
	if err != nil {
		return "embedded (fallback)"
	}
	return "embedded/custom"
}

func rootStylePrompt(name string) (string, bool) {
	switch name {
	case "chaos":
		return mrChaosPrompt, true
	case "haiku":
		return mrHaikuPrompt, true
	case "roast":
		return mrRoastPrompt, true
	case "intern":
		return mrInternPrompt, true
	case "shakespeare":
		return mrShakespearePrompt, true
	case "manager":
		return mrManagerPrompt, true
	case "yoda":
		return mrYodaPrompt, true
	case "excuse":
		return mrExcusePrompt, true
	default:
		return "", false
	}
}
