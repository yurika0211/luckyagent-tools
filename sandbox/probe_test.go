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

func TestDevModeDoesNotRequireProbe(t *testing.T) {
	if err := requireProbe(&Snapshot{Mode: ModeDev}); err != nil {
		t.Fatal(err)
	}
	if err := requireProbe(nil); err != nil {
		t.Fatal(err)
	}
}
