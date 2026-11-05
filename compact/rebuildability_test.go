package compact

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// These tests are the acceptance checks from luckyagent-tools#11. Each one
// asserts a property of the transcript rule instead of one fixed string.

func TestTranscriptKeepsReplayPointerOnFoldedMessage(t *testing.T) {
	body := strings.Repeat("progress step without payload\n", 80) +
		"config written to /home/dev/workspace/lucky-agent/config.example.json\n"
	out := Transcript([]Message{{
		Role:    "tool",
		Name:    "terminal",
		Index:   17,
		Content: body,
	}}, TextOptions{ToolRunes: 220})

	if !strings.Contains(out, "#17") {
		t.Fatalf("folded message lost its replay index:\n%s", out)
	}
	if !strings.Contains(out, "config.example.json") {
		t.Fatalf("folded message lost the identifier it pointed at:\n%s", out)
	}

	byRef := Transcript([]Message{{
		Role:    "tool",
		Name:    "terminal",
		Ref:     "msg-17",
		Content: body,
	}}, TextOptions{ToolRunes: 220})
	if !strings.Contains(byRef, "ref=msg-17") {
		t.Fatalf("folded message lost its external ref:\n%s", byRef)
	}
}

func TestTranscriptRendersToolCallArgumentsAndCapsThem(t *testing.T) {
	long := strings.Repeat("abcdefg ", 80)
	out := Transcript([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			Name:      "terminal",
			Arguments: `{"command":"` + long + `"}`,
		}},
	}}, TextOptions{})

	if !strings.Contains(out, "[tool_calls=terminal(") {
		t.Fatalf("tool call name and arguments were not rendered:\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Fatal("oversized tool arguments were rendered whole")
	}
	if !strings.Contains(out, "middle omitted") {
		t.Fatalf("oversized arguments were dropped instead of truncated:\n%s", out)
	}
}

func TestTranscriptHonorsToolRunes(t *testing.T) {
	body := strings.Repeat("one ordinary status line\n", 40)
	const budget = 120
	out := Transcript([]Message{{Role: "tool", Name: "terminal", Content: body}}, TextOptions{ToolRunes: budget})

	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "TOOL(") && !strings.Contains(line, "status line") && !strings.Contains(line, "omitted") {
			continue
		}
		if got := utf8.RuneCountInString(line); strings.HasPrefix(line, "TOOL(") && got > budget+len("TOOL(terminal): ") {
			t.Fatalf("tool message exceeded ToolRunes: %d runes", got)
		}
	}
	bodyRunes := utf8.RuneCountInString(strings.TrimPrefix(out, "TOOL(terminal): "))
	if bodyRunes > budget {
		t.Fatalf("tool body is %d runes, budget %d:\n%s", bodyRunes, budget, out)
	}
}

func TestSummarizeToolIsHeaderNotBody(t *testing.T) {
	body := "error: dial tcp 10.0.0.7:8443: connection refused\nelapsed=4213ms"
	out := Transcript([]Message{{Role: "tool", Name: "terminal", Content: body}}, TextOptions{
		SummarizeTool: func(name, result string) string { return "ran a command" },
	})

	if !strings.Contains(out, "ran a command") {
		t.Fatalf("summary header missing:\n%s", out)
	}
	if !strings.Contains(out, "connection refused") || !strings.Contains(out, "4213") {
		t.Fatalf("summary replaced the body:\n%s", out)
	}
	headerAt := strings.Index(out, "ran a command")
	bodyAt := strings.Index(out, "connection refused")
	if headerAt > bodyAt {
		t.Fatalf("summary is not a header:\n%s", out)
	}
}

func TestTranscriptDefaultShapeMatchesV023(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Fix internal/agent/context_planner.go"},
		{Role: "assistant", Content: "Updating planner compact boundary handling.", ToolCalls: []ToolCall{{Name: "terminal"}}},
		{Role: "tool", Name: "terminal", Content: "go test ./internal/agent passed"},
	}
	out := Transcript(messages, TextOptions{})

	for _, want := range []string{
		"USER: Fix internal/agent/context_planner.go",
		"ASSISTANT: Updating planner compact boundary handling. [tool_calls=terminal]",
		"TOOL(terminal): go test ./internal/agent passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("default transcript changed shape, missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[reasoning]") || strings.Contains(out, "[SKIP]") {
		t.Fatalf("reasoning leaked into a transcript with no scoring model:\n%s", out)
	}
}

