package main

import (
	"os/exec"
	"syscall"
)

// detach lets the new process outlive this one (DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP).
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
}
