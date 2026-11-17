package compact

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// SplitChunks keeps the historical signature. An unsplittable span comes back
// as the original turn chunk; SplitChunksChecked reports ErrTranscriptBudget
// for that case. Generate uses the checked form and does not call the model.
func SplitChunks(messages []Message, est Estimator, maxChunkTokens int) [][]Message {
	chunks, err := SplitChunksChecked(messages, est, maxChunkTokens)
	if err != nil {
		return splitTurnChunks(messages, est, maxChunkTokens)
	}
	return chunks
}

// splitOversizedChunkMessages breaks one oversized message on whitespace or
// sentence punctuation. Windows do not overlap: the next window starts where
// the previous one ended. A continuous span that itself exceeds the chunk
// token budget is left intact and reported by SplitChunksChecked.
// Several messages in one chunk stay together: the turn split already ran.
func splitOversizedChunkMessages(chunks [][]Message, est Estimator, maxChunkTokens int) [][]Message {
	out, err := splitOversizedChunkMessagesChecked(chunks, est, maxChunkTokens)
	if err != nil {
		// SplitChunks keeps its [][]Message result. The unsplittable message
		// is returned whole so Generate can reject it before any model call.
		return chunks
	}
	return out
}

// SplitChunksChecked is SplitChunks plus the unsplittable-span error.
// Generate uses it. A nil error means every returned window fits maxChunkTokens
// and no continuous span was cut. Callers of SplitChunks keep the old signature.
func SplitChunksChecked(messages []Message, est Estimator, maxChunkTokens int) ([][]Message, error) {
	if maxChunkTokens <= 0 {
		maxChunkTokens = DefaultMaxChunkTokens
	}
	chunks := splitTurnChunks(messages, est, maxChunkTokens)
	return splitOversizedChunkMessagesChecked(chunks, est, maxChunkTokens)
}

func splitTurnChunks(messages []Message, est Estimator, maxChunkTokens int) [][]Message {
	if len(messages) == 0 {
		return nil
	}
	if maxChunkTokens <= 0 {
		maxChunkTokens = DefaultMaxChunkTokens
	}
	if EstimateMessages(est, messages) <= maxChunkTokens {
		return [][]Message{append([]Message(nil), messages...)}
	}
	turnStarts := make([]int, 0, len(messages)/2+1)
	for i, msg := range messages {
		if msg.Role == "user" {
			turnStarts = append(turnStarts, i)
		}
	}
	if len(turnStarts) == 0 {
		return [][]Message{append([]Message(nil), messages...)}
	}
	if turnStarts[0] != 0 {
		turnStarts = append([]int{0}, turnStarts...)
	}
	turnStarts = append(turnStarts, len(messages))

	var chunks [][]Message
	chunkStart := turnStarts[0]
	for t := 0; t < len(turnStarts)-1; t++ {
		turnEnd := turnStarts[t+1]
		candidate := messages[chunkStart:turnEnd]
		if EstimateMessages(est, candidate) <= maxChunkTokens {
			continue
		}
		prevEnd := turnStarts[t]
		if prevEnd > chunkStart {
			chunks = append(chunks, append([]Message(nil), messages[chunkStart:prevEnd]...))
			chunkStart = prevEnd
			candidate = messages[chunkStart:turnEnd]
		}
		if EstimateMessages(est, candidate) > maxChunkTokens {
			chunks = append(chunks, append([]Message(nil), candidate...))
			chunkStart = turnEnd
		}
	}
	if chunkStart < len(messages) {
		chunks = append(chunks, append([]Message(nil), messages[chunkStart:]...))
	}
	if len(chunks) == 0 {
		return [][]Message{append([]Message(nil), messages...)}
	}
	return chunks
}

func splitOversizedChunkMessagesChecked(chunks [][]Message, est Estimator, maxChunkTokens int) ([][]Message, error) {
	if maxChunkTokens <= 0 {
		return chunks, nil
	}
	var out [][]Message
	for _, chunk := range chunks {
		if len(chunk) != 1 || EstimateMessages(est, chunk) <= maxChunkTokens {
			out = append(out, chunk)
			continue
		}
		parts, err := splitMessageWindows(chunk[0], maxChunkTokens, est)
		if err != nil {
			return chunks, err
		}
		if len(parts) <= 1 {
			if EstimateMessages(est, chunk) > maxChunkTokens {
				return chunks, fmt.Errorf("%w", ErrTranscriptBudget)
			}
			out = append(out, chunk)
			continue
		}
		for _, part := range parts {
			if contentTokens(est, part.Content) > contentTokenBudget(maxChunkTokens) {
				return chunks, fmt.Errorf("%w", ErrTranscriptBudget)
			}
			out = append(out, []Message{part})
		}
	}
	return out, nil
}

