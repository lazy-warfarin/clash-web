//go:build !windows

package helper

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd)    { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func signalProcessGroup(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
