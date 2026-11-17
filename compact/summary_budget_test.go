package compact

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSummaryBudgetIsAppliedOnEveryInvoke(t *testing.T) {
	const budget = 90 * time.Second
	var mu sync.Mutex
	segments, merges := 0, 0
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	engine := New(Config{
		MaxChunkTokens: 8,
		MaxParallel:    2,
		Logger:         logger,
		Estimator:      EstimatorFunc(func(text string) int { return len(text) }),
		SummaryBudget: func(request ChatRequest) time.Duration {
			return budget
		},
		Chat: func(ctx context.Context, request ChatRequest) (string, error) {
			mu.Lock()
			switch request.Kind {
			case "segment":
				segments++
			case "merge":
				merges++
			}
			mu.Unlock()
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("missing deadline")
			}
			remaining := time.Until(deadline)
			if remaining < 60*time.Second || remaining > budget+time.Second {
				t.Fatalf("kind=%s remaining=%s want about %s", request.Kind, remaining, budget)
			}
			return "Current user goal:\n- keep goal.txt\nCompleted work:\n- noted", nil
		},
	})
	messages := []Message{
		{Role: "user", Content: "keep goal.txt"},
		{Role: "assistant", Content: "0123456789abcdef"},
		{Role: "user", Content: "second turn keep goal.txt"},
		{Role: "assistant", Content: "fedcba9876543210"},
	}
	if _, _, err := engine.Generate(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	if segments < 2 || merges != 1 {
		t.Fatalf("segments=%d merges=%d", segments, merges)
	}
	if !strings.Contains(logs.String(), "timeout="+budget.String()) {
		t.Fatalf("log missed actual budget: %s", logs.String())
	}
}

func TestSummaryBudgetNilAndNonPositiveStayAtDefault(t *testing.T) {
	for _, name := range []string{"nil", "zero", "negative"} {
		t.Run(name, func(t *testing.T) {
			var budgetFn func(ChatRequest) time.Duration
			switch name {
			case "zero":
				budgetFn = func(ChatRequest) time.Duration { return 0 }
			case "negative":
				budgetFn = func(ChatRequest) time.Duration { return -time.Second }
			}
			engine := New(Config{
				SummaryBudget: budgetFn,
				Chat: func(ctx context.Context, request ChatRequest) (string, error) {
					if request.Kind != "segment" {
						t.Fatalf("single range kind=%s", request.Kind)
					}
					deadline, ok := ctx.Deadline()
					if !ok {
						t.Fatal("missing deadline")
					}
					remaining := time.Until(deadline)
					if remaining < 30*time.Second || remaining > SummaryTimeout+time.Second {
						t.Fatalf("remaining=%s", remaining)
					}
					return "Current user goal:\n- keep goal.txt", nil
				},
			})
			if _, source, err := engine.Generate(context.Background(), []Message{{Role: "user", Content: "keep goal.txt"}}); err != nil || source != "llm" {
				t.Fatalf("source=%s err=%v", source, err)
			}
		})
	}
}

type budgetKey struct{}

func TestSummaryBudgetShortParentDeadlineExtendsToRequestedBudget(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	parent = context.WithValue(parent, budgetKey{}, "keep")
	const budget = 2 * time.Second
	engine := New(Config{
		SummaryBudget: func(ChatRequest) time.Duration { return budget },
		Chat: func(ctx context.Context, _ ChatRequest) (string, error) {
			if ctx.Value(budgetKey{}) != "keep" {
				t.Fatal("parent value dropped")
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("missing deadline")
			}
			remaining := time.Until(deadline)
			if remaining < time.Second || remaining > budget+time.Second {
				t.Fatalf("short parent was not extended: %s", remaining)
			}
			return "Current user goal:\n- keep goal.txt", nil
		},
	})
	if _, _, err := engine.Generate(parent, []Message{{Role: "user", Content: "keep goal.txt"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryBudgetParentCancelBeforeAndDuringInvoke(t *testing.T) {
	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	called := false
	engine := New(Config{
		SummaryBudget: func(ChatRequest) time.Duration { return 2 * time.Second },
		Chat: func(context.Context, ChatRequest) (string, error) {
			called = true
			return "unused", nil
		},
	})
	if _, _, err := engine.Generate(canceled, []Message{{Role: "user", Content: "keep goal.txt"}}); err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}

	parent, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	engine = New(Config{
		SummaryBudget: func(ChatRequest) time.Duration { return 2 * time.Second },
		Chat: func(ctx context.Context, _ ChatRequest) (string, error) {
			close(started)
			cancel()
			<-ctx.Done()
			if ctx.Err() != context.Canceled {
				t.Fatalf("during cancel: %v", ctx.Err())
			}
			return "", ctx.Err()
		},
	})
	done := make(chan error, 1)
	go func() {
		_, _, err := engine.Generate(parent, []Message{{Role: "user", Content: "keep goal.txt"}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("invoke did not start")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("during cancel result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight cancel did not abort")
	}
}
