//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach lets the new process outlive this one.
func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
