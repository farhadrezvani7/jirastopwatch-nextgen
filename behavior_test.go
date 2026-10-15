package main

import (
	"jirastopwatch-nextgen/internal/timer"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return newServer()
}

func running(s *server) (keys []string) {
	for _, t := range s.timers.Snapshot() {
		if t.Running() {
			keys = append(keys, t.Key)
		}
	}
	return
}

func TestSessionLockModes(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		afterLock   int
		afterUnlock int
	}{
		{"", 1, 1},       // ignore
		{"pause", 0, 0},  // pause, stay paused
		{"resume", 0, 1}, // pause, resume on unlock
	} {
		s := newTestServer(t)
		s.timers.Add("A-1", "", "", "")
		s.timers.Add("B-2", "", "", "")
		s.timers.Start("A-1", false)
		s.cfg.LockMode = tc.mode
		s.onLock(true)
		if n := len(running(s)); n != tc.afterLock {
			t.Errorf("mode %q after lock: %d running, want %d", tc.mode, n, tc.afterLock)
		}
		s.onLock(false)
		if n := len(running(s)); n != tc.afterUnlock {
			t.Errorf("mode %q after unlock: %d running, want %d", tc.mode, n, tc.afterUnlock)
		}
	}
}

func TestResumeOnlyRestartsWhatTheLockPaused(t *testing.T) {
	s := newTestServer(t)
	s.timers.Add("A-1", "", "", "")
	s.timers.Add("B-2", "", "", "") // paused by the user before the lock
	s.timers.Start("A-1", false)
	s.cfg.LockMode = "resume"
	s.onLock(true)
	s.onLock(false)
	if got := running(s); len(got) != 1 || got[0] != "A-1" {
		t.Errorf("running after unlock = %v, want [A-1]", got)
	}
}

func TestExitModes(t *testing.T) {
	s := newTestServer(t)
	s.timers.Add("A-1", "", "", "")
	s.timers.Start("A-1", false)
	s.timers.SetElapsed("A-1", 5*time.Minute)
	s.onExit() // default: keep running
	if len(running(s)) != 1 {
		t.Error("default must keep timers running")
	}
	s.cfg.ExitMode = "pause"
	s.onExit()
	if g, _ := s.timers.Get("A-1"); g.Running() || g.Accumulated < 5*time.Minute {
		t.Errorf("pause: %+v", g)
	}
	s.cfg.ExitMode = "reset"
	s.onExit()
	if g, _ := s.timers.Get("A-1"); g.Accumulated != 0 {
		t.Errorf("reset: %+v", g)
	}
}

func TestCommentDraftSurvivesAndClearsOnReset(t *testing.T) {
	s := newTestServer(t)
	s.timers.Add("A-1", "", "", "")
	if err := s.timers.SetComment("A-1", "half-written note"); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.timers.Get("A-1"); g.Comment != "half-written note" {
		t.Errorf("%+v", g)
	}
	s.timers.Reset("A-1")
	if g, _ := s.timers.Get("A-1"); g.Comment != "" {
		t.Error("reset (after logging) must drop the draft")
	}
	if s.timers.SetComment("Z-9", "x") != timer.ErrNotFound {
		t.Error("unknown key")
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, cur string
		want        bool
	}{
		{"v1.10.0", "1.9.2", true}, {"1.2.0", "1.2.0", false}, {"1.2.1", "1.2.0", true},
		{"1.2", "1.2.0", false}, {"v2.0.0-rc1", "1.9.9", true}, {"1.0.0", "dev", false}, {"junk", "1.0.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.cur); got != c.want {
			t.Errorf("newerVersion(%q,%q) = %v", c.latest, c.cur, got)
		}
	}
}
