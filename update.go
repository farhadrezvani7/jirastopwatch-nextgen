package main

import (
	"context"
	"encoding/json"
	"net/http"
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

// checkUpdate asks GitHub for the latest release once, in the background.
func (s *server) checkUpdate() {
	if updateRepo == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "jirastopwatch-nextgen")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var rel struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&rel) != nil {
		return
	}
	s.mu.Lock()
	s.update = updateInfo{rel.Tag, rel.URL, newerVersion(rel.Tag, version)}
	s.mu.Unlock()
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
