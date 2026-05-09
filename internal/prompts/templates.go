package prompts

import _ "embed"

// User-facing --template prompts.
//
//go:embed templates/default.tmpl
var DefaultTemplate string

//go:embed templates/technical.tmpl
var MRTechnicalTemplate string

//go:embed templates/emoji.tmpl
var MREmojiTemplate string

//go:embed templates/jira.tmpl
var MRJiraTemplate string

//go:embed templates/monday.tmpl
var MRMondayTemplate string

//go:embed templates/sassy.tmpl
var MRSassyTemplate string

//go:embed templates/user-focused.tmpl
var MRUserFocusedTemplate string

//go:embed templates/conventional.tmpl
var MRConventionalTemplate string

//go:embed templates/commit.tmpl
var MRCommitTemplate string

//go:embed templates/commit-conventional.tmpl
var MRConventionalCommitTemplate string

//go:embed templates/commit-emoji.tmpl
var MRCommitEmojiTemplate string

// Internal prompts used programmatically, not via --template.
//
//go:embed templates/internal-commit-msg.tmpl
var CommitMsgPrompt string

//go:embed templates/internal-quick-commit.tmpl
var QuickCommitPrompt string

//go:embed templates/internal-quick-commit-free.tmpl
var QuickCommitFreePrompt string

//go:embed templates/internal-quick-commit-chaos.tmpl
var QuickCommitChaosPrompt string

//go:embed templates/internal-quick-commit-haiku.tmpl
var QuickCommitHaikuPrompt string

//go:embed templates/internal-quick-commit-roast.tmpl
var QuickCommitRoastPrompt string

//go:embed templates/internal-quick-commit-monday.tmpl
var QuickCommitMondayPrompt string

//go:embed templates/internal-quick-commit-jira.tmpl
var QuickCommitJiraPrompt string

//go:embed templates/internal-quick-commit-emoji.tmpl
var QuickCommitEmojiPrompt string

//go:embed templates/internal-quick-commit-sassy.tmpl
var QuickCommitSassyPrompt string

//go:embed templates/internal-quick-commit-technical.tmpl
var QuickCommitTechnicalPrompt string

//go:embed templates/internal-quick-commit-intern.tmpl
var QuickCommitInternPrompt string

//go:embed templates/internal-quick-commit-shakespeare.tmpl
var QuickCommitShakespearePrompt string

//go:embed templates/internal-quick-commit-manager.tmpl
var QuickCommitManagerPrompt string

//go:embed templates/internal-quick-commit-yoda.tmpl
var QuickCommitYodaPrompt string

//go:embed templates/internal-quick-commit-excuse.tmpl
var QuickCommitExcusePrompt string

//go:embed templates/internal-commit-msg-body.tmpl
var CommitMsgBodyPrompt string

//go:embed templates/internal-changelog.tmpl
var ChangelogPrompt string

//go:embed templates/chaos.tmpl
var MRChaosPrompt string

//go:embed templates/haiku.tmpl
var MRHaikuPrompt string

//go:embed templates/roast.tmpl
var MRoastPrompt string

//go:embed templates/intern.tmpl
var MRInternPrompt string

//go:embed templates/shakespeare.tmpl
var MRShakespearePrompt string

//go:embed templates/manager.tmpl
var MRManagerPrompt string

//go:embed templates/yoda.tmpl
var MRYodaPrompt string

//go:embed templates/excuse.tmpl
var MRExcusePrompt string

const FortunePrompt = `Generate a single short fortune-cookie-style quote for a software developer.
Output ONLY the quote — no attribution, no explanation, no quotes around it, no code fences.
Keep it under 80 characters. It should be witty, wise, or gently humorous.
Draw from themes like debugging, shipping, complexity, naming things, or the nature of code.
Generate something original every time.

Examples of the spirit (do NOT copy literally):
  The best code is the code you didn't have to write.
  It works on my machine is not a deployment strategy.
  Naming things is hard. Naming things well is an art.
  Every comment is an apology for unclear code.
  Ship it. The bugs will tell you what to fix next.`

const TitlePrompt = `Generate a single-line MR/PR title for the following diff.
Output only the title text — no explanation, no punctuation at the end, no quotes.
Keep it under 72 characters. Use the imperative mood (e.g. "Add", "Fix", "Refactor").
If the active template follows Conventional Commits style, prefix with the appropriate type (feat, fix, chore, etc.).`

var BuiltinTemplates = map[string]string{
	"technical":           MRTechnicalTemplate,
	"emoji":               MREmojiTemplate,
	"jira":                MRJiraTemplate,
	"monday":              MRMondayTemplate,
	"sassy":               MRSassyTemplate,
	"user-focused":        MRUserFocusedTemplate,
	"conventional":        MRConventionalTemplate,
	"commit":              MRCommitTemplate,
	"commit-conventional": MRConventionalCommitTemplate,
	"commit-emoji":        MRCommitEmojiTemplate,
	"chaos":               MRChaosPrompt,
	"haiku":               MRHaikuPrompt,
	"roast":               MRoastPrompt,
	"intern":              MRInternPrompt,
	"shakespeare":         MRShakespearePrompt,
	"manager":             MRManagerPrompt,
	"yoda":                MRYodaPrompt,
	"excuse":              MRExcusePrompt,
}

// NewTemplate returns the prompt for a built-in or custom template name.
// Custom templates are searched in the working directory and user config
// locations. Missing custom templates return the embedded default with an
// explanatory error so callers can warn and fall back.
func NewTemplate(templateName string) (string, error) {
	if templateName == "default" {
		return DefaultTemplate, nil
	}
	if template, ok := BuiltinTemplates[templateName]; ok {
		return template, nil
	}
	content, err := FindCustomTemplate(templateName)
	if err != nil {
		return DefaultTemplate, err
	}
	return content, nil
}
