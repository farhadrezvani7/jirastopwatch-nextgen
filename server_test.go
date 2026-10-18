package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"jirastopwatch-nextgen/internal/jira"
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
		_, _ = w.Write([]byte(`{"displayName":"Test User","emailAddress":"t@example.com","timeZone":"Asia/Tehran"}`))
	case r.URL.Path == "/rest/api/2/serverInfo":
		_, _ = w.Write([]byte(`{"serverTime":"2026-10-15T23:34:03.378+0000"}`)) // a Jira server set to UTC
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
	if c, _ := call("POST", "/api/settings", `{"shadows":true,"fontSize":12,"asideClosed":true}`); c != 200 {
		t.Errorf("look settings rejected: %d", c)
	}
	if _, m := call("GET", "/api/session", ""); m["prefs"].(map[string]any)["fontSize"] != float64(12) || m["prefs"].(map[string]any)["shadows"] != true {
		t.Errorf("look settings not kept: %v", m["prefs"])
	}
	if c, _ := call("POST", "/api/settings", `{"fontSize":99}`); c != 400 {
		t.Errorf("absurd font size accepted: %d", c)
	}
	if c, _ := call("GET", "/api/issues?src=all&jql="+strings.Repeat("a", maxJQL+1), ""); c != 400 {
		t.Errorf("oversized JQL accepted: %d", c)
	}
	call("GET", "/api/issues?src=all", "")
	if !fj.has("created+%3E%3D+%222000-01-01%22+ORDER+BY+updated+DESC") {
		t.Errorf("the unfiltered 'all' query is not the default: %v", fj.reqs)
	}
	call("GET", "/api/issues?src=all&cursor=50&jql=status%3D%22In+Progress%22+ORDER+BY+updated+DESC", "")
	if !fj.has(`jql=status%3D%22In+Progress%22+ORDER+BY+updated+DESC`) {
		t.Errorf("the JQL did not reach Jira: %v", fj.reqs)
	}
	if c, _ := call("POST", "/api/settings", `{"lastJql":"project = ABC"}`); c != 200 {
		t.Errorf("lastJql rejected: %d", c)
	}
	// "Move to the top when started": off by default, on after the setting.
	if c, _ := call("POST", "/api/timers", `{"key":"abc-1"}`); c != 200 {
		t.Fatalf("add abc-1")
	}
	s.timers.Add("ZZ-9", "s", "p", "st") // Add puts the newest first: ZZ-9, ABC-1
	call("POST", "/api/timers/ABC-1/start", "")
	if ts := s.timers.Snapshot(); ts[0].Key != "ZZ-9" {
		t.Errorf("moved to top although the setting is off: %v", ts[0].Key)
	}
	call("POST", "/api/settings", `{"startOnTop":true}`)
	call("POST", "/api/timers/ABC-1/pause", "")
	call("POST", "/api/timers/ABC-1/start", "")
	if ts := s.timers.Snapshot(); ts[0].Key != "ABC-1" {
		t.Errorf("a started timer did not move to the top: %v", ts[0].Key)
	}
	call("POST", "/api/settings", `{"startOnTop":false}`)
	s.timers.Remove("ZZ-9")
	s.timers.Remove("ABC-1")
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

