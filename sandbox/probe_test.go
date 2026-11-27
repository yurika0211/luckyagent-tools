package sandbox

import (
	"errors"
	"runtime"
	"testing"
)

func TestProbeDoesNotAcceptAUserCommand(t *testing.T) {
	err := ProbeIsolation()
	if runtime.GOOS != "linux" {
		if !errors.Is(err, ErrSandboxUnavailable) {
			t.Fatalf("non-linux probe = %v", err)
		}
		return
	}
	if err != nil && !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("probe error = %v", err)
	}
}

func TestClassifyProbeFailureSeparatesTheFourCases(t *testing.T) {
	cases := []struct {
		detail string
		reason string
	}{
		{"exec: bwrap: executable file not found in $PATH", "bubblewrap missing"},
		{"bwrap: setting up uid map: Permission denied", "namespace permission denied"},
		{"bwrap: No permissions to creating new namespace, likely because unprivileged_userns AppArmor policy", "namespace permission denied"},
		{"bwrap: Can't bind mount: no such file", "mount failed"},
		{"sandbox process exited before the barrier", "trusted exec unavailable"},
	}
	for _, tc := range cases {
		if got := classifyProbeFailure(tc.detail); got != tc.reason {
			t.Fatalf("classify(%q) = %q, want %q", tc.detail, got, tc.reason)
		}
	}
}

func TestDevModeDoesNotRequireProbe(t *testing.T) {
	if err := requireProbe(&Snapshot{Mode: ModeDev}); err != nil {
		t.Fatal(err)
	}
	if err := requireProbe(nil); err != nil {
		t.Fatal(err)
	}
}
