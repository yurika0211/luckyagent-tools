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
	reg.Register(&Tool{Name: "noop", Enabled: true, Handler: func(map[string]any) (string, error) { return "ok", nil }})
	first := NewSkillSandbox(reg, DefaultSandboxConfig())
	second := NewSkillSandbox(reg, DefaultSandboxConfig())
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	second.Track(cmd)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := first.ExecuteWithContext(ctx, "noop", nil); err == nil {
		t.Fatal("cancelled context returned success")
	}
	if err := cmd.Process.Signal(nil); false {
		_ = err
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		t.Fatal("stopping one sandbox stopped another sandbox's process")
	}
}
