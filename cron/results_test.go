package cron

import (
	"errors"
	"path/filepath"
	"strings"
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
	next := time.Now().UTC().Add(30 * time.Minute)
	if err := s.ResumeTask("task-1", next); err != nil {
		t.Fatal(err)
	}
	resumed, ok := s.GetTask("task-1")
	if !ok || !resumed.NextRunAt.Equal(next) || resumed.Status != TaskActive {
		t.Fatalf("resume = %#v, %t", resumed, ok)
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

type recordingFCM struct {
	fail string
	got  []FCMPush
}

func (r *recordingFCM) Send(push FCMPush) error {
	if r.fail != "" {
		return errors.New(r.fail)
	}
	r.got = append(r.got, push)
	return nil
}

func TestDispatchAndroidFCMAfterCommitOnly(t *testing.T) {
	s := NewMemoryResultStore()
	if _, err := s.EnsureTask(Task{ID: "task-1", Name: "日报", Channels: []string{"android"}}); err != nil {
		t.Fatal(err)
	}
	scheduled := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	execution, claimed, err := s.Claim("task-1", scheduled, scheduled)
	if err != nil || !claimed {
		t.Fatal(err)
	}
	sender := &recordingFCM{}
	if pushes, err := s.DispatchAndroidFCM(execution.ID, sender, scheduled); err == nil || pushes != nil {
		t.Fatalf("running execution dispatched %#v, %v", pushes, err)
	}

	if _, err := s.RegisterDevice(Device{ID: "phone", Channel: "android", FCMToken: "token-a", CapabilityKnown: true, NotificationsEnabled: true, ChannelEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(Device{ID: "tablet", Channel: "android", FCMToken: "token-b", CapabilityKnown: true, NotificationsEnabled: true, ChannelEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(Device{ID: "muted", Channel: "android", FCMToken: "token-c", CapabilityKnown: true, NotificationsEnabled: false, ChannelEnabled: false}); err != nil {
		t.Fatal(err)
	}
	finished, err := s.Complete(execution.ID, "done", nil, scheduled.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	pushes, err := s.DispatchAndroidFCM(finished.ID, sender, scheduled.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 2 || len(sender.got) != 2 {
		t.Fatalf("pushes=%#v", pushes)
	}
	if sender.got[0].TaskID != "task-1" || sender.got[0].ExecutionID != finished.ID || sender.got[0].DeliveryID == "" || sender.got[0].Summary != "done" {
		t.Fatalf("payload=%#v", sender.got[0])
	}
	if got := s.ListDeliveries(finished.ID)[0].Status; got != DeliveryPending {
		t.Fatalf("accepted transport became %s", got)
	}

	if _, err := s.Ack(s.ListDeliveries(finished.ID)[0].ID, "phone", scheduled.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	sender.got = nil
	again, err := s.DispatchAndroidFCM(finished.ID, sender, scheduled.Add(3*time.Minute))
	if err != nil || len(again) != 0 || len(sender.got) != 0 {
		t.Fatalf("retry after ack = %#v, %v", again, err)
	}

	other, claimed, err := s.Claim("task-1", scheduled.Add(time.Hour), scheduled.Add(time.Hour))
	if err != nil || !claimed {
		t.Fatal(err)
	}
	if _, err := s.Complete(other.ID, "", errors.New("boom"), scheduled.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	failing := &recordingFCM{fail: "upstream"}
	if _, err := s.DispatchAndroidFCM(other.ID, failing, scheduled.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("transport error = %v", err)
	}
	failedDelivery := s.ListDeliveries(other.ID)[0]
	if failedDelivery.Status != DeliveryRetrying || failedDelivery.Attempts != 1 {
		t.Fatalf("retry state = %#v", failedDelivery)
	}
	if got, _ := s.GetExecution(other.ID); got.Status != ExecutionFailed || got.Error != "boom" {
		t.Fatalf("execution changed by push: %#v", got)
	}

	empty := NewMemoryResultStore()
	if _, err := empty.EnsureTask(Task{ID: "task-1", Channels: []string{"android"}}); err != nil {
		t.Fatal(err)
	}
	lonely, claimed, err := empty.Claim("task-1", scheduled, scheduled)
	if err != nil || !claimed {
		t.Fatal(err)
	}
	if _, err := empty.Complete(lonely.ID, "quiet", nil, scheduled); err != nil {
		t.Fatal(err)
	}
	if pushes, err := empty.DispatchAndroidFCM(lonely.ID, sender, scheduled); err != nil || len(pushes) != 0 {
		t.Fatalf("no device = %#v, %v", pushes, err)
	}
	if got := empty.ListDeliveries(lonely.ID)[0].Status; got != DeliveryPending {
		t.Fatalf("no device status = %s", got)
	}
}
