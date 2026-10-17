package main

import (
	"net/http/httptest"
	"os"
	"strings"
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

// After a crash or power cut the computer may have been off for hours. The app must NOT change any
// timer by itself: it only asks, and acts on the user's answer.
func TestCrashRecoveryAsksAndOnlyActsOnTheAnswer(t *testing.T) {
	setup := func(t *testing.T) (*server, time.Time) {
		isolatedConfig(t)
		s := newServer()
		now := time.Now()
		// the timer started 10h ago; the app was last alive 9h ago (so it really ran for 1h)
		s.timers.Now = func() time.Time { return now.Add(-10 * time.Hour) }
		_ = s.timers.Add("ABC-1", "s", "p", "st")
		_ = s.timers.Start("ABC-1", false)
		s.timers.Now = time.Now
		return s, now.Add(-9 * time.Hour)
	}
	answer := func(s *server, action string) {
		req := httptest.NewRequest("POST", "/api/recovery", strings.NewReader(`{"action":"`+action+`"}`))
		w := httptest.NewRecorder()
		s.answerRecovery(w, req)
		if w.Code != 200 {
			t.Fatalf("answer %s: %d %s", action, w.Code, w.Body)
		}
	}

	t.Run("a quick restart is not a crash", func(t *testing.T) {
		s, _ := setup(t)
		s.recoverFromCrash(time.Now().Add(-20 * time.Second))
		if info, _ := s.currentRecovery(); info != nil {
			t.Error("asked after a quick restart")
		}
	})
	t.Run("nothing changes until the user answers", func(t *testing.T) {
		s, alive := setup(t)
		s.recoverFromCrash(alive)
		info, _ := s.currentRecovery()
		if info == nil || len(info.Keys) != 1 {
			t.Fatalf("the user is not asked: %+v", info)
		}
		if g, _ := s.timers.Get("ABC-1"); !g.Running() {
			t.Error("the app paused a timer without asking")
		}
	})
	t.Run("keep counting", func(t *testing.T) {
		s, alive := setup(t)
		s.recoverFromCrash(alive)
		answer(s, "keep")
		g, _ := s.timers.Get("ABC-1")
		if !g.Running() || g.Elapsed(time.Now()) < 9*time.Hour {
			t.Errorf("'keep' changed the timer: running=%v elapsed=%v", g.Running(), g.Elapsed(time.Now()))
		}
		if info, _ := s.currentRecovery(); info != nil {
			t.Error("still asking after the answer")
		}
	})
	t.Run("rewind to the last moment the app was alive", func(t *testing.T) {
		s, alive := setup(t)
		s.recoverFromCrash(alive)
		answer(s, "rewind")
		g, _ := s.timers.Get("ABC-1")
		if g.Running() || g.Accumulated < 50*time.Minute || g.Accumulated > 70*time.Minute {
			t.Errorf("rewind: running=%v counted=%v, want paused at about 1h", g.Running(), g.Accumulated)
		}
	})
	t.Run("the question survives a restart", func(t *testing.T) {
		s, alive := setup(t)
		s.recoverFromCrash(alive)
		s2 := newServer() // the app is started again without the user having answered
		if info, _ := s2.currentRecovery(); info == nil {
			t.Error("the pending question was lost")
		}
	})
	t.Run("timers the user already dealt with are left alone", func(t *testing.T) {
		s, alive := setup(t)
		s.recoverFromCrash(alive)
		_ = s.timers.Pause("ABC-1")
		_ = s.timers.SetElapsed("ABC-1", 3*time.Hour) // the user fixed the time by hand
		_ = s.timers.Start("ABC-1", false)            // and started it again: a different start time
		if info, _ := s.currentRecovery(); info != nil {
			t.Error("asked about a timer that is no longer in that state")
		}
		answer(s, "rewind")
		if g, _ := s.timers.Get("ABC-1"); !g.Running() || g.Accumulated != 3*time.Hour {
			t.Errorf("rewind touched a timer the user had changed: %+v", g)
		}
	})
}
