package cron

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestResultStoreClaimsOneExecutionAndCreatesDeliveryAfterFinish(t *testing.T) {
	s := NewMemoryResultStore()
	if _, err := s.EnsureTask(Task{ID: "task-1", Name: "daily", Channels: []string{"android", "telegram"}}); err != nil {
		t.Fatal(err)
	}
	scheduled := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	first, claimed, err := s.Claim("task-1", scheduled, scheduled.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("first claim = %#v, %t, %v", first, claimed, err)
	}
	second, claimed, err := s.Claim("task-1", scheduled, scheduled.Add(2*time.Minute))
	if err != nil || claimed || second.ID != first.ID {
		t.Fatalf("duplicate claim = %#v, %t, %v", second, claimed, err)
	}
	if got := s.ListDeliveries(""); len(got) != 0 {
		t.Fatalf("delivery appeared before result commit: %#v", got)
	}
	finished, err := s.Complete(first.ID, "answer", nil, scheduled.Add(3*time.Minute))
	if err != nil || finished.Status != ExecutionSucceeded {
		t.Fatalf("complete = %#v, %v", finished, err)
	}
	deliveries := s.ListDeliveries(first.ID)
	if len(deliveries) != 2 || deliveries[0].Status != DeliveryPending {
		t.Fatalf("unexpected deliveries: %#v", deliveries)
	}
}

