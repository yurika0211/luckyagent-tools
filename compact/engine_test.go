package compact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testSummary(goal string) string {
	return "Current user goal:\n- " + goal + "\nCompleted work:\n- inspected internal/example.go\nPending work:\n- finish and verify the requested change\nKey files and functions:\n- internal/example.go\nCommands and test results:\n- go test ./... passed\nUser constraints:\n- preserve the latest request\nUncertain facts:\n- verify the current workspace before continuing\n"
}

func testHistory() []Message {
	var messages []Message
	for i := 0; i < 5; i++ {
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("goal-%d %s", i, strings.Repeat("context ", 40))}, Message{Role: "assistant", Content: strings.Repeat("result ", 40)})
	}
	return messages
}

func TestParallelChunksStayOrderedAndWithinLimit(t *testing.T) {
	var active, peak atomic.Int32
	messages := testHistory()
	engine := New(Config{MaxChunkTokens: 250, MaxParallel: 2, Estimator: EstimatorFunc(func(text string) int { return len(text) }), Chat: func(ctx context.Context, request ChatRequest) (string, error) {
		if request.Kind == "merge" {
			previous := -1
			for i := range 5 {
				position := strings.Index(request.UserPrompt, fmt.Sprintf("goal-%d", i))
				if position < 0 || position <= previous {
					t.Errorf("segment order lost: %s", request.UserPrompt)
				}
				previous = position
			}
			return testSummary("goal-4"), nil
		}
		count := active.Add(1)
		for old := peak.Load(); count > old; old = peak.Load() {
			if peak.CompareAndSwap(old, count) {
				break
			}
		}
		defer active.Add(-1)
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		for i := range 5 {
			if strings.Contains(request.UserPrompt, fmt.Sprintf("goal-%d", i)) {
				return testSummary(fmt.Sprintf("goal-%d", i)), nil
			}
		}
		return "", errors.New("missing goal")
	}})
	summary, source, err := engine.Generate(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if source != "llm-mapreduce" || !Validate(summary, messages).Valid {
		t.Fatalf("result: %s %s", source, summary)
	}
	if peak.Load() > 2 || peak.Load() < 1 {
		t.Fatalf("parallel peak: %d", peak.Load())
	}
}

func TestChunkFailureAndMergeFailureKeepRecentGoal(t *testing.T) {
	messages := testHistory()
	engine := New(Config{MaxChunkTokens: 250, MaxParallel: 2, Estimator: EstimatorFunc(func(text string) int { return len(text) }), Chat: func(_ context.Context, request ChatRequest) (string, error) {
		if request.Kind == "merge" || strings.Contains(request.UserPrompt, "goal-2") {
			return "", errors.New("upstream unavailable")
		}
		for i := range 5 {
			if strings.Contains(request.UserPrompt, fmt.Sprintf("goal-%d", i)) {
				return testSummary(fmt.Sprintf("goal-%d", i)), nil
			}
		}
		return "", errors.New("missing goal")
	}})
	summary, source, err := engine.Generate(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if source != "llm-mapreduce-hybrid" || !strings.Contains(summary, "goal-4") || !Validate(summary, messages).Valid {
		t.Fatalf("fallback: %s\n%s", source, summary)
	}
}

func TestCancellationDoesNotCallModel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := New(Config{Chat: func(context.Context, ChatRequest) (string, error) {
		t.Fatal("canceled operation called model")
		return "", nil
	}})
	if _, _, err := engine.Generate(ctx, []Message{{Role: "user", Content: "goal"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

type testShell struct{}

func (testShell) GetCwd() string { return "/tmp/project" }
func (testShell) GetEnv() map[string]string {
	return map[string]string{"TOKEN": "never-export-this-value"}
}

func TestTranscriptAndRestorationPreserveEvidenceBoundaries(t *testing.T) {
	messages := []Message{{Role: "user", Content: "Fix internal/example.go"}, {Role: "tool", Name: "terminal", Content: "go test passed"}, {Role: "assistant", Content: "old checkpoint must stay out", CompactBoundary: true}}
	transcript := Transcript(messages, TextOptions{})
	if strings.Contains(transcript, "old checkpoint") || !strings.Contains(transcript, "internal/example.go") {
		t.Fatal(transcript)
	}
	attachments := RestoreAttachments(testShell{}, messages, EstimatorFunc(func(text string) int { return len(text) }), TextOptions{})
	if len(attachments) != 3 {
		t.Fatalf("attachments: %+v", attachments)
	}
	for _, attachment := range attachments {
		if strings.Contains(attachment.Content, "never-export-this-value") {
			t.Fatal("environment value entered restore context")
		}
	}
}
