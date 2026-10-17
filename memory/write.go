package memory

import (
	"context"
	"fmt"
	"math"
	"strings"
)

type WriteBackend interface {
	Writer
	ContentAnalyzer
}

// WriteRejectedError distinguishes invalid input from backend availability or
// persistence failures, so transports can return the appropriate status.
type WriteRejectedError struct{ Message string }

func (e *WriteRejectedError) Error() string { return e.Message }

func rejectWrite(message string, args ...any) error {
	return &WriteRejectedError{Message: fmt.Sprintf(message, args...)}
}

// Write is the shared host admission path for explicit tools, HTTP/CLI and
// automatic capture. The backend owns deduplication and state transitions.
func Write(ctx context.Context, backend WriteBackend, request RememberRequest) (RememberResult, error) {
	if err := ctx.Err(); err != nil {
		return RememberResult{}, err
	}
	if backend == nil {
		return RememberResult{}, fmt.Errorf("memory store not initialized")
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Tier < TierShort || request.Tier > TierLong {
		return RememberResult{}, rejectWrite("tier must be short, medium, or long")
	}
	if math.IsNaN(request.Importance) || math.IsInf(request.Importance, 0) || request.Importance < 0 || request.Importance > 1 {
		return RememberResult{}, rejectWrite("importance must be between 0 and 1")
	}
	if math.IsNaN(request.Options.Confidence) || math.IsInf(request.Options.Confidence, 0) || request.Options.Confidence < 0 || request.Options.Confidence > 1 {
		return RememberResult{}, rejectWrite("confidence must be between 0 and 1")
	}
	for _, issue := range backend.AnalyzeMemoryContent(request.Content) {
		switch issue.Reason {
		case "empty":
			return RememberResult{}, rejectWrite("content is required")
		case "raw_conversation":
			return RememberResult{}, rejectWrite("raw conversation content is rejected; save a concise stable summary instead")
		case "secret_like":
			return RememberResult{}, rejectWrite("secret-like content is rejected and must not be stored in durable memory")
		case "prompt_injection":
			return RememberResult{}, rejectWrite("prompt-injection-like content is rejected and must not be stored in durable memory")
		case "oversized":
			return RememberResult{}, rejectWrite("content exceeds %d rune limit; summarize it or index the source with rag_index", MaxDurableMemoryContentRunes)
		}
	}
	if request.Tier == TierLong && request.Options.Confidence > 0 && request.Options.Confidence < .35 {
		return RememberResult{}, rejectWrite("long-term memory with confidence below 0.35 is rejected; save a medium-tier note or confirm stronger evidence")
	}
	return backend.Remember(ctx, request)
}

// ExactScope is for metadata already bound by the host, never model-supplied
// authorization. Scoped writes must not deduplicate with another actor's note.
func ExactScope(tags []string) func(Entry) bool {
	keys := scopeTags(tags)
	if len(keys) == 0 {
		return nil
	}
	return func(entry Entry) bool {
		other := scopeTags(entry.Tags)
		if len(keys) != len(other) {
			return false
		}
		for key := range keys {
			if !other[key] {
				return false
			}
		}
		return true
	}
}

func scopeTags(tags []string) map[string]bool {
	out := make(map[string]bool)
	for _, tag := range tags {
		tag = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tag)), "#")
		if strings.HasPrefix(tag, "scope:") {
			out[tag] = true
		}
	}
	return out
}
