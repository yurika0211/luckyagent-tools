package compact

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// SummaryTimeout is the hard cap for the LLM summarizer call.
	// Parent turn deadlines are not inherited when they would cut this short;
	// explicit parent cancellation still aborts before the call starts.
	SummaryTimeout = 45 * time.Second

	// TranscriptMaxRunes bounds the text sent to the summarizer.
	// Unbounded per-message dumps were the main cause of
	// "generate summary: context deadline exceeded" on long sessions.
	TranscriptMaxRunes = 48_000

	compactTranscriptUserRunes      = 420
	compactTranscriptAssistantRunes = 300
	compactTranscriptToolRunes      = 220
	compactTranscriptDefaultRunes   = 260

	// compactToolArgumentRunes caps one rendered tool argument so a large JSON
	// blob handed in through ToolCall.Arguments cannot refill the transcript.
	compactToolArgumentRunes = 160

	// defaultReasoningSkipDelta is the Δ(r,o) above which an observed reasoning
	// block is replaced by [SKIP]. A caller overrides it with ReasoningSkipDelta.
	defaultReasoningSkipDelta = 0.5
)

// SummaryContext builds the context used for the compaction LLM call.
// It keeps request-scoped values, drops an already-tight parent deadline so the
// summarizer still gets a full budget, and fails fast if the parent is already
// canceled (user abort / process shutdown).
func SummaryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		dead, cancel := context.WithCancel(ctx)
		cancel()
		return dead, func() {}
	}
	base := ctx
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) < SummaryTimeout {
			base = context.WithoutCancel(ctx)
		}
	}
	summary, cancel := context.WithTimeout(base, SummaryTimeout)
	// Extending a short deadline must not detach an explicit user cancellation.
	stop := context.AfterFunc(ctx, func() {
		if ctx.Err() == context.Canceled {
			cancel()
		}
	})
	return summary, func() { stop(); cancel() }
}

// Transcript renders session messages into a budgeted plain-text transcript for
// the compaction summarizer. Each line is scored by how hard it would be to
// rebuild: lines that carry a path, URL, command, error head, commit, value, or
// diff hunk are kept, and blank, repeated, and progress lines are dropped
// first. Retained lines stay in their original order. A folded message keeps a
// replay pointer (its session index or external ref) so the model can point
// back at the source instead of inventing it.
func Transcript(messages []Message, opts TextOptions) string {
	budget := TranscriptMaxRunes
	if opts.TranscriptBudget > 0 {
		budget = opts.TranscriptBudget
	}
	lines := renderTranscriptLines(messages, opts)
	if len(lines) == 0 {
		return ""
	}
	total := 0
	for _, line := range lines {
		total += utf8.RuneCountInString(line) + 1
	}
	if total <= budget {
		return strings.Join(lines, "\n")
	}
	// The whole transcript is one block, so a line that only looks like an
	// identifier because it is a rendered message must still be droppable.
	// Per-message scoring already happened in renderTranscriptLines; here the
	// only job is to meet the budget, keeping the tail because that is the
	// most recent evidence.
	joined := strings.Join(lines, "\n")
	fitted := fitTranscript(joined, budget)
	if utf8.RuneCountInString(fitted) > budget {
		note := "[compact transcript: protected lines exceed the budget and were kept intact]"
		if utf8.RuneCountInString(note)+1+utf8.RuneCountInString(fitted) <= budget {
			return note + "\n" + fitted
		}
		// The note itself would push a protected block further over the budget,
		// so the block stands alone. Callers still see it is intact.
		return fitted
	}
	return fitted
}

