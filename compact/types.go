// Package compact summarizes conversation history without owning a model,
// session store, runtime configuration, or transport.
package compact

import (
	"context"
	"log/slog"
)

// Message contains only the evidence needed for compaction. Callers retain
// provider-specific messages and durable history outside this package.
type Message struct {
	Role             string
	Content          string
	ReasoningContent string
	Name             string
	ToolCalls        []ToolCall
	CompactBoundary  bool
}

type ToolCall struct{ Name string }

type Estimator interface{ Estimate(string) int }
type EstimatorFunc func(string) int

func (f EstimatorFunc) Estimate(text string) int { return f(text) }

// TextOptions supplies application-specific tool rendering and fallback goals.
type TextOptions struct {
	SummarizeTool     func(name, result string) string
	FallbackGoal      string
	MergeFallbackGoal string
}

type Attachment struct {
	Kind     string `json:"kind"`
	Source   string `json:"source,omitempty"`
	Content  string `json:"content"`
	Priority int    `json:"priority,omitempty"`
	Tokens   int    `json:"tokens,omitempty"`
}

type ShellState interface {
	GetCwd() string
	GetEnv() map[string]string
}

type Progress struct {
	Phase               string `json:"phase"`
	Trigger             string `json:"trigger,omitempty"`
	Message             string `json:"message,omitempty"`
	BoundaryID          string `json:"boundary_id,omitempty"`
	SummarySource       string `json:"summary_source,omitempty"`
	Chunk               int    `json:"chunk,omitempty"`
	Chunks              int    `json:"chunks,omitempty"`
	PreTokenEstimate    int    `json:"pre_token_estimate,omitempty"`
	PostTokenEstimate   int    `json:"post_token_estimate,omitempty"`
	DroppedMessages     int    `json:"dropped_messages,omitempty"`
	RetainedMessages    int    `json:"retained_messages,omitempty"`
	RestoredAttachments int    `json:"restored_attachments,omitempty"`
	Model               string `json:"model,omitempty"`
	Error               string `json:"error,omitempty"`
}

// ChatRequest contains a text-only summarization request. Chat implementations
// must not enable tools or write conversation history as a side effect.
type ChatRequest struct {
	SystemPrompt    string
	UserPrompt      string
	Kind            string
	Units           int
	TranscriptRunes int
}

type ChatFunc func(context.Context, ChatRequest) (string, error)

type Config struct {
	MaxChunkTokens int
	MaxParallel    int
	Model          string
	AgentName      string
	Estimator      Estimator
	Text           TextOptions
	Chat           ChatFunc
	Logger         *slog.Logger
	OnProgress     func(Progress)
}

const (
	DefaultMaxChunkTokens = 12000
	DefaultMaxParallel    = 4
)