func TestResultStorePausedDeletedAndAckSemantics(t *testing.T) {
	s := NewMemoryResultStore()
	if _, err := s.EnsureTask(Task{ID: "task-1", Channels: []string{"android"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseTask("task-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Claim("task-1", time.Now().UTC(), time.Now().UTC()); !errors.Is(err, ErrTaskPaused) {
		t.Fatalf("paused claim error = %v", err)
	}
	if err := s.ResumeTask("task-1"); err != nil {
		t.Fatal(err)
	}
	execution, claimed, err := s.Claim("task-1", time.Now().UTC().Add(time.Hour), time.Now().UTC())
	if err != nil || !claimed {
		t.Fatalf("claim after resume = %#v, %t, %v", execution, claimed, err)
	}
	if _, err := s.Complete(execution.ID, "ok", nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	device, err := s.RegisterDevice(Device{ID: "phone", Channel: "android", NotificationsEnabled: true, ChannelEnabled: true})
	if err != nil || device.ID != "phone" {
		t.Fatalf("register device = %#v, %v", device, err)
	}
	candidates, err := s.PendingForDevice("phone", 1)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates = %#v, %v", candidates, err)
	}
	if _, err := s.Ack(candidates[0].ID, "phone", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(execution.ID)[0].Status; got != DeliveryDelivered {
		t.Fatalf("ack status = %s", got)
	}

	second, claimed, err := s.Claim("task-1", time.Now().UTC().Add(2*time.Hour), time.Now().UTC())
	if err != nil || !claimed {
		t.Fatalf("second claim = %#v, %t, %v", second, claimed, err)
	}
	if err := s.DeleteTask("task-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(second.ID, "late", nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(second.ID); len(got) != 0 {
		t.Fatalf("deleted task created delivery: %#v", got)
	}
	if got := s.ListExecutions("task-1"); len(got) != 0 {
		t.Fatalf("deleted task exposed executions: %#v", got)
	}
}

func TestResultStoreDeviceCursorKeepsDeliveredAndTiedSlots(t *testing.T) {
	s := NewMemoryResultStore()
	for _, id := range []string{"task-a", "task-b"} {
		if _, err := s.EnsureTask(Task{ID: id, Channels: []string{"android"}}); err != nil {
			t.Fatal(err)
		}
	}
	phone, err := s.RegisterDevice(Device{ID: "phone", Channel: "android"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(Device{ID: "tablet", Channel: "android"}); err != nil {
		t.Fatal(err)
	}
	scheduled := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	executions := make([]Execution, 0, 2)
	for _, taskID := range []string{"task-a", "task-b"} {
		execution, claimed, claimErr := s.Claim(taskID, scheduled, scheduled.Add(time.Minute))
		if claimErr != nil || !claimed {
			t.Fatalf("claim %s = %#v, %t, %v", taskID, execution, claimed, claimErr)
		}
		finished, completeErr := s.Complete(execution.ID, taskID, nil, scheduled.Add(2*time.Minute))
		if completeErr != nil {
			t.Fatal(completeErr)
		}
		executions = append(executions, finished)
	}
	first, err := s.PendingForDevice(phone.ID, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first pending = %#v, %v", first, err)
	}
	if _, err := s.Ack(first[0].ID, phone.ID, scheduled.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := s.PendingForDevice(phone.ID, 1)
	if err != nil || len(second) != 1 || second[0].ExecutionID == first[0].ExecutionID {
		t.Fatalf("tied pending = %#v, %v (executions=%#v)", second, err, executions)
	}
	all, err := s.PendingForDevice("tablet", 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("other device pending delivered rows = %#v, %v", all, err)
	}
}

func TestResultStoreAckValidatesDeviceChannelAndCancelledDelivery(t *testing.T) {
	s := NewMemoryResultStore()
	if _, err := s.EnsureTask(Task{ID: "task-1", Channels: []string{"android"}}); err != nil {
		t.Fatal(err)
	}
	execution, claimed, err := s.Claim("task-1", time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC), time.Now().UTC())
	if err != nil || !claimed {
		t.Fatalf("claim = %#v, %t, %v", execution, claimed, err)
	}
	if _, err := s.Complete(execution.ID, "ok", nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	deliveries := s.ListDeliveries(execution.ID)
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %#v", deliveries)
	}
	if _, err := s.RegisterDevice(Device{ID: "phone", Channel: "android"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(Device{ID: "tablet", Channel: "telegram"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ack(deliveries[0].ID, "tablet", time.Now().UTC()); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("cross-channel ack error = %v", err)
	}
	if got := s.ListDeliveries(execution.ID)[0].Status; got != DeliveryPending {
		t.Fatalf("cross-channel ack changed status = %s", got)
	}
	if err := s.DeleteTask("task-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ack(deliveries[0].ID, "phone", time.Now().UTC()); !errors.Is(err, ErrResultNotFound) {
		t.Fatalf("cancelled ack error = %v", err)
	}
	if got := s.ListDeliveries(execution.ID)[0].Status; got != DeliveryCancelled {
		t.Fatalf("cancelled ack changed status = %s", got)
	}
}

func TestResultStoreConcurrentClaimAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	s, err := NewResultStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureTask(Task{ID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	scheduled := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ids []string
	var claims int
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, ok, err := s.Claim("task-1", scheduled, time.Now().UTC())
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if ok {
				claims++
			}
			ids = append(ids, item.ID)
		}()
	}
	wg.Wait()
	if claims != 1 || len(ids) != 8 {
		t.Fatalf("claims=%d ids=%v", claims, ids)
	}
	reloaded, err := NewResultStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ListExecutions("task-1"); len(got) != 1 {
		t.Fatalf("reloaded executions=%#v", got)
	}
}

func TestResultStoreBlockedOnlyWhenEveryDeviceReportsUnavailable(t *testing.T) {
	s := NewMemoryResultStore()
	if _, err := s.EnsureTask(Task{ID: "task-1", Channels: []string{"android"}}); err != nil {
		t.Fatal(err)
	}
	scheduled := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	claimAndFinish := func(at time.Time) Execution {
		t.Helper()
		execution, claimed, err := s.Claim("task-1", at, at)
		if err != nil || !claimed {
			t.Fatalf("claim = %#v, %t, %v", execution, claimed, err)
		}
		finished, err := s.Complete(execution.ID, "ok", nil, at.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return finished
	}

	none := claimAndFinish(scheduled)
	if got := s.ListDeliveries(none.ID)[0].Status; got != DeliveryPending {
		t.Fatalf("no device status = %s", got)
	}

	if _, err := s.RegisterDevice(Device{ID: "offline", Channel: "android"}); err != nil {
		t.Fatal(err)
	}
	unknown := claimAndFinish(scheduled.Add(time.Hour))
	if got := s.ListDeliveries(unknown.ID)[0].Status; got != DeliveryPending {
		t.Fatalf("unknown capability status = %s", got)
	}

	if _, err := s.RegisterDevice(Device{
		ID: "phone", Channel: "android", CapabilityKnown: true,
		NotificationsEnabled: false, ChannelEnabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(none.ID)[0].Status; got != DeliveryPending {
		t.Fatalf("one unknown device became blocked: %s", got)
	}

	if _, err := s.RegisterDevice(Device{ID: "offline", Channel: "android", CapabilityKnown: true, NotificationsEnabled: false, ChannelEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(none.ID)[0].Status; got != DeliveryBlocked {
		t.Fatalf("all explicit unavailable status = %s", got)
	}
	if got := s.ListDeliveries(unknown.ID)[0].Status; got != DeliveryBlocked {
		t.Fatalf("second delivery status = %s", got)
	}

	blocked, err := s.PendingForDevice("phone", 1)
	if err != nil || len(blocked) != 0 {
		t.Fatalf("blocked pending = %#v, %v", blocked, err)
	}

	created := claimAndFinish(scheduled.Add(2 * time.Hour))
	if got := s.ListDeliveries(created.ID)[0].Status; got != DeliveryBlocked {
		t.Fatalf("created while blocked = %s", got)
	}

	if _, err := s.RegisterDevice(Device{
		ID: "tablet", Channel: "android", FCMToken: "token",
		CapabilityKnown: true, NotificationsEnabled: true, ChannelEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(none.ID)[0].Status; got != DeliveryBlocked {
		t.Fatalf("recovered permission reopened delivery: %s", got)
	}
	if _, err := s.Ack(s.ListDeliveries(none.ID)[0].ID, "tablet", scheduled.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(none.ID)[0].Status; got != DeliveryDelivered {
		t.Fatalf("ack from blocked = %s", got)
	}
	if _, err := s.RegisterDevice(Device{ID: "tablet", Channel: "android", CapabilityKnown: true, NotificationsEnabled: false, ChannelEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if got := s.ListDeliveries(none.ID)[0].Status; got != DeliveryDelivered {
		t.Fatalf("later capability changed delivered: %s", got)
	}
}
