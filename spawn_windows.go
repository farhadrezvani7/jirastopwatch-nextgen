package main

import (
	"os/exec"
	"strconv"
	"syscall"
)

// detach lets the new process outlive this one (DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP).
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
}

// ownGroup: nothing to do on Windows; killTree uses the process tree instead.
func ownGroup(c *exec.Cmd) {}

// killTree ends the browser and every process it started, so no window is left behind.
func killTree(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid))
	k.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if k.Run() != nil {
		_ = c.Process.Kill()
	}
}
