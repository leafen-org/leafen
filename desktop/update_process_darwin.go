//go:build darwin && !ios

package main

import (
	"os/exec"
	"syscall"
)

// detach lets a started program — the update helper and the app it
// starts (update_helper.go) — outlive this one.
func detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive says whether a process is still running.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
