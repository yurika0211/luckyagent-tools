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

// ChunkOverlapRunes is how many runes a later window repeats from the previous
// cut, and only when that repeat still fits the token budget. It is not a
// minimum window and it is not grown to swallow an unsplittable span.
const ChunkOverlapRunes = 128

// splitOversizedChunkMessages breaks one oversized message on whitespace or
// punctuation. A continuous span that itself exceeds the chunk token budget
// is left intact and marked with ErrTranscriptBudget by SplitChunksChecked.
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
			window := []Message{part}
			// The +4 message frame in EstimateMessage is not text. Reject only
			// when the body itself exceeds the caller's token budget.
			if contentTokens(est, part.Content) > maxChunkTokens {
				return chunks, fmt.Errorf("%w", ErrTranscriptBudget)
			}
			out = append(out, window)
		}
	}
	return out, nil
}

func splitMessageWindows(msg Message, maxChunkTokens int, est Estimator) ([]Message, error) {
	runes := []rune(msg.Content)
	if len(runes) == 0 {
		return []Message{msg}, nil
	}
	if unsplittableSpanOverBudget(runes, maxChunkTokens, est) {
		return nil, fmt.Errorf("%w", ErrTranscriptBudget)
	}
	var parts []Message
	start := 0
	for start < len(runes) {
		end := fitWindowEnd(runes, start, maxChunkTokens, est)
		if end <= start {
			return nil, fmt.Errorf("%w", ErrTranscriptBudget)
		}
		part := msg
		part.Content = string(runes[start:end])
		if contentTokens(est, part.Content) > contentTokenBudget(maxChunkTokens) {
			return nil, fmt.Errorf("%w", ErrTranscriptBudget)
		}
		parts = append(parts, part)
		if end >= len(runes) {
			break
		}
		next := end
		if overlap := overlapRunes(est, maxChunkTokens, string(runes[start:end])); overlap > 0 && end-overlap > start {
			next = end - overlap
		}
		if next <= start {
			return nil, fmt.Errorf("%w", ErrTranscriptBudget)
		}
		start = next
	}
	return parts, nil
}

// fitWindowEnd returns the largest end in (start, len] whose slice fits the
// token budget. When the raw budget cut lands inside a continuous span, the
// cut moves back to the span start if that span still fits in a later window.
// The cut is never moved forward past the budget.
func fitWindowEnd(runes []rune, start, maxChunkTokens int, est Estimator) int {
	lo, hi := start+1, len(runes)
	best := start
	for lo <= hi {
		mid := (lo + hi) / 2
		if estimateRange(est, runes[start:mid]) <= contentTokenBudget(maxChunkTokens) {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best <= start {
		return start
	}
	if best >= len(runes) {
		return len(runes)
	}
	spanStart := continuousSpanStart(runes, best)
	if spanStart > start && estimateRange(est, runes[start:spanStart]) <= contentTokenBudget(maxChunkTokens) && estimateRange(est, runes[start:spanStart]) > 0 {
		return spanStart
	}
	return best
}

// continuousSpanStart is the start of the separator-free span that contains
// index. Separators are whitespace and punctuation, including CJK punctuation.
// Letters, digits, and marks stay inside the span, so a path or a long word
// is one span.
func continuousSpanStart(runes []rune, index int) int {
	if index <= 0 {
		return 0
	}
	if index > len(runes) {
		index = len(runes)
	}
	i := index
	if i == len(runes) || isSpanSeparator(runes[i]) {
		i--
	}
	for i > 0 && !isSpanSeparator(runes[i]) {
		i--
	}
	if isSpanSeparator(runes[i]) {
		return i + 1
	}
	return i
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

// overlapRunes repeats up to ChunkOverlapRunes, but only while the next
// window can still hold that repeat inside the token budget. A zero result
// means the next window starts at the cut.
func overlapRunes(est Estimator, maxChunkTokens int, window string) int {
	runes := []rune(window)
	n := ChunkOverlapRunes
	if n > len(runes) {
		n = len(runes)
	}
	for n > 0 && estimateRange(est, runes[len(runes)-n:]) >= contentTokenBudget(maxChunkTokens) {
		n--
	}
	// Keep one rune of progress even when the overlap would consume the window.
	if n >= len(runes) {
		return len(runes) - 1
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