// Jira groups by day on the server's clock. In "server clock" mode the time the user entered (Tehran, 03:21)
// must be written as 03:21 on the server's UTC clock; in the standard mode it stays the exact moment.
func TestServerClockModeWritesLocalReadingOnServerClock(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	fj := &fakeJira{}
	jsrv := httptest.NewServer(fj)
	defer jsrv.Close()
	s := newServer()
	call := func(method, path, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, req)
		var m map[string]any
		_ = json.NewDecoder(w.Body).Decode(&m)
		return w.Code, m
	}
	tm := func() map[string]any { _, m := call("GET", "/api/session", ""); return m["time"].(map[string]any) }
	logAt := func() string {
		fj.mu.Lock()
		fj.body = nil
		fj.mu.Unlock()
		if c, m := call("POST", "/api/timers/ABC-1/log", `{"time":"1m","started":"2026-10-16T03:21","estimate":"leave"}`); c != 200 {
			t.Fatalf("log %d %v", c, m)
		}
		fj.mu.Lock()
		defer fj.mu.Unlock()
		return fj.body["POST /rest/api/2/issue/ABC-1/worklog"]
	}

	call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`)
	call("POST", "/api/settings", `{"timezone":"Asia/Tehran","country":"IR"}`)
	call("POST", "/api/timers", `{"key":"abc-1"}`)

	if got := tm(); got["advice"] != "ask" || got["serverOffsetSec"] != float64(0) || got["localOffsetSec"] != float64(12600) {
		t.Fatalf("expected a clock-difference notice before any choice: %v", got)
	}
	if wl := logAt(); !strings.Contains(wl, `"started":"2026-10-16T03:21:00.000+0330"`) {
		t.Errorf("standard mode must keep the exact moment: %s", wl)
	}

	// Answering "keep exact" (the page sends the notice's key back) stops the notice from coming back.
	call("POST", "/api/settings", `{"timeAck":"`+tm()["key"].(string)+`"}`)
	if got := tm(); got["advice"] != "" {
		t.Errorf("notice came back after an answer: %v", got)
	}
	// The server-clock choice writes the same reading on the server's clock.
	call("POST", "/api/settings", `{"timeMode":"server"}`)
	if wl := logAt(); !strings.Contains(wl, `"started":"2026-10-16T03:21:00.000+0000"`) {
		t.Errorf("server-clock mode: %s", wl)
	}
	// ...and going back to standard restores exact moments.
	call("POST", "/api/settings", `{"timeMode":""}`)
	if wl := logAt(); !strings.Contains(wl, `+0330"`) {
		t.Errorf("did not return to exact moments: %s", wl)
	}

	// Once the server's zone matches the user's, server-clock mode offers to switch back (once).
	call("POST", "/api/settings", `{"timeMode":"server","timezone":"UTC","country":"GB"}`)
	if got := tm(); got["advice"] != "fixed" {
		t.Fatalf("expected the 'server now matches' notice: %v", got)
	}
	call("POST", "/api/settings", `{"timeAck":"fixed"}`)
	if got := tm(); got["advice"] != "" {
		t.Errorf("'fixed' notice repeated: %v", got)
	}
}

