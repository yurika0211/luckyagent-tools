package hitl

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionRepliesDoNotResolveOtherRequests(t *testing.T) {
	gate := New(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan Pending, 2)
	done := make(chan Resolution, 2)
	for _, session := range []string{"a", "b"} {
		go func(session string) {
			res, err := gate.RequestAndWait(ctx, Request{ID: "request-" + session, SessionID: session, Kind: KindInput}, func(p Pending) { ready <- p })
			if err == nil {
				done <- res
			}
		}(session)
	}
	<-ready
	<-ready
	if _, handled, err := gate.ResolveText("unrelated", "answer"); err != nil || handled {
		t.Fatalf("unrelated reply: %v %v", handled, err)
	}
	if _, handled, err := gate.ResolveText("", "answer"); err == nil || handled {
		t.Fatalf("ambiguous reply: %v %v", handled, err)
	}
	if _, handled, err := gate.ResolveText("b", "answer"); err != nil || !handled {
		t.Fatalf("scoped reply: %v %v", handled, err)
	}
	select {
	case res := <-done:
		if res.Input != "answer" {
			t.Fatal(res)
		}
	case <-time.After(time.Second):
		t.Fatal("reply did not release wait")
	}
	if _, err := gate.Resolve("request-a", "cancel", ""); err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutAndCancellationRemovePending(t *testing.T) {
	gate := New(Options{Timeout: time.Millisecond})
	_, err := gate.RequestAndWait(context.Background(), Request{ID: "timeout"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if len(gate.List()) != 0 {
		t.Fatal("timed-out request remains pending")
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = gate.RequestAndWait(ctx, Request{ID: "cancel"}, func(Pending) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if len(gate.List()) != 0 {
		t.Fatal("canceled request remains pending")
	}
}

func TestDuplicateIDCannotReplaceWaiter(t *testing.T) {
	gate := New(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan Resolution, 1)
	go func() {
		res, _ := gate.RequestAndWait(ctx, Request{ID: "same"}, func(Pending) { close(ready) })
		done <- res
	}()
	<-ready
	if _, err := gate.RequestAndWait(ctx, Request{ID: "same"}, nil); err == nil {
		t.Fatal("duplicate request replaced the active waiter")
	}
	if _, err := gate.Resolve("same", "allow", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-done:
		if !IsAllowDecision(res.Decision) {
			t.Fatal(res)
		}
	case <-time.After(time.Second):
		t.Fatal("original waiter was lost")
	}
}

func TestCredentialReplyRequiresForm(t *testing.T) {
	if _, _, ok := InterpretReply(KindCredential, "secret-from-chat"); ok {
		t.Fatal("credential accepted through chat")
	}
	if _, _, ok := InterpretReply(KindInput, "answer"); !ok {
		t.Fatal("ordinary input was rejected")
	}
}
