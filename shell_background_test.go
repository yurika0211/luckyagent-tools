package tool

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackgroundShellSurvivesTimeoutAndForegroundStillKills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group signals are unix-only")
	}
	t.Cleanup(resetShellBackgroundTasks)

	start := time.Now()
	out, err := handleShell(map[string]any{
		"command":    "sleep 30",
		"background": true,
		"timeout":    1,
	})
	if err != nil {
		t.Fatalf("background start: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("background start took %s", time.Since(start))
	}
	id := taskIDFromOutput(out)
	if id == "" {
		t.Fatalf("missing task_id in %q", out)
	}
	task, err := shellBackground.get(id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if !task.running() || !shellTaskAlive(task.cmd) {
		t.Fatal("background sleep was killed inside the foreground timeout window")
	}

	snap, err := handleShellOutput(map[string]any{"task_id": id, "timeout": 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snap, "state=running") {
		t.Fatalf("snapshot = %q", snap)
	}

	killed, err := handleShellKill(map[string]any{"task_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(killed, "state=killed") {
		t.Fatalf("kill = %q", killed)
	}
	if task.running() || shellTaskAlive(task.cmd) {
		t.Fatal("task still alive after kill")
	}

	fgStart := time.Now()
	_, fgErr := handleShell(map[string]any{"command": "sleep 30", "timeout": 1})
	if fgErr == nil || !strings.Contains(fgErr.Error(), "timed out after 1 seconds") {
		t.Fatalf("foreground timeout = %v", fgErr)
	}
	if time.Since(fgStart) > 8*time.Second {
		t.Fatalf("foreground kill returned too slowly: %s", time.Since(fgStart))
	}
}

func TestBackgroundWaitDoesNotKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group signals are unix-only")
	}
	t.Cleanup(resetShellBackgroundTasks)

	out, err := handleShell(map[string]any{"command": "sleep 30", "background": true})
	if err != nil {
		t.Fatal(err)
	}
	id := taskIDFromOutput(out)
	start := time.Now()
	snap, err := handleShellOutput(map[string]any{"task_id": id, "timeout": 1})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("wait overran: %s", time.Since(start))
	}
	if !strings.Contains(snap, "state=running") {
		t.Fatalf("wait killed or finished early: %q", snap)
	}
	task, _ := shellBackground.get(id)
	if !shellTaskAlive(task.cmd) {
		t.Fatal("process died during wait")
	}
}

func taskIDFromOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "task_id=") {
			return strings.TrimPrefix(line, "task_id=")
		}
	}
	return ""
}
