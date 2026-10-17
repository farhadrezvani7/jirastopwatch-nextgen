//go:build !windows

package main

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive lock that the operating system drops when the process dies.
func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
