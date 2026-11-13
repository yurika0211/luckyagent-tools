//go:build windows

package tool

import (
	"os"
	"os/exec"
)

func configureShellProcessGroup(cmd *exec.Cmd) {}

func killShellProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func signalShellTerm(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func shellProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil || proc == nil {
		return false
	}
	return true
}
