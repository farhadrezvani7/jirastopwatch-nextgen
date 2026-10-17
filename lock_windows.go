package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock that the operating system drops when the process dies.
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}
