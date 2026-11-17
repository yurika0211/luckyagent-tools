package compact

import "unicode/utf8"

func SplitChunks(messages []Message, est Estimator, maxChunkTokens int) [][]Message {
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
		// Flush everything before this turn when the running chunk overflowed.
		prevEnd := turnStarts[t]
		if prevEnd > chunkStart {
			chunks = append(chunks, append([]Message(nil), messages[chunkStart:prevEnd]...))
			chunkStart = prevEnd
			candidate = messages[chunkStart:turnEnd]
		}
		// Single turn still over budget: emit it alone and continue.
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
	return splitOversizedChunkMessages(chunks, est, maxChunkTokens)
}

// ChunkOverlapRunes is the fixed overlap between windows of one oversized
// message. It is not extended to cover a detected span. A fact longer than
// this overlap can sit in one window; a fact that straddles the cut is copied
// into the next window only for these runes.
const ChunkOverlapRunes = 128

// splitOversizedChunkMessages breaks a message that is still over the chunk
// token budget into ordered rune windows. A chunk that holds several messages
// is left whole: SplitChunks already isolated an oversized turn before this
// step. Tool calls travel with every window of that message so a call stays
// paired with the body slice. The added input is only the overlap copies.
func splitOversizedChunkMessages(chunks [][]Message, est Estimator, maxChunkTokens int) [][]Message {
	if maxChunkTokens <= 0 {
		return chunks
	}
	var out [][]Message
	for _, chunk := range chunks {
		if len(chunk) != 1 || EstimateMessages(est, chunk) <= maxChunkTokens {
			out = append(out, chunk)
			continue
		}
		parts := splitMessageWindows(chunk[0], maxChunkTokens, est)
		if len(parts) <= 1 {
			out = append(out, chunk)
			continue
		}
		for _, part := range parts {
			out = append(out, []Message{part})
		}
	}
	return out
}

func splitMessageWindows(msg Message, maxChunkTokens int, est Estimator) []Message {
	runes := []rune(msg.Content)
	window := chunkRuneWindow(msg.Content, maxChunkTokens, est)
	if window >= len(runes) || len(runes) == 0 {
		return []Message{msg}
	}
	if window < ChunkOverlapRunes*2 {
		window = ChunkOverlapRunes * 2
	}
	var parts []Message
	for start := 0; start < len(runes); {
		end := start + window
		if end > len(runes) {
			end = len(runes)
		} else if snapped := snapWindowEnd(runes, start, end); snapped > start {
			end = snapped
		}
		part := msg
		part.Content = string(runes[start:end])
		parts = append(parts, part)
		if end >= len(runes) {
			break
		}
		next := end - ChunkOverlapRunes
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return parts
}

// snapWindowEnd moves a cut back to a newline inside the fixed overlap.
// The window only shrinks. A line longer than ChunkOverlapRunes can still
// be split; the overlap then carries that many runes into the next window.
func snapWindowEnd(runes []rune, start, end int) int {
	searchFrom := end - ChunkOverlapRunes
	if searchFrom < start {
		searchFrom = start
	}
	for i := end - 1; i >= searchFrom; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}
	return end
}

// chunkRuneWindow converts a token budget into runes using the same estimator
// the chunker used. The result never exceeds TranscriptMaxRunes, so one window
// cannot hand Transcript an unbounded message. It is not grown to fit a span.
func chunkRuneWindow(content string, maxChunkTokens int, est Estimator) int {
	runes := utf8.RuneCountInString(content)
	window := TranscriptMaxRunes
	if est != nil && runes > 0 {
		tokens := est.Estimate(content)
		if tokens > 0 {
			room := maxChunkTokens - 4
			if room < 1 {
				room = 1
			}
			window = runes * room / tokens
		}
	} else if maxChunkTokens > 0 && maxChunkTokens < window {
		window = maxChunkTokens
	}
	// Leave room for the role label so the rendered line still fits
	// TranscriptMaxRunes. The window is not grown past this cap.
	const transcriptFrameRunes = 256
	if window > TranscriptMaxRunes-transcriptFrameRunes {
		window = TranscriptMaxRunes - transcriptFrameRunes
	}
	if window < 1 {
		window = 1
	}
	return window
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
