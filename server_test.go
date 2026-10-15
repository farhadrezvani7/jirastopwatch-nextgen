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

// A remembered sign-in must survive an app restart: the page logs in again with an empty body.
func TestRememberedLoginSurvivesRestart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	jsrv := httptest.NewServer(&fakeJira{})
	defer jsrv.Close()

	post := func(s *server, path, body string) (int, map[string]any) {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, req)
		var m map[string]any
		_ = json.NewDecoder(w.Body).Decode(&m)
		return w.Code, m
	}

	first := newServer()
	if c, m := post(first, "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p","remember":true}`); c != 200 {
		t.Fatalf("login %d %v", c, m)
	}

	second := newServer() // "reopen the app"
	if second.cfg.Token == "" {
		t.Fatal("token was not remembered")
	}
	if c, m := post(second, "/api/login", `{}`); c != 200 {
		t.Fatalf("auto sign-in with saved credentials: %d %v", c, m)
	}

	// ...and the sign-in after that one too (auto sign-in must not forget the token).
	third := newServer()
	if c, m := post(third, "/api/login", `{}`); c != 200 {
		t.Fatalf("second auto sign-in: %d %v", c, m)
	}
	if newServer().cfg.Token == "" {
		t.Fatal("auto sign-in wiped the remembered token")
	}

	// Signing out forgets the token, and an empty login then fails.
	post(second, "/api/logout", "")
	if c, _ := post(newServer(), "/api/login", `{}`); c == 200 {
		t.Error("signed-out user was logged in again")
	}
}

// The worklog should end when it is logged: 3 AM Tehran + 1h30m of work starts at 01:30, not when the timer was first started.
func TestDefaultStartEndsNow(t *testing.T) {
	tehran, _ := time.LoadLocation("Asia/Tehran")
	now := time.Date(2026, 10, 16, 3, 0, 20, 0, tehran)
	if got := defaultStart(now, 89*time.Minute+10*time.Second).In(tehran).Format(localLayout); got != "2026-10-16T01:30" {
		t.Errorf("default start = %s, want 2026-10-16T01:30", got)
	}
	if got := defaultStart(now, 0).In(tehran).Format(localLayout); got != "2026-10-16T02:59" {
		t.Errorf("empty timer start = %s", got)
	}
}
