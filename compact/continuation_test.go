package compact

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalFallbackPreservesPlansAsUnverified(t *testing.T) {
	messages := []Message{{Role: "user", Content: "Fix src/main.go and test"}, {Role: "assistant", Content: "I will edit src/main.go and run go test next. No changes have been made yet."}}
	summary := LocalSummary(messages, TextOptions{})
	completed := strings.Join(sectionBullets(summary, "Completed work"), "\n")
	if strings.Contains(completed, "I will edit") || !strings.Contains(summary, "Prior assistant statement (unverified)") {
		t.Fatal(summary)
	}
	if v := Validate(summary, messages); !v.Valid {
		t.Fatal(v)
	}
	bad := strings.Replace(summary, "No reliable assistant completion details were available to the local compact fallback.", "I will edit src/main.go next.", 1)
	if v := Validate(bad, messages); v.Valid {
		t.Fatal("future plan accepted as completed work")
	}
}

func TestExtendedSummaryBudgetStillHonorsExplicitCancel(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	summary, closeSummary := SummaryContext(parent)
	defer closeSummary()
	cancel()
	select {
	case <-summary.Done():
		if summary.Err() != context.Canceled {
			t.Fatal(summary.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("user cancellation was detached")
	}
}
