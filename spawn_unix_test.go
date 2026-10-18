//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A browser leaves helper processes behind; closing it for an update must take them all down,
// otherwise an old window can stay open.
func TestKillTreeStopsTheWholeBrowserGroup(t *testing.T) {
	c := exec.Command("sh", "-c", "sleep 60 & sleep 60 & wait")
	ownGroup(c)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	pid := c.Process.Pid
	go func() { _ = c.Wait() }()
	time.Sleep(200 * time.Millisecond)
	if syscall.Kill(-pid, 0) != nil {
		t.Fatal("the group never started")
	}
	start := time.Now()
	killTree(c)
	if syscall.Kill(-pid, 0) == nil {
		t.Error("helper processes survived killTree")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("killTree took %v", time.Since(start))
	}
	killTree(c) // a second call on a finished browser must be harmless
	killTree(nil)
}
