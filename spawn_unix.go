//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// detach lets the new process outlive this one.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// ownGroup puts the browser in its own process group so that all of its helper processes
// (renderers, GPU, crash handler...) can be closed together with it.
func ownGroup(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killTree closes a browser started with ownGroup: first politely (SIGTERM lets a browser close its
// windows), then, if anything is still alive, for good. Leaving a helper behind can leave a window open.
func killTree(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	pid := c.Process.Pid
	if c.SysProcAttr == nil || !c.SysProcAttr.Setpgid {
		_ = c.Process.Kill() // not started in its own group: all we can safely stop is the process itself
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	for i := 0; i < 15; i++ { // up to ~1.5 s
		time.Sleep(100 * time.Millisecond)
		if syscall.Kill(-pid, 0) != nil { // the whole group is gone
			return
		}
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
