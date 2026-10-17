package main

import (
	"os"
	"testing"
	"time"
)

func isolatedConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// Only one copy of the app may run: a second launch must not get the lock, and the lock must be
// free again once the first copy lets go (or dies).
func TestOnlyOneCopyHoldsTheLock(t *testing.T) {
	isolatedConfig(t)
	if !acquireLock() {
		t.Fatal("first launch could not take the lock")
	}
	defer releaseLock()
	// a second launch opens the same file separately, as another process would
	f, err := os.OpenFile(instanceFile()[:len(instanceFile())-len("instance.json")]+"app.lock", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if lockFile(f) == nil {
		t.Error("a second copy got the lock while the first is running")
	}
	releaseLock()
	if lockFile(f) != nil {
		t.Error("the lock was not released")
	}
}

func TestStaleMarkerOfACrashedCopyIsNotMistakenForARunningOne(t *testing.T) {
	isolatedConfig(t)
	if previousRunEndedBadly() {
		t.Fatal("a first run looks like a crash")
	}
	writeInstance("http://127.0.0.1:1/?t=x") // marker left by a copy that died
	if !previousRunEndedBadly() {
		t.Error("the leftover marker was not seen")
	}
	if _, ok := existingInstance(); ok {
		t.Error("a dead copy's URL was treated as a running instance")
	}
	removeInstance()
	if previousRunEndedBadly() {
		t.Error("a clean exit leaves a marker")
	}
}

// After a crash or power cut the computer was off for a while: that time must not become work.
func TestRecoverFromCrashPausesAtLastAliveMoment(t *testing.T) {
	isolatedConfig(t)
	s := newServer()
	now := time.Now()
	s.timers.Now = func() time.Time { return now }
	_ = s.timers.Add("ABC-1", "s", "p", "st")
	_ = s.timers.Start("ABC-1", false)
	// the timer started 10h ago; the app was last alive 9h ago (it ran for 1h)
	s.timers.Now = func() time.Time { return now.Add(-10 * time.Hour) }
	_ = s.timers.Pause("ABC-1")
	_ = s.timers.Start("ABC-1", false)
	s.timers.Now = time.Now

	// recent heartbeat = ordinary restart: nothing is touched
	s.recoverFromCrash(time.Now().Add(-20 * time.Second))
	if g, _ := s.timers.Get("ABC-1"); !g.Running() || s.recovered != nil {
		t.Fatal("a quick restart was treated as a crash")
	}
	s.recoverFromCrash(now.Add(-9 * time.Hour))
	g, _ := s.timers.Get("ABC-1")
	if g.Running() {
		t.Error("the timer kept running through the outage")
	}
	if g.Accumulated < 50*time.Minute || g.Accumulated > 70*time.Minute {
		t.Errorf("counted %v, want about the 1h the app was actually running", g.Accumulated)
	}
	if s.recovered == nil || len(s.recovered.Keys) != 1 {
		t.Errorf("the user is not told: %+v", s.recovered)
	}
}
