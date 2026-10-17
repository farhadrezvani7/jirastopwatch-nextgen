package timer

import (
	"os"
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
	m.MoveAfter("C-3", "B-2") // drag to the end
	if got := order(); got != "A-1 B-2 C-3 " {
		t.Errorf("after drag to end: %s", got)
	}
	m.MoveAfter("B-2", "") // drag to the top
	if got := order(); got != "B-2 A-1 C-3 " {
		t.Errorf("after drag to top: %s", got)
	}
	if m.MoveAfter("B-2", "Z-9") != ErrNotFound || m.MoveAfter("Z-9", "") != ErrNotFound {
		t.Error("drag with a missing key")
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

func TestLoadSurvivesDamagedFile(t *testing.T) {
	path := t.TempDir() + "/timers.json"
	m := Load(path)
	m.Add("A-1", "a", "p", "s")
	m.Add("B-2", "b", "p", "s")                                              // the second save keeps the first as the backup
	if err := os.WriteFile(path, []byte(`[{"key":"B-`), 0o600); err != nil { // power cut mid-write
		t.Fatal(err)
	}
	got := Load(path)
	if _, ok := got.Get("A-1"); !ok {
		t.Errorf("the timer from the last good copy was lost: %+v", got.Snapshot())
	}
	got.Add("C-3", "c", "p", "s") // and the app keeps working and saving afterwards
	if _, ok := Load(path).Get("C-3"); !ok {
		t.Error("saving after recovery failed")
	}
}

func TestClockSetBackNeverEatsBankedTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := &Manager{Now: func() time.Time { return now }}
	m.Add("A-1", "", "", "")
	m.SetElapsed("A-1", 20*time.Minute)
	m.Start("A-1", false)
	now = now.Add(-3 * time.Hour) // the computer's clock jumps back
	a, _ := m.Get("A-1")
	if got := a.Elapsed(now); got != 20*time.Minute {
		t.Errorf("elapsed after the clock moved back = %v, want the banked 20m", got)
	}
	m.Pause("A-1")
	if a, _ = m.Get("A-1"); a.Accumulated != 20*time.Minute {
		t.Errorf("banked time changed on pause: %v", a.Accumulated)
	}
}

func TestSaveErrorIsReported(t *testing.T) {
	dir := t.TempDir()
	blocker := dir + "/file"
	os.WriteFile(blocker, []byte("x"), 0o600)
	m := Load(blocker + "/sub/timers.json") // the parent "folder" is a file: writing must fail
	m.Add("A-1", "", "", "")
	if m.SaveError() == "" {
		t.Error("a failed save went unnoticed")
	}
	m2 := Load(dir + "/ok/timers.json")
	m2.Add("A-1", "", "", "")
	if m2.SaveError() != "" {
		t.Errorf("unexpected save error: %v", m2.SaveError())
	}
}

func keys(m *Manager) string {
	s := ""
	for _, x := range m.Snapshot() {
		s += x.Key
		if x.Pinned {
			s += "*"
		}
		s += " "
	}
	return s
}

// Pinning adds one rule (pinned timers are always above unpinned ones); every operation must keep it.
func TestPinnedTimersStayOnTopInEveryOperation(t *testing.T) {
	path := t.TempDir() + "/timers.json"
	m := Load(path)
	for _, k := range []string{"E-5", "D-4", "C-3", "B-2", "A-1"} { // Add puts the newest first: A-1 B-2 C-3 D-4 E-5
		m.Add(k, "", "", "")
	}
	if got := keys(m); got != "A-1 B-2 C-3 D-4 E-5 " {
		t.Fatalf("setup: %s", got)
	}

	// pin: goes to the end of the pinned block
	m.SetPinned("C-3", true)
	m.SetPinned("E-5", true)
	if got := keys(m); got != "C-3* E-5* A-1 B-2 D-4 " {
		t.Errorf("after pinning C and E: %s", got)
	}
	if m.SetPinned("C-3", true) != nil || keys(m) != "C-3* E-5* A-1 B-2 D-4 " {
		t.Error("pinning twice changed something")
	}
	if m.SetPinned("Z-9", true) != ErrNotFound {
		t.Error("pinning a missing timer")
	}

	// a new timer comes in below the pinned ones
	m.Add("F-6", "", "", "")
	if got := keys(m); got != "C-3* E-5* F-6 A-1 B-2 D-4 " {
		t.Errorf("new timer must go below the pinned ones: %s", got)
	}

	// ▲▼ moves inside a group, never across the boundary
	m.Move("E-5", -1)
	if got := keys(m); got != "E-5* C-3* F-6 A-1 B-2 D-4 " {
		t.Errorf("move inside the pinned group: %s", got)
	}
	m.Move("C-3", 1) // would step into the unpinned group: refused
	m.Move("F-6", -1)
	if got := keys(m); got != "E-5* C-3* F-6 A-1 B-2 D-4 " {
		t.Errorf("a move crossed the boundary: %s", got)
	}
	m.Move("F-6", 1)
	if got := keys(m); got != "E-5* C-3* A-1 F-6 B-2 D-4 " {
		t.Errorf("move inside the unpinned group: %s", got)
	}

	// drag and drop is clamped to the dragged card's own group
	m.MoveAfter("E-5", "D-4") // a pinned card dropped far below: it stays in the pinned block
	if got := keys(m); got != "C-3* E-5* A-1 F-6 B-2 D-4 " {
		t.Errorf("pinned card dropped among unpinned: %s", got)
	}
	m.MoveAfter("D-4", "") // an unpinned card dropped at the very top: just below the pinned ones
	if got := keys(m); got != "C-3* E-5* D-4 A-1 F-6 B-2 " {
		t.Errorf("unpinned card dropped on top: %s", got)
	}
	m.MoveAfter("B-2", "C-3") // unpinned dropped right after a pinned card
	if got := keys(m); got != "C-3* E-5* B-2 D-4 A-1 F-6 " {
		t.Errorf("unpinned card dropped after a pinned one: %s", got)
	}
	m.MoveAfter("E-5", "") // pinned dropped at the top
	if got := keys(m); got != "E-5* C-3* B-2 D-4 A-1 F-6 " {
		t.Errorf("pinned card dropped on top: %s", got)
	}

	// unpin: goes to the top of the unpinned block
	m.SetPinned("E-5", false)
	if got := keys(m); got != "C-3* E-5 B-2 D-4 A-1 F-6 " {
		t.Errorf("after unpinning E: %s", got)
	}
	m.SetPinned("E-5", true)

	// pinning survives a restart, the rest of the timer is untouched by it
	m.Start("E-5", false)
	m.SetElapsed("C-3", 5*time.Minute)
	m.ResetAll() // "reset on exit" must not unpin
	if got := keys(Load(path)); got != "C-3* E-5* B-2 D-4 A-1 F-6 " {
		t.Errorf("after restart/reset: %s", got)
	}

	// removing a pinned timer leaves the rule intact; the limit counts pinned timers too
	m.Remove("C-3")
	if got := keys(m); got != "E-5* B-2 D-4 A-1 F-6 " {
		t.Errorf("after removing a pinned timer: %s", got)
	}
	for i := 0; len(m.Snapshot()) < MaxTimers; i++ {
		m.Add("K-"+string(rune('A'+i)), "", "", "")
	}
	if err := m.Add("OVER-1", "", "", ""); err != ErrTooMany {
		t.Errorf("limit with pinned timers: %v", err)
	}
}

// A damaged or hand-edited file may list unpinned timers before pinned ones: loading puts it right.
func TestLoadRestoresPinnedFirstOrder(t *testing.T) {
	path := t.TempDir() + "/timers.json"
	os.WriteFile(path, []byte(`[{"key":"A-1"},{"key":"B-2","pinned":true},{"key":"C-3"},{"key":"D-4","pinned":true}]`), 0o600)
	if got := keys(Load(path)); got != "B-2* D-4* A-1 C-3 " {
		t.Errorf("order after load: %s", got)
	}
	// a file written before pinning existed has no flag at all: nothing is pinned, order unchanged
	os.WriteFile(path, []byte(`[{"key":"A-1"},{"key":"B-2"}]`), 0o600)
	if got := keys(Load(path)); got != "A-1 B-2 " {
		t.Errorf("old file: %s", got)
	}
}
