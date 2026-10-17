// Package timer keeps the set of issue stopwatches and persists them.
package timer

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"jirastopwatch-nextgen/internal/fsutil"
)

// Timer is one issue stopwatch. Accumulated is time banked from earlier
// runs; while RunningSince is set, wall-clock time since then is added.
type Timer struct {
	Key          string        `json:"key"`
	Summary      string        `json:"summary"`
	Project      string        `json:"project"`
	Status       string        `json:"status"`
	Accumulated  time.Duration `json:"accumulated"`
	RunningSince *time.Time    `json:"runningSince,omitempty"`
	FirstStart   *time.Time    `json:"firstStart,omitempty"`
	Comment      string        `json:"comment,omitempty"` // unsent worklog note, kept across restarts
}

func (t *Timer) Elapsed(now time.Time) time.Duration {
	if t.RunningSince == nil {
		return t.Accumulated
	}
	return t.Accumulated + runSpan(now, *t.RunningSince)
}

// runSpan is how long a run has lasted. If the computer's clock was set back (time sync, DST
// mistakes, a wrong date fixed by hand) the span would be negative and eat banked time: count zero.
func runSpan(now, since time.Time) time.Duration {
	if d := now.Sub(since); d > 0 {
		return d
	}
	return 0
}

func (t *Timer) Running() bool { return t.RunningSince != nil }

type Manager struct {
	mu     sync.Mutex
	path   string
	timers []*Timer // display order
	Now    func() time.Time

	saveErr string // why the last write to disk failed ("" = fine)
}

// SaveError is the reason the timers could not be written to disk the last time (disk full,
// read-only folder, ...), or "" when everything is saved. The page tells the user.
func (m *Manager) SaveError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveErr
}

var (
	ErrNotFound = errors.New("err.timerNotFound")
	ErrTooMany  = errors.New("err.maxIssues")
)

// MaxTimers matches the original StopWatch limit.
const MaxTimers = 20

func Load(path string) *Manager {
	m := &Manager{path: path, Now: time.Now}
	// A damaged file (power cut mid-write) falls back to the last good copy; see fsutil.ReadJSON.
	_, _ = fsutil.ReadJSON(path, &m.timers)
	return m
}

func (m *Manager) save() {
	if m.path == "" {
		return
	}
	b, _ := json.MarshalIndent(m.timers, "", " ")
	if err := fsutil.WriteAtomic(m.path, b, 0o600); err != nil {
		m.saveErr = err.Error()
	} else {
		m.saveErr = ""
	}
}

func (m *Manager) find(key string) *Timer {
	for _, t := range m.timers {
		if t.Key == key {
			return t
		}
	}
	return nil
}

// Snapshot returns copies of all timers.
func (m *Manager) Snapshot() []Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Timer, len(m.timers))
	for i, t := range m.timers {
		out[i] = *t
	}
	return out
}

func (m *Manager) Get(key string) (Timer, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.find(key); t != nil {
		return *t, true
	}
	return Timer{}, false
}

// Add inserts a timer at the top (or refreshes metadata if it exists).
func (m *Manager) Add(key, summary, project, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.find(key); t != nil {
		t.Summary, t.Project, t.Status = summary, project, status
	} else {
		if len(m.timers) >= MaxTimers {
			return ErrTooMany
		}
		m.timers = append([]*Timer{{Key: key, Summary: summary, Project: project, Status: status}}, m.timers...)
	}
	m.save()
	return nil
}

func (m *Manager) Remove(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.timers {
		if t.Key == key {
			m.timers = append(m.timers[:i], m.timers[i+1:]...)
			break
		}
	}
	m.save()
}

// Start starts key; if pauseOthers, every other running timer is paused.
func (m *Manager) Start(key string, pauseOthers bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.find(key)
	if t == nil {
		return ErrNotFound
	}
	now := m.Now()
	if pauseOthers {
		for _, o := range m.timers {
			if o != t {
				pause(o, now)
			}
		}
	}
	if t.RunningSince == nil {
		t.RunningSince = &now
		if t.FirstStart == nil {
			t.FirstStart = &now
		}
	}
	m.save()
	return nil
}

