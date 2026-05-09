package app

type rootOutputPayload struct {
	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	Comment       string `json:"comment,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
	Verdict       string `json:"verdict,omitempty"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	DiffSource    string `json:"diff_source,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
}

func buildRootOutputPayload(cfg *Config, titleOnly, generateCommitMsg bool, title, comment, commitMessage, verdict, diffSource string, truncated bool) rootOutputPayload {
	switch {
	case titleOnly:
		return rootOutputPayload{
			Title:      title,
			Provider:   string(cfg.Provider),
			Model:      getModelName(cfg),
			DiffSource: diffSource,
			Truncated:  truncated,
		}
	case generateCommitMsg:
		return rootOutputPayload{
			CommitMessage: commitMessage,
			Provider:      string(cfg.Provider),
			Model:         getModelName(cfg),
			DiffSource:    diffSource,
			Truncated:     truncated,
		}
	default:
		return rootOutputPayload{
			Title:       title,
			Description: comment,
			Comment:     comment,
			Verdict:     verdict,
			Provider:    string(cfg.Provider),
			Model:       getModelName(cfg),
			DiffSource:  diffSource,
			Truncated:   truncated,
		}
	}
}