func TestTimeAdvice(t *testing.T) {
	m := clockState{Known: true, ServerOffset: 0, LocalOffset: 12600, ProfileOffset: 12600, HasProfile: true}
	for _, c := range []struct {
		name      string
		mode, ack string
		st        clockState
		want      string
	}{
		{"mismatch asks", "", "", m, "ask"},
		{"answered", "", mismatchKey(m), m, ""},
		{"server unknown", "", "", clockState{LocalOffset: 12600, ProfileOffset: 12600, HasProfile: true}, ""},
		{"same zone", "", "", clockState{Known: true, ServerOffset: 12600, LocalOffset: 12600, ProfileOffset: 12600, HasProfile: true}, ""},
		// travelling: the Jira profile is elsewhere, so this is not the server's mistake
		{"profile elsewhere", "", "", clockState{Known: true, ServerOffset: 0, LocalOffset: 12600, ProfileOffset: 0, HasProfile: true}, ""},
		{"no profile zone", "", "", clockState{Known: true, ServerOffset: 0, LocalOffset: 12600}, ""},
		{"server mode, still off", "server", "", m, ""},
		{"server mode, server fixed", "server", "", clockState{Known: true, ServerOffset: 12600, LocalOffset: 12600}, "fixed"},
		{"server mode, fixed answered", "server", "fixed", clockState{Known: true, ServerOffset: 12600, LocalOffset: 12600}, ""},
	} {
		if got := timeAdvice(c.mode, c.ack, c.st); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// A dropped connection can leave a worklog saved in Jira although the app never saw the answer.
// Logging again must not book it twice, must still free the timer, and a separate comment must
// only be posted after the time is safe.
func TestLogWorkRetryDoesNotDuplicateAndCommentComesAfter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	var seq []string
	existing := false
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1","timeZone":"UTC"}`))
		case r.URL.Path == "/rest/api/2/serverInfo", r.URL.Path == "/rest/api/2/configuration":
			w.WriteHeader(404)
		case strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == "GET":
			if existing {
				_, _ = w.Write([]byte(`{"total":1,"worklogs":[{"author":{"name":"me","key":"U1"},"started":"2026-10-17T09:00:00.000+0000","timeSpentSeconds":1800}]}`))
			} else {
				_, _ = w.Write([]byte(`{"total":0,"worklogs":[]}`))
			}
		default:
			seq = append(seq, r.Method+" "+r.URL.Path)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
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
	call("POST", "/api/settings", `{"commentMode":"comment","timezone":"UTC"}`)
	_ = s.timers.Add("ABC-1", "s", "p", "st")
	_ = s.timers.SetElapsed("ABC-1", 30*time.Minute)
	body := `{"time":"30m","started":"2026-10-17T09:00","comment":"did it"}`

	// first attempt: saved normally; the worklog is posted before the comment
	if c, m := call("POST", "/api/timers/ABC-1/log", body); c != 200 || m["warning"] != nil {
		t.Fatalf("first log: %d %v", c, m)
	}
	if len(seq) != 2 || !strings.HasSuffix(seq[0], "/worklog") || !strings.HasSuffix(seq[1], "/comment") {
		t.Errorf("order of requests: %v", seq)
	}
	// the answer was lost on the way: the timer still has its time, Jira already has the entry
	_ = s.timers.SetElapsed("ABC-1", 30*time.Minute)
	mu.Lock()
	existing, seq = true, nil
	mu.Unlock()
	c, m := call("POST", "/api/timers/ABC-1/log", body)
	if c != 200 || !strings.HasPrefix(m["warning"].(string), "warn.alreadyLogged") {
		t.Errorf("retry: %d %v", c, m)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seq) != 0 {
		t.Errorf("the retry posted again: %v", seq)
	}
	if g, _ := s.timers.Get("ABC-1"); g.Elapsed(time.Now()) != 0 {
		t.Errorf("timer not freed after the retry: %v", g.Elapsed(time.Now()))
	}
}

func TestPinEndpointAndStartOnTopRespectsPins(t *testing.T) {
	s := newTestServer(t)
	do := func(method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, req)
		return w.Code
	}
	for _, k := range []string{"C-3", "B-2", "A-1"} {
		_ = s.timers.Add(k, "", "", "")
	}
	order := func() string {
		out := ""
		for _, x := range s.timers.Snapshot() {
			out += x.Key
			if x.Pinned {
				out += "*"
			}
			out += " "
		}
		return out
	}
	if c := do("POST", "/api/timers/C-3/pin", `{"pinned":true}`); c != 200 || order() != "C-3* A-1 B-2 " {
		t.Fatalf("pin: %d %s", c, order())
	}
	if do("POST", "/api/timers/C-3/pin", `{}`) != 400 || do("POST", "/api/timers/Z-9/pin", `{"pinned":true}`) != 404 {
		t.Error("bad pin requests accepted")
	}
	// start-on-top: an unpinned timer goes just below the pinned ones; a pinned one is not moved
	do("POST", "/api/settings", `{"startOnTop":true}`)
	do("POST", "/api/timers/B-2/start", "")
	if order() != "C-3* B-2 A-1 " {
		t.Errorf("start-on-top with a pin: %s", order())
	}
	do("POST", "/api/timers/C-3/pin", `{"pinned":false}`)
	do("POST", "/api/timers/C-3/pin", `{"pinned":true}`)
	do("POST", "/api/timers/A-1/pin", `{"pinned":true}`) // C-3* A-1* B-2
	do("POST", "/api/timers/A-1/start", "")
	if order() != "C-3* A-1* B-2 " {
		t.Errorf("starting a pinned timer reordered the pins: %s", order())
	}
	// the state the page gets carries the flag
	req := httptest.NewRequest("GET", "/api/state", nil)
	req.Host = "127.0.0.1"
	req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"pinned":true`) {
		t.Errorf("state lacks the pinned flag: %s", w.Body)
	}
}

func TestQueueEndpoints(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1"}`))
		case r.URL.Path == "/rest/servicedeskapi/servicedesk":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"20","projectKey":"HD","projectName":"HelpDesk"}]}`))
		case r.URL.Path == "/rest/servicedeskapi/servicedesk/20/queue":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"425","name":"Support","issueCount":2}]}`))
		case r.URL.Path == "/rest/servicedeskapi/servicedesk/20/queue/425/issue":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"HD-9"},{"key":"HD-3"}]}`))
		case r.URL.Path == "/rest/api/2/search":
			_, _ = w.Write([]byte(`{"issues":[{"key":"HD-3","fields":{"summary":"c","project":{"name":"P"},"status":{"name":"Open"}}},{"key":"HD-9","fields":{"summary":"n","project":{"name":"P"},"status":{"name":"Open"}}}],"total":2}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if c, _ := call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`); c != 200 {
		t.Fatal("login")
	}
	if c, b := call("GET", "/api/queues/desks", ""); c != 200 || !strings.Contains(b, `"key":"HD"`) {
		t.Errorf("desks: %d %s", c, b)
	}
	if c, b := call("GET", "/api/queues?desk=20", ""); c != 200 || !strings.Contains(b, `"name":"Support"`) || !strings.Contains(b, `"count":2`) {
		t.Errorf("queues: %d %s", c, b)
	}
	// the issues come in the queue's order (HD-9 first), in the usual {issues,next} shape
	if c, b := call("GET", "/api/issues?src=queue&desk=20&queue=425", ""); c != 200 || strings.Index(b, "HD-9") > strings.Index(b, "HD-3") || !strings.Contains(b, `"next":""`) {
		t.Errorf("queue issues: %d %s", c, b)
	}
	for _, bad := range []string{"/api/queues?desk=", "/api/queues?desk=1/../2", "/api/issues?src=queue&desk=20&queue=x", "/api/issues?src=queue&queue=425"} {
		if c, _ := call("GET", bad, ""); c != 400 {
			t.Errorf("%s accepted: %d", bad, c)
		}
	}
}

// The timer was running on the wrong card: the time is booked on the issue the user picks, and the
// wrong card's timer is the one that is used up. Without a target nothing changes.
func TestLogWorkToAnotherIssueConsumesTheSourceTimer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	var seq []string
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1","timeZone":"UTC"}`))
		case r.URL.Path == "/rest/api/2/serverInfo", r.URL.Path == "/rest/api/2/configuration":
			w.WriteHeader(404)
		case strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == "GET":
			_, _ = w.Write([]byte(`{"total":0,"worklogs":[]}`))
		default:
			seq = append(seq, r.Method+" "+r.URL.Path)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(path, body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	if c, _ := call("/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`); c != 200 {
		t.Fatal("login")
	}
	call("/api/settings", `{"timezone":"UTC"}`)
	_ = s.timers.Add("WRONG-1", "wrong card", "p", "st")
	_ = s.timers.SetElapsed("WRONG-1", 30*time.Minute)
	body := func(target string) string {
		return `{"time":"30m","started":"2026-10-17T09:00","comment":"","target":"` + target + `","transition":"31"}`
	}
	if c, _ := call("/api/timers/WRONG-1/log", body("not a key")); c != 400 {
		t.Errorf("a malformed target was accepted: %d", c)
	}
	if g, _ := s.timers.Get("WRONG-1"); g.Elapsed(time.Now()) != 30*time.Minute || len(seq) != 0 {
		t.Fatalf("a refused request changed something: %v %v", g.Elapsed(time.Now()), seq)
	}
	c, m := call("/api/timers/WRONG-1/log", body("right-7"))
	if c != 200 || m["target"] != "RIGHT-7" {
		t.Fatalf("log to another issue: %d %v", c, m)
	}
	mu.Lock()
	got := strings.Join(seq, ",")
	mu.Unlock()
	if !strings.Contains(got, "POST /rest/api/2/issue/RIGHT-7/worklog") || !strings.Contains(got, "POST /rest/api/2/issue/RIGHT-7/transitions") || strings.Contains(got, "WRONG-1") {
		t.Errorf("everything must go to the chosen issue only: %s", got)
	}
	if g, _ := s.timers.Get("WRONG-1"); g.Elapsed(time.Now()) != 0 {
		t.Errorf("the wrong card's timer was not consumed: %v", g.Elapsed(time.Now()))
	}
	// the card's own key as target behaves like no target
	mu.Lock()
	seq = nil
	mu.Unlock()
	_ = s.timers.SetElapsed("WRONG-1", 30*time.Minute)
	if c, m := call("/api/timers/WRONG-1/log", body("wrong-1")); c != 200 || m["target"] != "WRONG-1" {
		t.Fatalf("own key: %d %v", c, m)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(seq, ","), "POST /rest/api/2/issue/WRONG-1/worklog") {
		t.Errorf("own key did not log on the card: %v", seq)
	}
}

