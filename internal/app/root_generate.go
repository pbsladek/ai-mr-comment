package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"golang.org/x/sync/errgroup"
)

type rootChatFunc func(context.Context, *Config, ApiProvider, string, string) (string, error)

type rootGenerationRequest struct {
	Context      context.Context
	Config       *Config
	Options      RootOptions
	Chat         rootChatFunc
	SystemPrompt string
	DiffContent  string
	SmartChunk   bool
	ShouldStream bool
	Out          io.Writer
}

type rootGenerationResult struct {
	Comment       string
	Title         string
	CommitMessage string
	StreamedOK    bool
}

func generateRootProviderOutput(req rootGenerationRequest) (rootGenerationResult, error) {
	cfg := req.Config
	opts := req.Options
	ctx := req.Context

	var result rootGenerationResult
	var err error
	switch {
	case opts.TitleOnly:
		result.Title, err = timedCall(cfg, "title", func() (string, error) {
			return req.Chat(ctx, cfg, cfg.Provider, titlePrompt, req.DiffContent)
		})
		result.Title = strings.TrimSpace(result.Title)
	case opts.GenerateCommitMsg:
		debugLog(cfg, "commit-msg: generating commit message with separate API call (multi-line=%v)", opts.MultiLine)
		prompt := commitMsgPrompt
		if opts.MultiLine {
			prompt = commitMsgBodyPrompt
		} else if isCommitOnlyTemplate(opts.EffectiveTemplate) {
			prompt = req.SystemPrompt
		}
		result.CommitMessage, err = timedCall(cfg, "commit-msg", func() (string, error) {
			return req.Chat(ctx, cfg, cfg.Provider, prompt, req.DiffContent)
		})
		if opts.MultiLine {
			result.CommitMessage = normalizeCommitBody(result.CommitMessage)
		} else {
			result.CommitMessage = normalizeCommitMessage(result.CommitMessage)
		}
	case req.SmartChunk:
		result.Comment, err = generateSmartChunkComment(req)
	case req.ShouldStream:
		result.Comment, err = timedCall(cfg, "comment (stream)", func() (string, error) {
			return streamToWriter(ctx, cfg, cfg.Provider, req.SystemPrompt, req.DiffContent, req.Out)
		})
		if err != nil {
			result.Comment, err = timedCall(cfg, "comment (fallback)", func() (string, error) {
				return req.Chat(ctx, cfg, cfg.Provider, req.SystemPrompt, req.DiffContent)
			})
		} else {
			result.StreamedOK = true
		}
	default:
		result.Comment, result.Title, err = generateBufferedRootCommentAndTitle(req)
	}
	if err != nil {
		return result, normalizeProviderConnectionError(cfg, err)
	}

	needsTitle := (opts.GenerateTitle || opts.UpdateTitle || opts.Format == "json") && !opts.GenerateCommitMsg
	if needsTitle && !opts.TitleOnly && result.Title == "" {
		debugLog(cfg, "title: generating title after stream")
		result.Title, err = timedCall(cfg, "title", func() (string, error) {
			return req.Chat(ctx, cfg, cfg.Provider, titlePrompt, req.DiffContent)
		})
		if err != nil {
			return result, normalizeProviderConnectionError(cfg, err)
		}
		result.Title = strings.TrimSpace(result.Title)
	}
	return result, nil
}

func generateSmartChunkComment(req rootGenerationRequest) (string, error) {
	cfg := req.Config
	chunks := splitDiffByFile(req.DiffContent)
	debugLog(cfg, "smart-chunk: files=%d", len(chunks))
	if len(chunks) <= 1 {
		return timedCall(cfg, "comment", func() (string, error) {
			return req.Chat(req.Context, cfg, cfg.Provider, req.SystemPrompt, req.DiffContent)
		})
	}

	const chunkPrompt = "Summarize the changes in this file diff in 3-5 bullet points. Be concise and technical."
	summaries := make([]string, len(chunks))
	debugLog(cfg, "smart-chunk: summarizing %d chunks in parallel", len(chunks))
	eg, egCtx := errgroup.WithContext(req.Context)
	for i, chunk := range chunks {
		i, chunk := i, chunk
		eg.Go(func() error {
			debugLog(cfg, "smart-chunk: processing chunk %d/%d", i+1, len(chunks))
			summary, err := timedCall(cfg, fmt.Sprintf("chunk-summary-%d", i+1), func() (string, error) {
				return req.Chat(egCtx, cfg, cfg.Provider, chunkPrompt, processDiff(chunk, 1000))
			})
			if err != nil {
				return err
			}
			summaries[i] = summary
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return "", err
	}

	debugLog(cfg, "smart-chunk: all chunks summarized, running synthesis call")
	combinedSummaries := strings.Join(summaries, "\n\n---\n\n")
	return timedCall(cfg, "synthesis", func() (string, error) {
		return req.Chat(req.Context, cfg, cfg.Provider, req.SystemPrompt, combinedSummaries)
	})
}

func generateBufferedRootCommentAndTitle(req rootGenerationRequest) (comment, title string, err error) {
	cfg := req.Config
	needsTitle := (req.Options.GenerateTitle || req.Options.UpdateTitle || req.Options.Format == "json") && !req.Options.GenerateCommitMsg
	if !needsTitle {
		comment, err = timedCall(cfg, "comment", func() (string, error) {
			return req.Chat(req.Context, cfg, cfg.Provider, req.SystemPrompt, req.DiffContent)
		})
		return comment, "", err
	}

	debugLog(cfg, "title+comment: running in parallel")
	eg, egCtx := errgroup.WithContext(req.Context)
	eg.Go(func() error {
		var callErr error
		comment, callErr = timedCall(cfg, "comment (parallel)", func() (string, error) {
			return req.Chat(egCtx, cfg, cfg.Provider, req.SystemPrompt, req.DiffContent)
		})
		return callErr
	})
	eg.Go(func() error {
		var callErr error
		title, callErr = timedCall(cfg, "title (parallel)", func() (string, error) {
			return req.Chat(egCtx, cfg, cfg.Provider, titlePrompt, req.DiffContent)
		})
		return callErr
	})
	if err := eg.Wait(); err != nil {
		return comment, "", err
	}
	return comment, strings.TrimSpace(title), nil
}

func normalizeProviderConnectionError(cfg *Config, err error) error {
	if cfg.Provider == Ollama && strings.Contains(err.Error(), "connection refused") {
		return fmt.Errorf("failed to connect to Ollama at %s.\nMake sure Ollama is running (try 'ollama serve') or check your configuration", cfg.OllamaEndpoint)
	}
	return err
}
