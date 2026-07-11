//go:build windows

package helper

import (
	"os"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd)    {}
func signalProcessGroup(cmd *exec.Cmd) { _ = cmd.Process.Signal(os.Interrupt) }
