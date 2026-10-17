package compact

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
	return chunks
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