// Typing an issue key character by character ("HD-7", "HD-72"...) must keep returning results, even
// though Jira refuses a query that names an issue key that does not exist.
func TestKeyShapedSearchFallsBackToTextWhenTheKeyDoesNotExist(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	var jqls []string
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1"}`))
		case "/rest/api/2/search":
			jql := r.URL.Query().Get("jql")
			mu.Lock()
			jqls = append(jqls, jql)
			mu.Unlock()
			if strings.Contains(jql, `issuekey = "HD-7"`) {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"errorMessages":["An issue with key 'HD-7' does not exist for field 'issuekey'."]}`))
				return
			}
			_, _ = w.Write([]byte(`{"issues":[{"key":"HD-70","fields":{"summary":"s","project":{"name":"P"},"status":{"name":"Open"}}}],"total":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if c, _ := call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`); c != 200 {
		t.Fatal("login")
	}
	c, b := call("GET", "/api/issues?src=search&q=HD-7", "")
	if c != 200 || !strings.Contains(b, "HD-70") {
		t.Fatalf("a partial key gave %d %s", c, b)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(jqls) != 2 || strings.Contains(jqls[1], "issuekey") {
		t.Errorf("expected the key query and then a text-only retry: %v", jqls)
	}
}

func TestMyWorkEndpointsAddEditDeleteAndValidation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	var reqs []string
	var bodies []string
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		bodies = append(bodies, string(b))
		mu.Unlock()
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1"}`))
		case r.URL.Path == "/rest/api/2/serverInfo", r.URL.Path == "/rest/api/2/configuration":
			w.WriteHeader(404)
		case r.URL.Path == "/rest/api/2/search":
			_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{"summary":"First","project":{"name":"P"},"status":{"name":"Open"}}}],"total":1}`))
		case r.URL.Path == "/rest/api/2/issue/A-1/worklog" && r.Method == "GET":
			_, _ = w.Write([]byte(`{"total":1,"worklogs":[{"id":"11","author":{"name":"me","key":"U1"},"started":"2026-10-16T09:00:00.000+0000","timeSpentSeconds":5400,"timeSpent":"1h 30m","comment":"c"}]}`))
		default:
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if c, _ := call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`); c != 200 {
		t.Fatal("login")
	}
	call("POST", "/api/settings", `{"timezone":"UTC"}`)

	// read: Jira's own entries, with the day taken from the started text
	c, b := call("GET", "/api/mywork?from=2026-10-15&to=2026-10-17", "")
	if c != 200 || !strings.Contains(b, `"day":"2026-10-16"`) || !strings.Contains(b, `"timeSpent":"1h 30m"`) || !strings.Contains(b, `"key":"A-1"`) {
		t.Fatalf("mywork: %d %s", c, b)
	}
	for _, bad := range []string{"/api/mywork", "/api/mywork?from=2026-10-17&to=2026-10-15", "/api/mywork?from=x&to=y", "/api/mywork?from=2026-01-01&to=2026-12-31"} {
		if c, _ := call("GET", bad, ""); c != 400 {
			t.Errorf("%s accepted: %d", bad, c)
		}
	}

	// add without any timer: goes to the chosen issue, started as the user's local clock
	mu.Lock()
	reqs, bodies = nil, nil
	mu.Unlock()
	if c, b := call("POST", "/api/worklogs", `{"key":"a-1","time":"45m","started":"2026-10-16T14:00","comment":"manual"}`); c != 200 {
		t.Fatalf("add: %d %s", c, b)
	}
	mu.Lock()
	joined := strings.Join(reqs, ",")
	mu.Unlock()
	if !strings.Contains(joined, "POST /rest/api/2/issue/A-1/worklog?adjustEstimate=auto") && !strings.Contains(joined, "POST /rest/api/2/issue/A-1/worklog?") {
		t.Errorf("add did not reach Jira: %s", joined)
	}
	if len(s.timers.Snapshot()) != 0 {
		t.Error("a manual log created a timer")
	}
	for _, bad := range []string{`{"key":"nope","time":"1h","started":"2026-10-16T14:00"}`, `{"key":"A-1","time":"0","started":"2026-10-16T14:00"}`, `{"key":"A-1","time":"1h","started":"garbage"}`} {
		if c, _ := call("POST", "/api/worklogs", bad); c != 400 {
			t.Errorf("accepted %s: %d", bad, c)
		}
	}

	// edit keeps the zone Jira reported; delete goes to the right worklog
	mu.Lock()
	reqs, bodies = nil, nil
	mu.Unlock()
	if c, b := call("PUT", "/api/worklogs/a-1/11", `{"time":"2h","started":"2026-10-16T09:30","offset":"+0330","comment":"edited"}`); c != 200 {
		t.Fatalf("edit: %d %s", c, b)
	}
	if c, b := call("DELETE", "/api/worklogs/A-1/11", ""); c != 200 {
		t.Fatalf("delete: %d %s", c, b)
	}
	mu.Lock()
	if len(reqs) != 2 || !strings.HasPrefix(reqs[0], "PUT /rest/api/2/issue/A-1/worklog/11?") || !strings.Contains(bodies[0], `"started":"2026-10-16T09:30:00.000+0330"`) ||
		!strings.Contains(bodies[0], `"timeSpent":"2h 0m"`) || !strings.Contains(bodies[0], `"comment":"edited"`) || !strings.HasPrefix(reqs[1], "DELETE /rest/api/2/issue/A-1/worklog/11?") {
		t.Errorf("edit/delete requests: %v %v", reqs, bodies)
	}
	mu.Unlock()
	for _, bad := range []string{"/api/worklogs/A-1/abc", "/api/worklogs/bad/11"} {
		if c, _ := call("DELETE", bad, ""); c != 400 {
			t.Errorf("DELETE %s accepted: %d", bad, c)
		}
	}
	if c, _ := call("PUT", "/api/worklogs/A-1/11", `{"time":"2h","started":"2026-10-16T09:30","offset":"zz"}`); c != 400 {
		t.Errorf("a bad offset was accepted: %d", c)
	}
	if c, _ := call("POST", "/api/settings", `{"weekStart":"sat"}`); c != 200 {
		t.Errorf("weekStart rejected: %d", c)
	}
	if c, _ := call("POST", "/api/settings", `{"weekStart":"fri"}`); c != 400 {
		t.Errorf("an invalid weekStart was accepted: %d", c)
	}
}

