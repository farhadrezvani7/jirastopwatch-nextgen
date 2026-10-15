package timer

import (
	"testing"
	"time"
)

func TestStartPauseElapsed(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	m := &Manager{Now: func() time.Time { return now }}
	m.Add("A-1", "a", "p", "s")
	m.Add("B-2", "b", "p", "s")
	m.Start("A-1", true)
	now = now.Add(10 * time.Minute)
	m.Start("B-2", true) // pauses A-1
	now = now.Add(5 * time.Minute)
	a, _ := m.Get("A-1")
	b, _ := m.Get("B-2")
	if a.Running() || a.Elapsed(now) != 10*time.Minute {
		t.Errorf("A-1: running=%v elapsed=%v", a.Running(), a.Elapsed(now))
	}
	if !b.Running() || b.Elapsed(now) != 5*time.Minute {
		t.Errorf("B-2: running=%v elapsed=%v", b.Running(), b.Elapsed(now))
	}
}

func TestSetElapsedAndReset(t *testing.T) {
	now := time.Now()
	m := &Manager{Now: func() time.Time { return now }}
	m.Add("A-1", "", "", "")
	m.Start("A-1", false)
	now = now.Add(time.Hour)
	m.SetElapsed("A-1", 30*time.Minute)
	now = now.Add(time.Minute)
	a, _ := m.Get("A-1")
	if a.Elapsed(now) != 31*time.Minute {
		t.Error(a.Elapsed(now))
	}
	m.Reset("A-1")
	a, _ = m.Get("A-1")
	if a.Running() || a.Elapsed(now) != 0 {
		t.Error("not reset")
	}
}

func TestPersistenceSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/sub/timers.json"
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	m := Load(path)
	m.Now = func() time.Time { return now }
	m.Add("A-1", "a", "p", "s")
	m.Start("A-1", true)
	now = now.Add(7 * time.Minute)

	m2 := Load(path) // "restart": timer was running, wall-clock keeps counting
	got, ok := m2.Get("A-1")
	if !ok || !got.Running() || got.Elapsed(now) != 7*time.Minute {
		t.Fatalf("restored %+v ok=%v elapsed=%v", got, ok, got.Elapsed(now))
	}
	m2.Reset("A-1")
	if g, _ := Load(path).Get("A-1"); g.Running() || g.Accumulated != 0 {
		t.Errorf("reset not persisted: %+v", g)
	}
}

func TestMoveAndLimit(t *testing.T) {
	m := &Manager{Now: time.Now}
	for _, k := range []string{"A-1", "B-2", "C-3"} { // Add puts newest on top: C-3, B-2, A-1
		m.Add(k, "", "", "")
	}
	m.Move("A-1", -1)
	order := func() string {
		s := ""
		for _, x := range m.Snapshot() {
			s += x.Key + " "
		}
		return s
	}
	if got := order(); got != "C-3 A-1 B-2 " {
		t.Errorf("after move up: %s", got)
	}
	m.Move("B-2", 1) // already last: no-op
	if got := order(); got != "C-3 A-1 B-2 " {
		t.Errorf("edge move changed order: %s", got)
	}
	if m.Move("Z-9", 1) != ErrNotFound {
		t.Error("missing key")
	}
	for i := 0; i < MaxTimers; i++ {
		_ = m.Add("K-"+string(rune('A'+i)), "", "", "")
	}
	if err := m.Add("OVER-1", "", "", ""); err != ErrTooMany {
		t.Errorf("limit: %v", err)
	}
	if err := m.Add("C-3", "new", "", ""); err != nil { // refreshing an existing one is always fine
		t.Errorf("refresh at limit: %v", err)
	}
}
