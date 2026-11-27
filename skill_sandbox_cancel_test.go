package tool

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestCancelStopsTrackedSkillProcess(t *testing.T) {
	reg := NewRegistry()
	sb := NewSkillSandbox(reg, SandboxConfig{MaxExecutionTime: time.Minute, EnableAuditLog: false})
	reg.Register(&Tool{
		Name: "sleep_skill", Enabled: true, Source: "demo",
		Handler: func(map[string]any) (string, error) {
			cmd := exec.Command("sleep", "30")
			if err := cmd.Start(); err != nil {
				return "", err
			}
			sb.Track(cmd)
			err := cmd.Wait()
			return "done", err
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := sb.ExecuteWithContext(ctx, "sleep_skill", nil)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled skill returned success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel returned without the child exiting")
	}
}

func TestOtherSandboxIsNotStopped(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Tool{
		Name: "sleep_skill", Enabled: true,
		Handler: func(map[string]any) (string, error) { return "unused", nil },
	})
	first := NewSkillSandbox(reg, SandboxConfig{MaxExecutionTime: time.Minute, EnableAuditLog: false})
	second := NewSkillSandbox(reg, SandboxConfig{MaxExecutionTime: time.Minute, EnableAuditLog: false})
	cmdA := exec.Command("sleep", "30")
	cmdB := exec.Command("sleep", "30")
	if err := cmdA.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmdB.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmdA.Process != nil {
			_ = cmdA.Process.Kill()
		}
		if cmdB.Process != nil {
			_ = cmdB.Process.Kill()
		}
	})
	first.Track(cmdA)
	second.Track(cmdB)
	waitA := make(chan error, 1)
	waitB := make(chan error, 1)
	go func() { waitA <- cmdA.Wait() }()
	go func() { waitB <- cmdB.Wait() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := first.ExecuteWithContext(ctx, "sleep_skill", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled sandbox A returned success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sandbox A did not return after cancel")
	}
	select {
	case <-waitA:
	case <-time.After(3 * time.Second):
		t.Fatal("sandbox A's child did not exit")
	}
	select {
	case <-waitB:
		t.Fatal("stopping sandbox A stopped sandbox B's process")
	case <-time.After(200 * time.Millisecond):
	}
}