// "Today" is the date Jira will write on an entry logged now, the same date the My work view groups by.
func TestWorklogDayFollowsHowJiraWritesStarted(t *testing.T) {
	now := time.Date(2026, 10, 17, 22, 30, 0, 0, time.UTC) // 01:00 on the 18th in Tehran (+03:30)
	cloud := jira.New("https://acme.atlassian.net", jira.ModeCloud, "me@x.com", "t")
	srv := jira.New("https://jira.example.com", jira.ModePAT, "", "t")
	s := &server{user: &jira.User{TimeZone: "Asia/Tehran"}}
	s.cfg.Timezone = "Asia/Tehran"

	// Server, exact times: Jira writes entries in the server's zone (UTC here): still the 17th
	s.serverKnown, s.serverOff = true, 0
	if got := s.worklogDay(srv, now); got != "2026-10-17" {
		t.Errorf("server zone: %s", got)
	}
	// Server, "match the server clock": the local wall clock is written, so the date is the local one: the 18th
	s.cfg.TimeMode = timeServer
	if got := s.worklogDay(srv, now); got != "2026-10-18" {
		t.Errorf("server-clock mode: %s", got)
	}
	// Cloud: Jira reports the user's profile zone: the 18th
	s.cfg.TimeMode = ""
	if got := s.worklogDay(cloud, now); got != "2026-10-18" {
		t.Errorf("cloud profile zone: %s", got)
	}
	// server zone unknown: fall back to the app's zone
	s.serverKnown = false
	if got := s.worklogDay(srv, now); got != "2026-10-18" {
		t.Errorf("unknown server zone: %s", got)
	}
}

