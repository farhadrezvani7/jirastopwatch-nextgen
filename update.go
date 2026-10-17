package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Set at build time:
//
//	go build -ldflags "-X main.version=1.2.0 -X main.updateRepo=owner/name"
//
// With no updateRepo the update check is off (and hidden in Settings).
var (
	version    = "dev"
	updateRepo = ""
)

type updateInfo struct {
	Latest string `json:"latest"`
	URL    string `json:"url"`
	Newer  bool   `json:"newer"`
}

// newerVersion reports whether latest is a higher dotted version than current
// ("v1.10.0" > "1.9.2"). A non-numeric current ("dev") is never outdated.
func newerVersion(latest, current string) bool {
	parse := func(v string) ([]int, bool) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		var out []int
		for _, p := range strings.Split(v, ".") {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, false
			}
			out = append(out, n)
		}
		return out, len(out) > 0
	}
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < len(l) || i < len(c); i++ {
		var a, b int
		if i < len(l) {
			a = l[i]
		}
		if i < len(c) {
			b = c[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

// Release locations; tests point these at a local fake.
var (
	apiBase      = "https://api.github.com"
	downloadBase = "https://github.com"
)

type releaseInfo struct {
	Tag string `json:"tag_name"`
	URL string `json:"html_url"`
}

// latestRelease asks GitHub for the newest published release.
func latestRelease(ctx context.Context) (releaseInfo, error) {
	var rel releaseInfo
	if updateRepo == "" {
		return rel, errors.New("updates are not enabled in this build")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("User-Agent", "jirastopwatch-nextgen")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return rel, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.Tag == "" {
		return rel, errors.New("unexpected answer from GitHub")
	}
	return rel, nil
}

// checkUpdate asks GitHub for the latest release once, in the background.
func (s *server) checkUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if rel, err := latestRelease(ctx); err == nil {
		s.mu.Lock()
		s.update = updateInfo{rel.Tag, rel.URL, newerVersion(rel.Tag, version)}
		s.mu.Unlock()
	}
}

func (s *server) updateStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	u, off := s.update, s.cfg.NoUpdateCheck
	s.mu.RUnlock()
	if off {
		u.Newer = false
	}
	writeJSON(w, 200, u)
}

// updateCheck is the "Check for updates" button: it asks GitHub right now (even if the automatic check is off).
func (s *server) updateCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	rel, err := latestRelease(ctx)
	if err != nil {
		fail(w, 502, "err.updCheck|"+err.Error())
		return
	}
	u := updateInfo{rel.Tag, rel.URL, newerVersion(rel.Tag, version)}
	s.mu.Lock()
	s.update = u
	s.mu.Unlock()
	exe, err := exePath()
	writeJSON(w, 200, map[string]any{
		"latest": u.Latest, "url": u.URL, "newer": u.Newer, "current": version,
		"canInstall": err == nil && (writable(filepath.Dir(exe)) || canElevate()),
		"running":    s.runningKeys(),
	})
}

// updateApply downloads the newest release, swaps it in for the running binary and restarts the app.
func (s *server) updateApply(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.updating {
		s.mu.Unlock()
		fail(w, 409, "err.updInstall|already in progress")
		return
	}
	s.updating = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.updating = false; s.mu.Unlock() }()

	// Running timers must be settled (paused or logged) by the user first; the page offers "pause all".
	if keys := s.runningKeys(); len(keys) > 0 {
		fail(w, 409, "err.updRunning")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	// Ask GitHub again instead of trusting the page: what gets installed is decided here.
	rel, err := latestRelease(ctx)
	if err != nil {
		fail(w, 502, "err.updCheck|"+err.Error())
		return
	}
	if !newerVersion(rel.Tag, version) {
		fail(w, 409, "err.updInstall|no newer version")
		return
	}
	exe, err := exePath()
	if err != nil {
		fail(w, 500, "err.updInstall|"+err.Error())
		return
	}
	newPath, err := downloadUpdate(ctx, rel.Tag, exe)
	if err != nil {
		if errors.Is(err, errNotWritable) {
			fail(w, 409, "err.updNoWrite")
		} else {
			fail(w, 502, "err.updInstall|"+err.Error())
		}
		return
	}
	if err := swapBinary(newPath, exe); err != nil {
		_ = os.Remove(newPath)
		fail(w, 500, "err.updInstall|"+err.Error())
		return
	}
	s.mu.Lock()
	s.restartExe = exe
	quit := s.quit
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "version": rel.Tag})
	if quit != nil {
		go func() { time.Sleep(600 * time.Millisecond); quit() }() // let the answer reach the page first
	}
}

// runningKeys lists the issues whose timers are running right now.
func (s *server) runningKeys() []string {
	keys := []string{}
	for _, t := range s.timers.Snapshot() {
		if t.Running() {
			keys = append(keys, t.Key)
		}
	}
	return keys
}

func (s *server) setQuit(f func()) { s.mu.Lock(); s.quit = f; s.mu.Unlock() }

func (s *server) restartTarget() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.restartExe }