func TestTranscriptDropsByScoreNotPosition(t *testing.T) {
	var lines []string
	lines = append(lines, "name | status | note")
	for i := 0; i < 30; i++ {
		lines = append(lines, "name | status | note", "", "loading...", "50%")
	}
	lines = append(lines,
		"config written to /home/dev/workspace/lucky-agent/config.example.json",
		"another uninteresting status line",
		"error: dial tcp 10.0.0.7:8443: connection refused",
		"elapsed=4213ms",
	)
	for i := 0; i < 30; i++ {
		lines = append(lines, "trailing chatter")
	}
	out := Transcript([]Message{{Role: "tool", Name: "terminal", Content: strings.Join(lines, "\n")}}, TextOptions{ToolRunes: 400})

	body := strings.TrimPrefix(out, "TOOL(terminal): ")
	if utf8.RuneCountInString(body) > 400 {
		t.Fatalf("tool body exceeds budget: %d runes", utf8.RuneCountInString(body))
	}
	previous := -1
	for _, want := range []string{
		"/home/dev/workspace/lucky-agent/config.example.json",
		"error: dial tcp 10.0.0.7:8443: connection refused",
		"elapsed=4213ms",
	} {
		at := strings.Index(body, want)
		if at < 0 {
			t.Fatalf("middle identifier dropped: %q\n%s", want, body)
		}
		if at < previous {
			t.Fatalf("retained lines were reordered around %q", want)
		}
		previous = at
	}
	if strings.Count(body, "name | status | note") > 1 {
		t.Fatalf("repeated header outranked the middle content:\n%s", body)
	}
}

func TestTranscriptRetainedLinesKeepOrder(t *testing.T) {
	original := make([]string, 0, 40)
	for i := 0; i < 10; i++ {
		original = append(original, "filler line", "", "go test ./internal/agent", "note")
	}
	out := Transcript([]Message{{Role: "tool", Name: "tool", Content: strings.Join(original, "\n")}}, TextOptions{
		TranscriptBudget: 600,
		ToolRunes:        160,
	})

	// A retained line is either an original line or a shortened copy of one,
	// and it must appear no earlier than its source. That is the subsequence
	// property once shrinking is allowed.
	position := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimPrefix(line, "TOOL(tool): ")
		if strings.Contains(line, "omitted") || strings.HasPrefix(strings.TrimSpace(line), "n transcript)") {
			continue
		}
		found := false
		for ; position < len(original); position++ {
			if original[position] == line || strings.HasPrefix(original[position], strings.TrimSpace(strings.Split(line, "...")[0])) {
				found = true
				position++
				break
			}
		}
		if !found {
			t.Fatalf("retained line is not in order: %q", line)
		}
	}
	if !strings.Contains(out, "omitted for context") {
		t.Fatalf("over-budget result dropped lines without recording it:\n%s", out)
	}
}

func TestTranscriptProtectedOverflowStaysIntact(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, "error: failed to write /tmp/build/output-"+strings.Repeat("x", 40)+".log")
	}
	body := strings.Join(lines, "\n")
	out := Transcript([]Message{{Role: "tool", Name: "terminal", Content: body}}, TextOptions{ToolRunes: 80})

	for _, line := range lines {
		if !strings.Contains(out, line) && !strings.Contains(out, "/tmp/build/output-") {
			t.Fatalf("protected line was discarded to meet the budget:\n%s", out)
		}
	}
}

func TestValidateRejectsDroppedIdentifierAndNamesIt(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "修复构建失败。"},
		{Role: "tool", Name: "terminal", Content: "config written to /home/dev/workspace/lucky-agent/agent/compact_adapter.go\nerror: undefined: compactlib.Validate\nelapsed=4213ms"},
	}
	required := Identifiers(messages)
	positive := "当前用户目标：修复构建失败，并确认 compact 不再丢掉源消息里的标识符。已确认改动落在 /home/dev/workspace/lucky-agent/agent/compact_adapter.go，命令报错 error: undefined: compactlib.Validate，耗时 elapsed=4213ms。\n\npending work: rerun the suite."
	if got := Validate(positive, messages, ValidateOptions{RequiredIdentifiers: required}); !got.Valid {
		t.Fatalf("positive sample rejected: %s", got.Reason)
	}

	negative := strings.ReplaceAll(positive, "elapsed=4213ms", "elapsed measured")
	got := Validate(negative, messages, ValidateOptions{RequiredIdentifiers: required})
	if got.Valid {
		t.Fatal("negative sample accepted after dropping a source identifier")
	}
	if !strings.Contains(got.Reason, "4213") {
		t.Fatalf("failure does not name the dropped identifier: %q", got.Reason)
	}
}

func TestValidateWithoutIdentifiersKeepsShapeChecksOnly(t *testing.T) {
	messages := []Message{{Role: "tool", Name: "terminal", Content: "error: undefined: compactlib.Validate"}}
	summary := "当前用户目标：修复构建失败，并确认 compact 在没有标识符清单时仍只做形状检查，不额外要求召回。\n\npending work: rerun the suite. 已确认命令报错，见 agent/compact_adapter.go，改动保持最小。"
	if got := Validate(summary, messages); !got.Valid {
		t.Fatalf("shape-only validation started requiring identifiers: %s", got.Reason)
	}
}