// splitMessageWindows packs separator-free spans in order. The next window
// starts at the previous end. A span is never cut. Role, name, and tool calls
// are copied onto every window; only Content changes.
func splitMessageWindows(msg Message, maxChunkTokens int, est Estimator) ([]Message, error) {
	runes := []rune(msg.Content)
	if len(runes) == 0 {
		return []Message{msg}, nil
	}
	room := contentTokenBudget(maxChunkTokens)
	if unsplittableSpanOverBudget(runes, maxChunkTokens, est) {
		return nil, fmt.Errorf("%w", ErrTranscriptBudget)
	}
	var parts []Message
	start := 0
	for start < len(runes) {
		end := greedyWindowEnd(runes, start, room, est)
		if end <= start {
			return nil, fmt.Errorf("%w", ErrTranscriptBudget)
		}
		part := msg
		part.Content = string(runes[start:end])
		if contentTokens(est, part.Content) > room || utf8.RuneCountInString(part.Content) > room {
			return nil, fmt.Errorf("%w", ErrTranscriptBudget)
		}
		parts = append(parts, part)
		start = end
	}
	return parts, nil
}

// greedyWindowEnd takes whole spans and the separators between them until the
// next span would pass the room. It returns start when even one span does not
// fit. It never returns an index inside a span.
func greedyWindowEnd(runes []rune, start, room int, est Estimator) int {
	end := start
	i := start
	for i < len(runes) {
		next := i
		if isSpanSeparator(runes[i]) {
			for next < len(runes) && isSpanSeparator(runes[next]) {
				next++
			}
		} else {
			for next < len(runes) && !isSpanSeparator(runes[next]) {
				next++
			}
		}
		if estimateRange(est, runes[start:next]) > room || next-start > room {
			break
		}
		end = next
		i = next
	}
	return end
}

func isSpanSeparator(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	// Sentence punctuation only. Slash, hyphen, dot and underscore stay inside
	// a path, a date, or an amount, so those tokens are one span.
	switch r {
	case '。', '！', '？', '；', '，', '、', ',', '!', '?', ';', ':', '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'', '`':
		return true
	default:
		return false
	}
}

// unsplittableSpanOverBudget reports a separator-free span whose own token
// estimate exceeds the chunk budget. Splitting it would hide the span from
// every window.
func unsplittableSpanOverBudget(runes []rune, maxChunkTokens int, est Estimator) bool {
	start := 0
	for i := 0; i <= len(runes); i++ {
		if i < len(runes) && !isSpanSeparator(runes[i]) {
			continue
		}
		if i > start && estimateRange(est, runes[start:i]) > contentTokenBudget(maxChunkTokens) {
			return true
		}
		start = i + 1
	}
	return false
}

func estimateRange(est Estimator, runes []rune) int {
	return contentTokens(est, string(runes))
}

// contentTokenBudget is the token room for message text. EstimateMessage adds
// 4 for the message frame; that frame is not part of the span.
func contentTokenBudget(maxChunkTokens int) int {
	room := maxChunkTokens - 4
	if room < 1 {
		room = 1
	}
	return room
}

func contentTokens(est Estimator, text string) int {
	if est == nil {
		return utf8.RuneCountInString(text)
	}
	n := est.Estimate(text)
	if n < 0 {
		return 0
	}
	return n
}

func SelectInput(raw []Message, est Estimator, retainTurns, targetTailTokens int) ([]Message, []Message) {
	if len(raw) == 0 {
		return nil, nil
	}
	if retainTurns <= 0 && targetTailTokens <= 0 {
		return append([]Message(nil), raw...), nil
	}

	turnStarts := make([]int, 0, len(raw)/2+1)
	for i, msg := range raw {
		if msg.Role == "user" {
			turnStarts = append(turnStarts, i)
		}
	}
	turnStarts = append(turnStarts, len(raw))

	protectedStart := 0
	if retainTurns > 0 && len(turnStarts) > 1 {
		userTurns := len(turnStarts) - 1
		if userTurns > retainTurns {
			protectedStart = turnStarts[userTurns-retainTurns]
		}
	}

	cut := protectedStart
	if targetTailTokens > 0 {
		cut = -1
		for _, candidate := range turnStarts {
			if candidate > protectedStart {
				break
			}
			if EstimateMessages(est, raw[candidate:]) <= targetTailTokens {
				cut = candidate
				break
			}
		}
		// A single protected turn can exceed the target. In that case retention
		// is soft: advance only at complete user-turn boundaries.
		if cut < 0 {
			for _, candidate := range turnStarts {
				if candidate <= protectedStart {
					continue
				}
				if EstimateMessages(est, raw[candidate:]) <= targetTailTokens {
					cut = candidate
					break
				}
			}
		}
	}
	if cut < 0 {
		cut = len(raw)
	}
	if cut <= 0 {
		return nil, append([]Message(nil), raw...)
	}
	return append([]Message(nil), raw[:cut]...), append([]Message(nil), raw[cut:]...)
}

// EstimateMessages preserves message overhead and reasoning content in budgets.
func EstimateMessages(est Estimator, messages []Message) int {
	if est == nil {
		return 0
	}
	total := 0
	for _, msg := range messages {
		total += EstimateMessage(est, msg.Content, msg.ReasoningContent)
	}
	return total
}

// EstimateMessage lets adapters budget their native messages without allocating
// an intermediate compaction message slice.
func EstimateMessage(est Estimator, content, reasoning string) int {
	if est == nil {
		return 0
	}
	total := est.Estimate(content) + 4
	if reasoning != "" {
		total += est.Estimate(reasoning)
	}
	return total
}