func pause(t *Timer, now time.Time) {
	if t.RunningSince != nil {
		t.Accumulated += runSpan(now, *t.RunningSince)
		t.RunningSince = nil
	}
}

func (m *Manager) Pause(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.find(key)
	if t == nil {
		return ErrNotFound
	}
	pause(t, m.Now())
	m.save()
	return nil
}

func (m *Manager) PauseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	for _, t := range m.timers {
		pause(t, now)
	}
	m.save()
}

// SetElapsed overrides the elapsed time, keeping the running state.
func (m *Manager) SetElapsed(key string, d time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.find(key)
	if t == nil {
		return ErrNotFound
	}
	t.Accumulated = d
	if t.RunningSince != nil {
		now := m.Now()
		t.RunningSince = &now
	}
	m.save()
	return nil
}

// Reset zeroes and stops the timer (after a worklog was posted).
func (m *Manager) Reset(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.find(key); t != nil {
		t.Accumulated, t.RunningSince, t.FirstStart, t.Comment = 0, nil, nil, ""
	}
	m.save()
}

// Move shifts a timer one place up (dir<0) or down (dir>0) in the list.
func (m *Manager) Move(key string, dir int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.timers {
		if t.Key != key {
			continue
		}
		j := i + dir
		if j >= 0 && j < len(m.timers) {
			m.timers[i], m.timers[j] = m.timers[j], m.timers[i]
			m.save()
		}
		return nil
	}
	return ErrNotFound
}

// MoveAfter puts key right after the timer named after ("" = first place), for drag and drop.
func (m *Manager) MoveAfter(key, after string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	from := -1
	for i, t := range m.timers {
		if t.Key == key {
			from = i
		}
	}
	if from < 0 {
		return ErrNotFound
	}
	item := m.timers[from]
	rest := append(append([]*Timer{}, m.timers[:from]...), m.timers[from+1:]...)
	at := 0
	if after != "" {
		at = -1
		for i, t := range rest {
			if t.Key == after {
				at = i + 1
			}
		}
		if at < 0 {
			return ErrNotFound
		}
	}
	m.timers = append(rest[:at], append([]*Timer{item}, rest[at:]...)...)
	m.save()
	return nil
}

// RewindTo pauses, as of the moment `at` (not "now"), those timers that are still running from
// exactly the start time recorded in since. It is used only after the user confirmed that the
// computer was off (see the crash recovery question); anything the user has since changed is left alone.
func (m *Manager) RewindTo(since map[string]time.Time, at time.Time) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for _, t := range m.timers {
		if s, ok := since[t.Key]; ok && t.RunningSince != nil && t.RunningSince.Equal(s) {
			pause(t, at)
			keys = append(keys, t.Key)
		}
	}
	if len(keys) > 0 {
		m.save()
	}
	return keys
}

// PauseRunning pauses every running timer and returns their keys.
func (m *Manager) PauseRunning() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	var keys []string
	for _, t := range m.timers {
		if t.RunningSince != nil {
			keys = append(keys, t.Key)
			pause(t, now)
		}
	}
	if len(keys) > 0 {
		m.save()
	}
	return keys
}

// Resume restarts the given timers (those that still exist and are paused).
func (m *Manager) Resume(keys []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	for _, k := range keys {
		if t := m.find(k); t != nil && t.RunningSince == nil {
			n := now
			t.RunningSince = &n
			if t.FirstStart == nil {
				t.FirstStart = &n
			}
		}
	}
	m.save()
}

// ResetAll zeroes and stops every timer.
func (m *Manager) ResetAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.timers {
		t.Accumulated, t.RunningSince, t.FirstStart, t.Comment = 0, nil, nil, ""
	}
	m.save()
}

func (m *Manager) SetComment(key, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.find(key)
	if t == nil {
		return ErrNotFound
	}
	t.Comment = text
	m.save()
	return nil
}