// TestValidatorStrength measures the identifier check instead of assuming it.
// Three negatives each drop one identifier; three positives only reword.
// Samples: 6, constructed by substitution on one source summary. sigma is the
// fraction of samples the check classifies the way the construction says.
func TestValidatorStrength(t *testing.T) {
	messages := []Message{
		{Role: "tool", Name: "terminal", Content: "wrote /home/dev/workspace/lucky-agent/agent/compact_adapter.go\nerror: undefined: compactlib.Validate\nelapsed=4213ms"},
	}
	required := Identifiers(messages)
	base := "当前用户目标：修复构建失败，并确认 compact 不再丢掉源消息里的标识符。已确认改动落在 /home/dev/workspace/lucky-agent/agent/compact_adapter.go，报错 error: undefined: compactlib.Validate，耗时 elapsed=4213ms。\n\npending work: rerun the suite."

	negatives := []string{
		strings.ReplaceAll(base, "4213", "measured"),
		strings.ReplaceAll(base, "/home/dev/workspace/lucky-agent/agent/compact_adapter.go", "the adapter file"),
		strings.ReplaceAll(base, "compactlib.Validate", "the validator"),
	}
	positives := []string{
		base,
		strings.ReplaceAll(base, "已确认改动落在", "变更位置为"),
		strings.ReplaceAll(base, "修复构建失败", "修好构建异常"),
	}

	agree := 0
	total := len(negatives) + len(positives)
	for _, sample := range negatives {
		if !Validate(sample, messages, ValidateOptions{RequiredIdentifiers: required}).Valid {
			agree++
		}
	}
	for _, sample := range positives {
		if Validate(sample, messages, ValidateOptions{RequiredIdentifiers: required}).Valid {
			agree++
		}
	}
	sigma := float64(agree) / float64(total)
	t.Logf("validator strength sigma=%.2f over %d samples (%d negatives dropping one identifier each, %d positives rewording only)", sigma, total, len(negatives), len(positives))
	if sigma < 1.0 {
		t.Fatalf("validator strength regressed: sigma=%.2f", sigma)
	}
}

func TestReasoningCompressionRequiresAScorer(t *testing.T) {
	messages := []Message{{
		Role:             "assistant",
		Content:          "running the test",
		ReasoningContent: "first guess about the failure\n\nsecond guess that the observation confirms",
	}, {
		Role:    "tool",
		Content: "go test ./internal/agent passed",
	}}

	plain := Transcript(messages, TextOptions{})
	if strings.Contains(plain, "[SKIP]") || strings.Contains(plain, "[reasoning]") {
		t.Fatalf("reasoning was rendered without a scoring model:\n%s", plain)
	}
	if !strings.Contains(plain, "running the test") {
		t.Fatalf("assistant content changed:\n%s", plain)
	}

	scored := Transcript(messages, TextOptions{ReasoningScore: func(block string, prior []string, observation string) (float64, bool) {
		if strings.Contains(block, "first guess") {
			return 0.9, true
		}
		return 0.1, true
	}})
	if !strings.Contains(scored, "[SKIP]") {
		t.Fatalf("low-information block was kept:\n%s", scored)
	}
	if !strings.Contains(scored, "second guess") {
		t.Fatalf("high-information block was dropped:\n%s", scored)
	}
	if !strings.Contains(scored, "go test ./internal/agent passed") {
		t.Fatalf("observation changed across reasoning compression:\n%s", scored)
	}
}

func TestReasoningSingleBlockIsNeverSkipped(t *testing.T) {
	out := Transcript([]Message{{
		Role:             "assistant",
		Content:          "done",
		ReasoningContent: "the only block",
	}, {
		Role:    "tool",
		Content: "ok",
	}}, TextOptions{ReasoningScore: func(string, []string, string) (float64, bool) {
		return 1, true
	}})
	if strings.Contains(out, "[SKIP]") || !strings.Contains(out, "the only block") {
		t.Fatalf("the only reasoning block was skipped:\n%s", out)
	}
}

func TestReasoningScorerRefusalSkipsTheStep(t *testing.T) {
	out := Transcript([]Message{{
		Role:             "assistant",
		Content:          "done",
		ReasoningContent: "unscored block",
	}, {
		Role:    "tool",
		Content: "ok",
	}}, TextOptions{ReasoningScore: func(string, []string, string) (float64, bool) {
		return 0, false
	}})
	if strings.Contains(out, "[SKIP]") {
		t.Fatalf("a refusing scorer still produced [SKIP]:\n%s", out)
	}
	if !strings.Contains(out, "unscored block") {
		t.Fatalf("unscored reasoning was dropped:\n%s", out)
	}
}
