package compact

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"
)

// Engine coordinates pure compaction work. It never mutates a session or
// chooses credentials, providers, runtime policies, or durable checkpoints.
type Engine struct {
	cfg        Config
	progressMu sync.Mutex
}

func New(cfg Config) *Engine {
	if cfg.MaxChunkTokens <= 0 {
		cfg.MaxChunkTokens = DefaultMaxChunkTokens
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = DefaultMaxParallel
	}
	if cfg.Estimator == nil {
		cfg.Estimator = EstimatorFunc(func(text string) int { return (utf8.RuneCountInString(text) + 3) / 4 })
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Engine{cfg: cfg}
}

func (e *Engine) emit(progress Progress) {
	if e.cfg.OnProgress == nil {
		return
	}
	e.progressMu.Lock()
	defer e.progressMu.Unlock()
	e.cfg.OnProgress(progress)
}

func (e *Engine) Generate(ctx context.Context, messages []Message) (string, string, error) {
	if len(messages) == 0 {
		return "", "", fmt.Errorf("compact session: no textual content to summarize")
	}
	if e == nil || e.cfg.Chat == nil {
		return "", "", fmt.Errorf("compact session: provider is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	est := e.cfg.Estimator
	maxChunkTokens, maxParallel := e.cfg.MaxChunkTokens, e.cfg.MaxParallel
	chunks := SplitChunks(messages, est, maxChunkTokens)
	if len(chunks) <= 1 {
		e.emit(Progress{
			Phase:   "progress",
			Message: "Summarizing conversation context…",
			Model:   e.cfg.Model,
			Chunk:   1,
			Chunks:  1,
		})
		summary, err := e.summarize(ctx, messages)
		if err != nil {
			return "", "", err
		}
		return summary, "llm", nil
	}

	e.cfg.Logger.Info("compact map-reduce start",
		"chunks", len(chunks),
		"max_parallel", maxParallel,
		"max_chunk_tokens", maxChunkTokens,
		"model", e.cfg.Model,
	)
	e.emit(Progress{
		Phase:   "progress",
		Message: fmt.Sprintf("Compressing context in %d parallel chunks…", len(chunks)),
		Chunks:  len(chunks),
		Model:   e.cfg.Model,
	})

	type chunkResult struct {
		index   int
		summary string
		source  string
		err     error
	}
	results := make([]chunkResult, len(chunks))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := range chunks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			e.emit(Progress{
				Phase:   "progress",
				Message: fmt.Sprintf("Compressing context · chunk %d/%d", i+1, len(chunks)),
				Chunk:   i + 1,
				Chunks:  len(chunks),
				Model:   e.cfg.Model,
			})
			summary, err := e.summarize(ctx, chunks[i])
			source := "llm"
			if err != nil {
				e.cfg.Logger.Warn("compact chunk LLM failed, trying local",
					"chunk", i+1, "chunks", len(chunks), "error", err)
				local := LocalSummary(chunks[i], e.cfg.Text)
				if strings.TrimSpace(local) == "" {
					results[i] = chunkResult{index: i, err: err}
					return
				}
				if validation := Validate(local, chunks[i]); !validation.Valid {
					results[i] = chunkResult{index: i, err: fmt.Errorf("chunk %d local fallback invalid: %s", i+1, validation.Reason)}
					return
				}
				summary = local
				source = "local"
				err = nil
			}
			results[i] = chunkResult{index: i, summary: summary, source: source, err: err}
		}(i)
	}
	wg.Wait()

	segmentSummaries := make([]string, 0, len(chunks))
	usedLocal := 0
	for i, r := range results {
		if r.err != nil {
			return "", "", fmt.Errorf("compact session: chunk %d/%d: %w", i+1, len(chunks), r.err)
		}
		if strings.TrimSpace(r.summary) == "" {
			return "", "", fmt.Errorf("compact session: chunk %d/%d produced empty summary", i+1, len(chunks))
		}
		if r.source == "local" {
			usedLocal++
		}
		segmentSummaries = append(segmentSummaries, fmt.Sprintf("### Segment %d/%d\n%s", i+1, len(chunks), strings.TrimSpace(r.summary)))
	}

	e.emit(Progress{
		Phase:   "progress",
		Message: fmt.Sprintf("Merging %d compact segments…", len(segmentSummaries)),
		Chunks:  len(chunks),
		Model:   e.cfg.Model,
	})
	merged, err := e.merge(ctx, segmentSummaries)
	if err != nil {
		e.cfg.Logger.Warn("compact merge LLM failed, trying local merge", "error", err, "segments", len(segmentSummaries))
		merged = MergeLocal(segmentSummaries, e.cfg.Text)
		if strings.TrimSpace(merged) == "" {
			return "", "", fmt.Errorf("compact session: merge summaries: %w", err)
		}
	}
	source := "llm-mapreduce"
	if usedLocal == len(chunks) {
		source = "local-mapreduce"
	} else if usedLocal > 0 {
		source = "llm-mapreduce-hybrid"
	}
	return merged, source, nil
}

func (e *Engine) summarize(ctx context.Context, messages []Message) (string, error) {
	transcript := Transcript(messages, e.cfg.Text)
	if strings.TrimSpace(transcript) == "" {
		return "", fmt.Errorf("compact session: no textual content to summarize")
	}
	return e.invoke(ctx, ChatRequest{UserPrompt: SegmentPrompt(transcript, e.cfg.AgentName), Kind: "segment", Units: len(messages), TranscriptRunes: utf8.RuneCountInString(transcript)})
}

func (e *Engine) merge(ctx context.Context, summaries []string) (string, error) {
	body := strings.Join(summaries, "\n\n")
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("compact session: no segment summaries to merge")
	}
	body = truncateRunes(body, TranscriptMaxRunes)
	return e.invoke(ctx, ChatRequest{UserPrompt: MergePrompt(body, e.cfg.AgentName), Kind: "merge", Units: len(summaries), TranscriptRunes: utf8.RuneCountInString(body)})
}

func (e *Engine) invoke(ctx context.Context, request ChatRequest) (string, error) {
	sumCtx, cancel := SummaryContext(ctx)
	defer cancel()
	if err := sumCtx.Err(); err != nil {
		return "", err
	}
	e.cfg.Logger.Debug("compact summary request", "kind", request.Kind, "units", request.Units, "transcript_runes", request.TranscriptRunes, "model", e.cfg.Model, "timeout", SummaryTimeout.String())
	request.SystemPrompt = "You are a compaction agent. Tool use is not allowed. Produce only a factual text summary for future context."
	summary, err := e.cfg.Chat(sumCtx, request)
	if err != nil {
		return "", fmt.Errorf("compact session: generate summary: %w", err)
	}
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("compact session: empty summary")
	}
	return strings.TrimSpace(summary), nil
}