func TestOrderByOnlyJQLGetsADateBound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	var jqls []string
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1"}`))
		case "/rest/api/2/search":
			mu.Lock()
			jqls = append(jqls, r.URL.Query().Get("jql"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(method, path, body string) int {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`)
	call("GET", "/api/issues?src=all&jql="+url.QueryEscape("order   by created DESC"), "")
	call("GET", "/api/issues?src=all&jql="+url.QueryEscape("project = KAN ORDER BY created DESC"), "")
	mu.Lock()
	defer mu.Unlock()
	if len(jqls) != 2 || !strings.HasPrefix(jqls[0], `created >= "2000-01-01" order   by created DESC`) || jqls[1] != "project = KAN ORDER BY created DESC" {
		t.Errorf("queries sent: %q", jqls)
	}
}

// Search words: each typed word is a prefix and all must match, in any script, hyphens and all.
func TestSearchBuildsOneConditionPerWord(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var mu sync.Mutex
	var jqls []string
	jsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"displayName":"T","name":"me","key":"U1"}`))
		case "/rest/api/2/search":
			mu.Lock()
			jqls = append(jqls, r.URL.Query().Get("jql"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer jsrv.Close()
	s := newServer()
	app := httptest.NewServer(s.handler())
	defer app.Close()
	call := func(method, path, body string) {
		req, _ := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	call("POST", "/api/login", `{"site":"`+jsrv.URL+`","mode":"basic","email":"u","token":"p"}`)
	for _, q := range []string{"jsw-probe checkout", "HD-72", "login", "привет мир", "!!!", `say "hi"`} {
		call("GET", "/api/issues?src=search&q="+url.QueryEscape(q), "")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		`text ~ "jsw*" AND text ~ "probe*" AND text ~ "checkout*" ORDER BY updated DESC`,
		`issuekey = "HD-72" OR (text ~ "HD*" AND text ~ "72*") ORDER BY updated DESC`,
		`text ~ "login*" ORDER BY updated DESC`,
		`text ~ "привет*" AND text ~ "мир*" ORDER BY updated DESC`,
		`text ~ "!!!*" ORDER BY updated DESC`,
		`text ~ "say*" AND text ~ "hi*" ORDER BY updated DESC`,
	}
	if len(jqls) != len(want) {
		t.Fatalf("queries: %q", jqls)
	}
	for i := range want {
		if jqls[i] != want[i] {
			t.Errorf("query %d:\n got  %s\n want %s", i, jqls[i], want[i])
		}
	}
}
