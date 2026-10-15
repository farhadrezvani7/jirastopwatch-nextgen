package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeJira records the requests that matter and answers the few endpoints we use.
type fakeJira struct {
	mu   sync.Mutex
	reqs []string // "METHOD path?query"
	body map[string]string
}

func (f *fakeJira) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.RequestURI())
	if f.body == nil {
		f.body = map[string]string{}
	}
	f.body[r.Method+" "+r.URL.Path] = string(b)
	f.mu.Unlock()
	switch {
	case r.URL.Path == "/rest/api/2/myself":
		_, _ = w.Write([]byte(`{"displayName":"Test User","emailAddress":"t@example.com"}`))
	case r.URL.Path == "/rest/api/2/configuration":
		_, _ = w.Write([]byte(`{"timeTrackingConfiguration":{"workingHoursPerDay":8}}`))
	case r.URL.Path == "/rest/api/2/issue/ABC-1" && r.Method == "GET":
		_, _ = w.Write([]byte(`{"key":"ABC-1","fields":{"summary":"Sum","project":{"name":"P"},"status":{"name":"To Do"}}}`))
	case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == "GET":
		_, _ = w.Write([]byte(`{"transitions":[{"id":"21","name":"Start Progress"}]}`))
	default:
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{}`))
	}
}

func (f *fakeJira) has(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.reqs {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestWorklogUsesChosenTimezoneEstimateAndCommentMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	fj := &fakeJira{}
	jsrv := httptest.NewServer(fj)
	defer jsrv.Close()

	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	jar := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := jar.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}

	if c, m := call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`); c != 200 {
		t.Fatalf("login %d %v", c, m)
	}
	if c, m := call("POST", "/api/settings", `{"country":"IR","timezone":"Asia/Tehran","commentMode":"both","lang":"fa","startTransitions":"progress"}`); c != 200 {
		t.Fatalf("settings %d %v", c, m)
	}
	if c, _ := call("POST", "/api/settings", `{"timezone":"Mars/Base"}`); c != 400 {
		t.Errorf("bad zone accepted: %d", c)
	}
	if c, m := call("POST", "/api/timers", `{"key":"abc-1"}`); c != 200 {
		t.Fatalf("add %d %v", c, m)
	}
	call("POST", "/api/timers/ABC-1/start", "")
	// Start-transition runs in the background; wait for it.
	for i := 0; i < 250 && !fj.has("POST /rest/api/2/issue/ABC-1/transitions"); i++ {
		jsrvWait()
	}
	if !fj.has("POST /rest/api/2/issue/ABC-1/transitions") {
		t.Error("start transition was not applied")
	}

	c, m := call("POST", "/api/timers/ABC-1/log",
		`{"time":"1h 30m","started":"2026-10-15T09:00","comment":"hello","estimate":"manual","estimateValue":"30m"}`)
	if c != 200 {
		t.Fatalf("log %d %v", c, m)
	}
	fj.mu.Lock()
	defer fj.mu.Unlock()
	wl := fj.body["POST /rest/api/2/issue/ABC-1/worklog"]
	if !strings.Contains(wl, `"started":"2026-10-15T09:00:00.000+0330"`) {
		t.Errorf("worklog should start in Tehran time (+0330): %s", wl)
	}
	if !strings.Contains(wl, `"timeSpent":"1h 30m"`) || !strings.Contains(wl, `"comment":"hello"`) {
		t.Errorf("worklog body: %s", wl)
	}
	var sawEst, sawComment bool
	for _, r := range fj.reqs {
		sawEst = sawEst || (strings.Contains(r, "/worklog?") && strings.Contains(r, "adjustEstimate=manual") && strings.Contains(r, "reduceBy=30m"))
		sawComment = sawComment || r == "POST /rest/api/2/issue/ABC-1/comment"
	}
	if !sawEst {
		t.Errorf("estimate params missing: %v", fj.reqs)
	}
	if !sawComment {
		t.Errorf("comment mode 'both' should post a comment: %v", fj.reqs)
	}
}

func jsrvWait() { time.Sleep(20 * time.Millisecond) }

func TestGuardRejectsForeignHostAndMissingToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := newServer()
	h := s.handler()
	do := func(host, path string, cookie bool) int {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = host
		if cookie {
			r.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := do("evil.example:80", "/", true); c != 403 {
		t.Errorf("foreign host (DNS rebinding): %d", c)
	}
	if c := do("127.0.0.1:5000", "/api/state", false); c != 403 {
		t.Errorf("no cookie: %d", c)
	}
	if c := do("127.0.0.1:5000", "/api/state", true); c != 200 {
		t.Errorf("valid: %d", c)
	}
	if c := do("localhost:5000", "/?t="+s.token, false); c != 302 {
		t.Errorf("token bootstrap: %d", c)
	}
	if c := do("127.0.0.1:5000", "/?t=wrong", false); c != 200 {
		t.Errorf("wrong token just serves the static page: %d", c)
	}
	for _, p := range []string{"/", "/i18n.js", "/zones.json"} {
		if c := do("127.0.0.1:5000", p, false); c != 200 {
			t.Errorf("static %s: %d", p, c)
		}
	}
}
