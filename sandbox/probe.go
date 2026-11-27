package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrSandboxUnavailable is returned when a restricted tool cannot get an
// operating-system boundary. Callers must not retry the command on the host.
var ErrSandboxUnavailable = errors.New("SANDBOX_UNAVAILABLE")

// Probe is one fixed self-check. It does not take a user command or path.
type Probe struct {
	Reason string
	Detail string
}

func (p Probe) Error() string {
	if p.Detail == "" {
		return ErrSandboxUnavailable.Error() + ": " + p.Reason
	}
	return ErrSandboxUnavailable.Error() + ": " + p.Reason + ": " + p.Detail
}

func (p Probe) Unwrap() error { return ErrSandboxUnavailable }

// ProbeIsolation runs the fixed check for the current host. A missing
// bubblewrap, a refused user namespace, or a failed mount is a failure.
// Success does not mean a later command may skip its own sandbox.
func ProbeIsolation() error {
	if runtime.GOOS != "linux" {
		return Probe{Reason: "namespace permission denied", Detail: "not linux"}
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return Probe{Reason: "bubblewrap missing", Detail: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bwrap,
		"--die-with-parent", "--unshare-user", "--unshare-pid", "--unshare-net",
		"--tmpfs", "/", "--proc", "/proc", "--dev", "/dev",
		"--", "true",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(output.String())
		if detail == "" {
			detail = err.Error()
		}
		return Probe{Reason: classifyProbeFailure(detail), Detail: detail}
	}
	return nil
}

// classifyProbeFailure names the four cases the contract requires a caller to
// tell apart. It does not change AppArmor, users, or sudo. A refused user
// namespace stays "namespace permission denied" so it is not reported as a
// working isolation boundary.
func classifyProbeFailure(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "mount"),
		strings.Contains(lower, "bind"):
		return "mount failed"
	case strings.Contains(lower, "executable file not found"),
		strings.Contains(lower, "bwrap: not found"):
		return "bubblewrap missing"
	case strings.Contains(lower, "setpcap"),
		strings.Contains(lower, "net_admin"),
		strings.Contains(lower, "uid map"),
		strings.Contains(lower, "apparmor"),
		strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "operation not permitted"):
		return "namespace permission denied"
	default:
		return "trusted exec unavailable"
	}
}

// requireProbe blocks a restricted command when the fixed check fails.
// dev mode is unchanged. The check is not a user command.
func requireProbe(s *Snapshot) error {
	if s == nil || s.Mode == ModeDev || !s.Restricted() {
		return nil
	}
	if err := ProbeIsolation(); err != nil {
		return err
	}
	return nil
}

// WriteProbeReport records the fixed check under the runtime directory.
// It is evidence for operators. It does not change AppArmor or users.
func WriteProbeReport(home string) error {
	err := ProbeIsolation()
	dir := filepath.Join(home, "runtime")
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return mkErr
	}
	text := "ok\n"
	if err != nil {
		text = err.Error() + "\n"
	}
	return os.WriteFile(filepath.Join(dir, "sandbox_probe.txt"), []byte(text), 0o600)
}
