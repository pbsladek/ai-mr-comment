package app

import "github.com/pbsladek/ai-mr-comment/internal/prompts"

var (
	defaultPromptTemplate = prompts.DefaultTemplate
	mrCommitPrompt        = prompts.MRCommitTemplate

	commitMsgPrompt              = prompts.CommitMsgPrompt
	quickCommitPrompt            = prompts.QuickCommitPrompt
	quickCommitFreePrompt        = prompts.QuickCommitFreePrompt
	quickCommitChaosPrompt       = prompts.QuickCommitChaosPrompt
	quickCommitHaikuPrompt       = prompts.QuickCommitHaikuPrompt
	quickCommitRoastPrompt       = prompts.QuickCommitRoastPrompt
	quickCommitMondayPrompt      = prompts.QuickCommitMondayPrompt
	quickCommitJiraPrompt        = prompts.QuickCommitJiraPrompt
	quickCommitEmojiPrompt       = prompts.QuickCommitEmojiPrompt
	quickCommitSassyPrompt       = prompts.QuickCommitSassyPrompt
	quickCommitTechnicalPrompt   = prompts.QuickCommitTechnicalPrompt
	quickCommitInternPrompt      = prompts.QuickCommitInternPrompt
	quickCommitShakespearePrompt = prompts.QuickCommitShakespearePrompt
	quickCommitManagerPrompt     = prompts.QuickCommitManagerPrompt
	quickCommitYodaPrompt        = prompts.QuickCommitYodaPrompt
	quickCommitExcusePrompt      = prompts.QuickCommitExcusePrompt
	commitMsgBodyPrompt          = prompts.CommitMsgBodyPrompt
	changelogPrompt              = prompts.ChangelogPrompt

	mrChaosPrompt       = prompts.MRChaosPrompt
	mrHaikuPrompt       = prompts.MRHaikuPrompt
	mrRoastPrompt       = prompts.MRoastPrompt
	mrInternPrompt      = prompts.MRInternPrompt
	mrShakespearePrompt = prompts.MRShakespearePrompt
	mrManagerPrompt     = prompts.MRManagerPrompt
	mrYodaPrompt        = prompts.MRYodaPrompt
	mrExcusePrompt      = prompts.MRExcusePrompt

	fortunePrompt = prompts.FortunePrompt
	titlePrompt   = prompts.TitlePrompt
)

func resolveSystemPrompt(raw string) (string, error) {
	return prompts.ResolveSystemPrompt(raw)
}

func NewPromptTemplate(templateName string) (string, error) {
	return prompts.NewTemplate(templateName)
}
