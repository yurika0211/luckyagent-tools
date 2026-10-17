package compact

import (
	"fmt"
	"strings"
	"testing"
)

func TestSliceCompactInputChunksRespectsTurnBoundaries(t *testing.T) {
	est := EstimatorFunc(func(text string) int { return (len(text) + 3) / 4 })
	var raw []Message
	for i := 0; i < 6; i++ {
		raw = append(raw,
			Message{Role: "user", Content: fmt.Sprintf("goal-%d %s", i, strings.Repeat("context ", 80))},
			Message{Role: "assistant", Content: fmt.Sprintf("answer-%d %s", i, strings.Repeat("result ", 80))},
			Message{Role: "tool", Name: "terminal", Content: strings.Repeat("go test output ", 40)},
		)
	}
	full := EstimateMessages(est, raw)
	maxChunk := full / 3
	if maxChunk < 100 {
		maxChunk = 100
	}
	chunks := SplitChunks(raw, est, maxChunk)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for oversized input, got %d (full=%d max=%d)", len(chunks), full, maxChunk)
	}
	joined := 0
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Fatalf("chunk %d empty", i)
		}
		if chunk[0].Role != "user" && i > 0 {
			// first chunk may start mid-history only if raw had no leading user; ours always does
			if raw[0].Role == "user" && chunk[0].Role != "user" {
				t.Fatalf("chunk %d must start at user turn, got role=%s", i, chunk[0].Role)
			}
		}
		joined += len(chunk)
	}
	if joined != len(raw) {
		t.Fatalf("chunks must cover full input: joined=%d raw=%d", joined, len(raw))
	}
}

func TestSliceCompactInputChunksSingleWhenSmall(t *testing.T) {
	est := EstimatorFunc(func(text string) int { return (len(text) + 3) / 4 })
	raw := []Message{
		{Role: "user", Content: "short goal"},
		{Role: "assistant", Content: "short answer"},
	}
	chunks := SplitChunks(raw, est, 12000)
	if len(chunks) != 1 || len(chunks[0]) != 2 {
		t.Fatalf("expected one chunk, got %+v", chunks)
	}
}

func TestMergeCompactSummariesLocalKeepsLatestGoal(t *testing.T) {
	segs := []string{
		"### Segment 1/2\nCurrent user goal:\n- old goal about alpha.go\nCompleted work:\n- did alpha\nPending work:\n- more alpha\nKey files and functions:\n- alpha.go\nCommands and test results:\n- go test alpha passed\nUser constraints:\n- keep tests\nUncertain facts:\n- none\n",
		"### Segment 2/2\nCurrent user goal:\n- new goal about beta.go\nCompleted work:\n- did beta\nPending work:\n- more beta\nKey files and functions:\n- beta.go\nCommands and test results:\n- go test beta passed\nUser constraints:\n- keep tests\nUncertain facts:\n- check beta\n",
	}
	out := MergeLocal(segs, TextOptions{})
	if !strings.Contains(out, "new goal about beta.go") {
		t.Fatalf("expected latest goal, got:\n%s", out)
	}
	if !strings.Contains(out, "alpha.go") || !strings.Contains(out, "beta.go") {
		t.Fatalf("expected both file refs:\n%s", out)
	}
}
