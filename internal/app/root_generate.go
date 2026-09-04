package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"golang.org/x/sync/errgroup"
)

type rootChatFunc func(context.Context, *Config, ApiProvider, string, string) (string, error)
type rootStreamFunc func(context.Context, *Config, ApiProvider, string, string, io.Writer) (string, error)

type rootGenerationRequest struct {
	Context      context.Context
	Config       *Config
	Options      RootOptions
	Chat         rootChatFunc
	Stream       rootStreamFunc
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

const (
	smartChunkConcurrency = 4
	smartChunkBatchSize   = 20
)

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
		streamFn := req.Stream
		if streamFn == nil {
			streamFn = streamToWriter
		}
		streamOut := &countingWriter{w: req.Out}
		result.Comment, err = timedCall(cfg, "comment (stream)", func() (string, error) {
			return streamFn(ctx, cfg, cfg.Provider, req.SystemPrompt, req.DiffContent, streamOut)
		})
		if err != nil {
			if streamOut.Written() > 0 {
				return result, err
			}
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
		titleInput := req.DiffContent
		if req.SmartChunk {
			// The complete raw diff may be far larger than a single provider
			// request. The synthesized description already represents every chunk.
			titleInput = result.Comment
		}
		result.Title, err = timedCall(cfg, "title", func() (string, error) {
			return req.Chat(ctx, cfg, cfg.Provider, titlePrompt, titleInput)
		})
		if err != nil {
			return result, normalizeProviderConnectionError(cfg, err)
		}
		result.Title = strings.TrimSpace(result.Title)
	}
	return result, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func (w *countingWriter) Written() int64 {
	if w == nil {
		return 0
	}
	return w.n
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
	debugLog(cfg, "smart-chunk: summarizing %d chunks with concurrency=%d", len(chunks), smartChunkConcurrency)
	eg, egCtx := errgroup.WithContext(req.Context)
	eg.SetLimit(smartChunkConcurrency)
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
			summaries[i] = fmt.Sprintf("File %d/%d — %s\n%s", i+1, len(chunks), firstLine(chunk), strings.TrimSpace(summary))
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return "", err
	}

	return synthesizeSmartChunkSummaries(req, summaries)
}

func synthesizeSmartChunkSummaries(req rootGenerationRequest, summaries []string) (string, error) {
	const aggregationPrompt = `Condense these per-file diff summaries for a later whole-change synthesis.
Preserve every file name and every material behavior, risk, compatibility concern, and test note.
Use concise technical bullets only. Do not drop a file merely because its change seems minor.`

	current := summaries
	level := 1
	for len(current) > smartChunkBatchSize {
		batchCount := (len(current) + smartChunkBatchSize - 1) / smartChunkBatchSize
		debugLog(req.Config, "smart-chunk: aggregation level=%d inputs=%d batches=%d", level, len(current), batchCount)
		next := make([]string, batchCount)
		eg, egCtx := errgroup.WithContext(req.Context)
		eg.SetLimit(smartChunkConcurrency)
		for batch := 0; batch < batchCount; batch++ {
			batch := batch
			start := batch * smartChunkBatchSize
			end := min(start+smartChunkBatchSize, len(current))
			input := strings.Join(current[start:end], "\n\n---\n\n")
			eg.Go(func() error {
				summary, err := timedCall(req.Config, fmt.Sprintf("chunk-aggregate-%d-%d", level, batch+1), func() (string, error) {
					return req.Chat(egCtx, req.Config, req.Config.Provider, aggregationPrompt, input)
				})
				if err != nil {
					return err
				}
				next[batch] = fmt.Sprintf("Aggregate %d/%d\n%s", batch+1, batchCount, strings.TrimSpace(summary))
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return "", err
		}
		current = next
		level++
	}

	debugLog(req.Config, "smart-chunk: all chunks represented, running final synthesis call")
	combinedSummaries := strings.Join(current, "\n\n---\n\n")
	return timedCall(req.Config, "synthesis", func() (string, error) {
		return req.Chat(req.Context, req.Config, req.Config.Provider, req.SystemPrompt, combinedSummaries)
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
