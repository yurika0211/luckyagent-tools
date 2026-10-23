//go:build windows

package tool

import "os/exec"

func configureShellProcessGroup(cmd *exec.Cmd) {}

func killShellProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
