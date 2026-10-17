package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jirastopwatch-nextgen/internal/fsutil"
	"jirastopwatch-nextgen/internal/store"
)

// Two copies of the app would each keep their own in-memory timers and overwrite each other's
// timers.json, losing time. So the running copy holds an OS lock for its whole life; a second launch
// cannot take it, shows the first copy's window instead and exits. The lock dies with the process, so
// a crash or power cut never leaves the app "stuck as already running".

type instanceInfo struct {
	PID int    `json:"pid"`
	URL string `json:"url"`
}

var lockHandle *os.File

func instanceFile() string { return filepath.Join(store.Dir(), "instance.json") }
func aliveFile() string    { return filepath.Join(store.Dir(), "alive") }

// acquireLock returns false only when another copy of the app really holds the lock. If the lock
// file itself can't be used (read-only folder, odd file system) the app still starts.
func acquireLock() bool {
	if err := os.MkdirAll(store.Dir(), 0o700); err != nil {
		return true
	}
	f, err := os.OpenFile(filepath.Join(store.Dir(), "app.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	if lockFile(f) != nil {
		f.Close()
		return false
	}
	lockHandle = f
	return true
}

func releaseLock() {
	if lockHandle != nil {
		lockHandle.Close()
		lockHandle = nil
	}
}

// existingInstance returns the page URL recorded by a running copy, if that URL still answers like our server.
func existingInstance() (string, bool) {
	var in instanceInfo
	b, err := os.ReadFile(instanceFile())
	if err != nil || json.Unmarshal(b, &in) != nil || in.URL == "" || in.PID == os.Getpid() {
		return "", false
	}
	c := &http.Client{Timeout: 1500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(in.URL)
	if err != nil {
		return "", false
	}
	resp.Body.Close()
	// Our own server answers the token URL with a redirect that sets the "sw" cookie; anything else
	// on that port (a different program that reused it) is not us.
	for _, c := range resp.Cookies() {
		if c.Name == "sw" && resp.StatusCode == http.StatusFound {
			return in.URL, true
		}
	}
	return "", false
}

// waitForInstance polls until the running copy has published its URL (it does so right after it starts).
func waitForInstance(max time.Duration) string {
	for end := time.Now().Add(max); ; {
		if u, ok := existingInstance(); ok {
			return u
		}
		if time.Now().After(end) {
			return ""
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// previousRunEndedBadly: the last copy left its marker behind, so it never exited cleanly
// (crash, power cut, forced shutdown). Called while holding the lock.
func previousRunEndedBadly() bool {
	_, err := os.Stat(instanceFile())
	return err == nil
}

func writeInstance(url string) {
	b, _ := json.Marshal(instanceInfo{PID: os.Getpid(), URL: url})
	_ = fsutil.WriteAtomicOpts(instanceFile(), b, 0o600, false)
}

// heartbeat records "still running" every few seconds, so after an unclean end the app knows when
// it last was alive and does not count the time the computer was off as work.
func heartbeat(stop <-chan struct{}) {
	beat := func() { _ = os.WriteFile(aliveFile(), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o600) }
	beat()
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			beat()
		case <-stop:
			return
		}
	}
}

func lastAlive() time.Time {
	b, err := os.ReadFile(aliveFile())
	n, perr := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || perr != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// removeInstance marks a clean exit (also done before an update restarts the app).
func removeInstance() {
	_ = os.Remove(instanceFile())
	_ = os.Remove(aliveFile())
}
