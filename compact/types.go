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
	// Index is the message position in the session. Zero means unset, so a
	// transcript only prints a replay pointer when the caller filled one in.
	Index int
	// Ref is an externalized id for a body that lives outside the transcript.
	Ref string
}

// ToolCall is one tool invocation rendered into the transcript. Arguments is
// optional and must already be truncated by the caller; the transcript still
// caps it so a large JSON blob cannot come back in through this field.
type ToolCall struct {
	Name      string
	Arguments string
}

type Estimator interface{ Estimate(string) int }
type EstimatorFunc func(string) int

func (f EstimatorFunc) Estimate(text string) int { return f(text) }

// TextOptions supplies application-specific tool rendering and fallback goals.
// Every field is optional. A zero value keeps the v0.2.3 transcript shape,
// except that a tool summary is no longer truncated twice.
type TextOptions struct {
	// SummarizeTool returns one header line for a tool result. It no longer
	// replaces the body; the body comes from CompressTool or the built-in
	// line scorer. Returning an empty string omits the header.
	SummarizeTool func(name, result string) string
	// CompressTool returns the excerpt to keep for one tool result. When nil
	// the engine scores lines itself and falls back to its own cap.
	CompressTool func(name, result string, budget int) string
	// TranscriptBudget overrides TranscriptMaxRunes when positive.
	TranscriptBudget int
	// ToolRunes overrides the per-tool cap when positive.
	ToolRunes int
	// ReasoningScore scores one reasoning block against the observation that
	// followed it. The bool reports whether a scoring model is available;
	// false skips reasoning compression entirely instead of inventing a rule.
	ReasoningScore func(block string, prior []string, observation string) (float64, bool)
	// ReasoningSkipDelta is the Δ(r,o) above which an observed reasoning block
	// is replaced by [SKIP]. Zero selects the built-in threshold.
	ReasoningSkipDelta float64
	FallbackGoal       string
	MergeFallbackGoal  string
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