// renderTranscriptLines renders one transcript line per message, skipping
// compact boundaries and messages with nothing to say.
func renderTranscriptLines(messages []Message, opts TextOptions) []string {
	lines := make([]string, 0, len(messages))
	for i, msg := range messages {
		if line := formatCompactTranscriptLine(msg, messages, i, opts); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func formatCompactTranscriptLine(msg Message, messages []Message, position int, opts TextOptions) string {
	if msg.CompactBoundary {
		return ""
	}
	content := strings.TrimSpace(msg.Content)
	reasoning := renderReasoning(msg, messages, position, opts)
	calls := renderToolCalls(msg.ToolCalls)
	if content == "" && reasoning == "" && calls == "" {
		return ""
	}

	role := strings.ToUpper(strings.TrimSpace(msg.Role))
	if role == "" {
		role = "MESSAGE"
	}

	var header string
	switch strings.ToLower(strings.TrimSpace(msg.Role)) {
	case "tool":
		header, content = renderToolContent(msg.Name, content, opts)
	case "user":
		content = fitRoleContent(content, compactTranscriptUserRunes)
	case "assistant":
		content = fitRoleContent(content, compactTranscriptAssistantRunes)
	default:
		content = fitRoleContent(content, compactTranscriptDefaultRunes)
	}

	var b strings.Builder
	b.WriteString(role)
	if name := strings.TrimSpace(msg.Name); name != "" {
		b.WriteString("(" + name + ")")
	}
	if pointer := replayPointer(msg); pointer != "" {
		b.WriteString(" ")
		b.WriteString(pointer)
	}
	b.WriteString(": ")
	if header != "" {
		b.WriteString(header)
		if content != "" || reasoning != "" {
			b.WriteString("\n")
		}
	}
	if content != "" {
		b.WriteString(content)
	}
	if reasoning != "" {
		if content != "" {
			b.WriteString("\n")
		}
		b.WriteString(reasoning)
	}
	if calls != "" {
		b.WriteString(" ")
		b.WriteString(calls)
	}
	return b.String()
}

// renderToolContent keeps a SummarizeTool result as a header line and lets
// CompressTool, or the built-in line fitter, decide the body. A summary no
// longer replaces the body, so a one-line description cannot drop the path or
// error that only appears part way through the result.
func renderToolContent(name, content string, opts TextOptions) (string, string) {
	budget := compactTranscriptToolRunes
	if opts.ToolRunes > 0 {
		budget = opts.ToolRunes
	}
	header := ""
	if opts.SummarizeTool != nil {
		header = strings.TrimSpace(opts.SummarizeTool(name, content))
	}
	if opts.CompressTool != nil {
		content = opts.CompressTool(name, content, budget)
	}
	content = fitRoleContent(content, budget)
	return header, content
}

// fitRoleContent applies the per-role rune cap. A block inside the cap is
// returned unchanged; a block past it is reduced by line score, and a single
// oversized line is cut in the middle.
func fitRoleContent(content string, budget int) string {
	if budget <= 0 || content == "" {
		return ""
	}
	if utf8.RuneCountInString(content) <= budget {
		return content
	}
	fitted, _ := fitLines(content, budget)
	if utf8.RuneCountInString(fitted) <= budget {
		return fitted
	}
	return truncateMiddle(fitted, budget)
}

// replayPointer names where a folded message can be read back. An external ref
// wins over a session index, and a zero index stays silent so unset messages
// do not all claim to be message #0.
func replayPointer(msg Message) string {
	if ref := strings.TrimSpace(msg.Ref); ref != "" {
		return "ref=" + ref
	}
	if msg.Index > 0 {
		return fmt.Sprintf("#%d", msg.Index)
	}
	return ""
}

// renderToolCalls renders a call as [tool_calls=name(args),...]. Arguments are
// capped per call; a call with no arguments keeps the bare name, matching the
// v0.2.3 rendering.
func renderToolCalls(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		args := strings.TrimSpace(call.Arguments)
		if args == "" {
			rendered = append(rendered, name)
			continue
		}
		if utf8.RuneCountInString(args) > compactToolArgumentRunes {
			args = truncateMiddle(args, compactToolArgumentRunes)
		}
		rendered = append(rendered, name+"("+args+")")
	}
	if len(rendered) == 0 {
		return fmt.Sprintf("[tool_calls=%d]", len(calls))
	}
	return fmt.Sprintf("[tool_calls=%s]", strings.Join(rendered, ","))
}

// renderReasoning renders an assistant's reasoning only when the caller
// supplied a scoring model. Without one the step is skipped entirely, so a
// rule cannot invent a [SKIP]. A block whose Δ(r,o) exceeds the threshold is
// replaced by [SKIP], except when it is the only block in the sequence.
func renderReasoning(msg Message, messages []Message, position int, opts TextOptions) string {
	if opts.ReasoningScore == nil {
		return ""
	}
	reasoning := strings.TrimSpace(msg.ReasoningContent)
	if reasoning == "" || strings.ToLower(strings.TrimSpace(msg.Role)) != "assistant" {
		return ""
	}
	observation := observationAfter(messages, position)
	if observation == "" {
		return renderReasoningBlocks(splitReasoningBlocks(reasoning), nil)
	}
	blocks := splitReasoningBlocks(reasoning)
	if len(blocks) == 0 {
		return ""
	}
	threshold := opts.ReasoningSkipDelta
	if threshold <= 0 {
		threshold = defaultReasoningSkipDelta
	}
	prior := make([]string, 0, len(blocks))
	rendered := make([]string, len(blocks))
	scored := make([]float64, len(blocks))
	haveScore := make([]bool, len(blocks))
	for i, block := range blocks {
		delta, ok := opts.ReasoningScore(block, prior, observation)
		scored[i], haveScore[i] = delta, ok
		prior = append(prior, block)
	}
	skippable := 0
	for i := range blocks {
		if haveScore[i] && scored[i] > threshold {
			skippable++
		}
	}
	// A sequence of one block is never skipped: replacing it would erase the
	// reasoning rather than compress it. The same rule keeps the last block
	// when every block scores above the threshold.
	skipAll := skippable == len(blocks)
	for i, block := range blocks {
		lastOfAllSkipped := skipAll && i == len(blocks)-1
		if haveScore[i] && scored[i] > threshold && len(blocks) > 1 && !lastOfAllSkipped {
			rendered[i] = "[SKIP]"
			continue
		}
		rendered[i] = block
	}
	return renderReasoningBlocks(rendered, nil)
}

// splitReasoningBlocks splits reasoning on blank lines. A block is one unit of
// reasoning the scorer can keep or replace.
func splitReasoningBlocks(reasoning string) []string {
	parts := strings.Split(reasoning, "\n\n")
	blocks := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		blocks = append(blocks, part)
	}
	return blocks
}

// observationAfter returns the text of the next non-boundary message, which is
// the observation the reasoning was written before. An empty result means the
// reasoning was never followed by an observation, so it cannot be scored.
func observationAfter(messages []Message, position int) string {
	for i := position + 1; i < len(messages); i++ {
		if messages[i].CompactBoundary {
			continue
		}
		if text := strings.TrimSpace(messages[i].Content); text != "" {
			return text
		}
	}
	return ""
}

// renderReasoningBlocks joins scored blocks back into one reasoning section.
func renderReasoningBlocks(blocks []string, _ []int) string {
	kept := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		kept = append(kept, block)
	}
	if len(kept) == 0 {
		return ""
	}
	return "[reasoning]\n" + strings.Join(kept, "\n\n")
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	b.Grow(maxRunes * 4)
	n := 0
	for _, r := range s {
		if n >= maxRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
