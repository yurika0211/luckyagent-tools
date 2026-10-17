package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yurika0211/luckyagent-tools/memory"
)

type writeProbe struct {
	calls   int
	issue   string
	failure error
}

func (p *writeProbe) AnalyzeMemoryContent(string) []memory.HygieneIssue {
	if p.issue != "" {
		return []memory.HygieneIssue{{Reason: p.issue}}
	}
	return nil
}
func (p *writeProbe) Remember(context.Context, memory.RememberRequest) (memory.RememberResult, error) {
	p.calls++
	return memory.RememberResult{SaveResult: memory.SaveResult{ID: "saved"}}, p.failure
}

func TestWriteAdmissionStopsAllDirtyContentBeforeBackend(t *testing.T) {
	for _, issue := range []string{"empty", "raw_conversation", "secret_like", "prompt_injection", "oversized"} {
		t.Run(issue, func(t *testing.T) {
			p := &writeProbe{issue: issue}
			_, err := memory.Write(context.Background(), p, memory.RememberRequest{Content: "candidate"})
			if err == nil || p.calls != 0 {
				t.Fatalf("error=%v backend calls=%d", err, p.calls)
			}
		})
	}
}
func TestWriteReturnsBackendFailureAndHonorsCancellation(t *testing.T) {
	want := errors.New("disk unavailable")
	p := &writeProbe{failure: want}
	_, err := memory.Write(context.Background(), p, memory.RememberRequest{Content: "fact"})
	if !errors.Is(err, want) || p.calls != 1 {
		t.Fatalf("error=%v calls=%d", err, p.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = memory.Write(ctx, p, memory.RememberRequest{Content: "fact"})
	if !errors.Is(err, context.Canceled) || p.calls != 1 {
		t.Fatalf("canceled request reached backend: %v calls=%d", err, p.calls)
	}
}
func TestExactScopeDoesNotMergeAnotherActorsFact(t *testing.T) {
	filter := memory.ExactScope([]string{"scope:personal", "scope:user:a"})
	if filter(memory.Entry{Tags: []string{"scope:personal", "scope:user:b"}}) || filter(memory.Entry{}) {
		t.Fatal("different actor accepted")
	}
	if !filter(memory.Entry{Tags: []string{"scope:user:a", "scope:personal", "project:example"}}) {
		t.Fatal("same actor rejected")
	}
}
