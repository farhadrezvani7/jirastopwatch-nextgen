package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"jirastopwatch-nextgen/internal/jira"
	"jirastopwatch-nextgen/internal/store"
	"jirastopwatch-nextgen/internal/timer"
)

//go:embed web
var webFS embed.FS

const localLayout = "2006-01-02T15:04"

var keyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

type server struct {
	token string // per-launch secret; guards the local API against other web pages

	mu          sync.RWMutex
	client      *jira.Client
	user        *jira.User
	hoursPerDay float64
	cfg         store.Config

	timers *timer.Manager

	lockPaused []string // timers paused by a session lock, for "resume on unlock"
	update     updateInfo

	pingMu   sync.Mutex
	lastPing time.Time
}

func newServer() *server {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return &server{
		token:  hex.EncodeToString(b),
		cfg:    store.Load(),
		timers: timer.Load(filepath.Join(store.Dir(), "timers.json")),
	}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	mux.HandleFunc("GET /api/ping", s.ping)
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/settings", s.settings)

	mux.HandleFunc("POST /api/timers", s.addTimer)
	mux.HandleFunc("POST /api/timers/pause-all", s.pauseAll)
	mux.HandleFunc("POST /api/timers/{key}/start", s.startTimer)
	mux.HandleFunc("POST /api/timers/{key}/pause", s.pauseTimer)
	mux.HandleFunc("POST /api/timers/{key}/set", s.setTimer)
	mux.HandleFunc("DELETE /api/timers/{key}", s.removeTimer)
	mux.HandleFunc("POST /api/timers/{key}/log", s.logWork)
	mux.HandleFunc("POST /api/timers/{key}/reset", s.resetTimer)
	mux.HandleFunc("POST /api/timers/{key}/comment", s.setComment)
	mux.HandleFunc("GET /api/update", s.updateStatus)
	mux.HandleFunc("POST /api/timers/{key}/move", s.moveTimer)
	mux.HandleFunc("GET /api/issues/{key}/estimate", s.estimate)

	mux.HandleFunc("GET /api/issues", s.issues)
	mux.HandleFunc("GET /api/filters", s.filters)
	mux.HandleFunc("GET /api/issues/{key}/transitions", s.transitions)

	return s.guard(mux)
}

// guard enforces a loopback Host header (anti DNS-rebinding) and, for /api,
// the per-launch token cookie. Visiting /?t=<token> sets the cookie.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if t := r.URL.Query().Get("t"); t != "" && s.tokenOK(t) {
			http.SetCookie(w, &http.Cookie{Name: "sw", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			c, err := r.Cookie("sw")
			if err != nil || !s.tokenOK(c.Value) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *server) tokenOK(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func failErr(w http.ResponseWriter, err error) {
	var ae *jira.APIError
	if errors.As(err, &ae) {
		code := http.StatusBadGateway
		if ae.Status == 401 {
			code = http.StatusUnauthorized
		}
		fail(w, code, ae.Message)
		return
	}
	fail(w, http.StatusBadGateway, err.Error())
}

func decode(r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v) == nil
}

func (s *server) jc(w http.ResponseWriter) *jira.Client {
	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()
	if c == nil {
		fail(w, http.StatusUnauthorized, "err.notSignedIn")
	}
	return c
}

func (s *server) hours() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hoursPerDay <= 0 {
		return 8
	}
	return s.hoursPerDay
}

func reqKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	k := strings.ToUpper(strings.TrimSpace(r.PathValue("key")))
	if !keyRe.MatchString(k) {
		fail(w, 400, "err.badKey")
		return "", false
	}
	return k, true
}

// ---- session ----

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, 200, map[string]any{
		"loggedIn":    s.client != nil,
		"user":        s.user,
		"site":        s.cfg.Site,
		"email":       s.cfg.Email,
		"hasSaved":    s.cfg.Token != "",
		"pauseOthers": s.cfg.PauseOthers,
		"prefs":       s.prefs(),
	})
}

// prefs is the user-facing slice of the config (never the secret).
func (s *server) prefs() map[string]any {
	return map[string]any{
		"theme":            s.cfg.Theme,
		"lockMode":         s.cfg.LockMode,
		"exitMode":         s.cfg.ExitMode,
		"updateCheck":      !s.cfg.NoUpdateCheck,
		"updateEnabled":    updateRepo != "",
		"lang":             s.cfg.Lang,
		"country":          s.cfg.Country,
		"timezone":         s.cfg.Timezone,
		"commentMode":      s.cfg.CommentMode,
		"startTransitions": s.cfg.StartTransitions,
	}
}

type loginReq struct {
	Mode     string `json:"mode"`
	Site     string `json:"site"`
	Email    string `json:"email"`
	Token    string `json:"token"`
	Remember bool   `json:"remember"`
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	s.mu.RLock()
	saved := s.cfg
	s.mu.RUnlock()
	// Empty token + saved credentials = "continue as saved user".
	if req.Token == "" && saved.Token != "" {
		req.Mode, req.Site, req.Email, req.Token = saved.Mode, saved.Site, saved.Email, saved.Token
	}
	site, err := jira.NormalizeSite(req.Site)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	req.Email, req.Token = strings.TrimSpace(req.Email), strings.TrimSpace(req.Token)
	switch req.Mode {
	case jira.ModeCloud, jira.ModePAT, jira.ModeBasic:
	default:
		req.Mode = jira.DefaultMode(site)
	}
	if req.Token == "" || (req.Mode != jira.ModePAT && req.Email == "") {
		fail(w, 400, "err.fillAll")
		return
	}
	if req.Mode == jira.ModePAT {
		req.Email = ""
	}
	c := jira.New(site, req.Mode, req.Email, req.Token)
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	user, err := c.Myself(ctx)
	if err != nil {
		failErr(w, err)
		return
	}
	hpd := c.HoursPerDay(ctx)

	s.mu.Lock()
	s.client, s.user, s.hoursPerDay = c, user, hpd
	s.cfg.Site, s.cfg.Mode, s.cfg.Email, s.cfg.Token = site, req.Mode, req.Email, ""
	if req.Remember {
		s.cfg.Token = req.Token
	}
	cfg := s.cfg
	s.mu.Unlock()
	if err := store.Save(cfg); err != nil {
		fail(w, 500, "err.saveSettings|"+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"user": user})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.client, s.user = nil, nil
	s.cfg.Token = ""
	cfg := s.cfg
	s.mu.Unlock()
	_ = store.Save(cfg)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// settings applies a partial update: fields absent from the request are kept.
func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PauseOthers      *bool   `json:"pauseOthers"`
		Theme            *string `json:"theme"`
		LockMode         *string `json:"lockMode"`
		ExitMode         *string `json:"exitMode"`
		UpdateCheck      *bool   `json:"updateCheck"`
		Lang             *string `json:"lang"`
		Country          *string `json:"country"`
		Timezone         *string `json:"timezone"`
		CommentMode      *string `json:"commentMode"`
		StartTransitions *string `json:"startTransitions"`
	}
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	if req.Timezone != nil && *req.Timezone != "" {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			fail(w, 400, "err.badZone")
			return
		}
	}
	if req.Theme != nil && *req.Theme != "" && *req.Theme != "light" && *req.Theme != "dark" {
		fail(w, 400, "err.badRequest")
		return
	}
	for _, v := range []*string{req.LockMode, req.ExitMode} {
		if v != nil {
			switch *v {
			case "", "pause", "resume", "reset":
			default:
				fail(w, 400, "err.badRequest")
				return
			}
		}
	}
	if req.CommentMode != nil {
		switch *req.CommentMode {
		case "", "worklog", "comment", "both":
		default:
			fail(w, 400, "err.badRequest")
			return
		}
	}
	s.mu.Lock()
	if req.PauseOthers != nil {
		s.cfg.PauseOthers = *req.PauseOthers
	}
	if req.Theme != nil {
		s.cfg.Theme = *req.Theme
	}
	if req.LockMode != nil {
		s.cfg.LockMode = *req.LockMode
	}
	if req.ExitMode != nil {
		s.cfg.ExitMode = *req.ExitMode
	}
	if req.UpdateCheck != nil {
		s.cfg.NoUpdateCheck = !*req.UpdateCheck
	}
	if req.Lang != nil {
		s.cfg.Lang = *req.Lang
	}
	if req.Country != nil {
		s.cfg.Country = *req.Country
	}
	if req.Timezone != nil {
		s.cfg.Timezone = *req.Timezone
	}
	if req.CommentMode != nil {
		s.cfg.CommentMode = *req.CommentMode
	}
	if req.StartTransitions != nil {
		s.cfg.StartTransitions = strings.TrimSpace(*req.StartTransitions)
	}
	cfg := s.cfg
	s.mu.Unlock()
	_ = store.Save(cfg)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- timers ----

type timerDTO struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	Project    string `json:"project"`
	Status     string `json:"status"`
	ElapsedSec int64  `json:"elapsedSec"`
	Running    bool   `json:"running"`
	Comment    string `json:"comment"`
	StartedAt  string `json:"startedAt"` // local time in the chosen zone, for <input type=datetime-local>
}

func (s *server) state(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.mu.RLock()
	po, loc := s.cfg.PauseOthers, s.cfg.Location()
	s.mu.RUnlock()
	ts := s.timers.Snapshot()
	out := make([]timerDTO, len(ts))
	for i, t := range ts {
		out[i] = timerDTO{t.Key, t.Summary, t.Project, t.Status,
			int64(t.Elapsed(now).Seconds()), t.Running(), t.Comment, t.StartTime(now).In(loc).Format(localLayout)}
	}
	_, off := now.In(loc).Zone()
	writeJSON(w, 200, map[string]any{"timers": out, "pauseOthers": po,
		"zone": map[string]any{"name": loc.String(), "offsetSec": off, "nowUnix": now.Unix()}})
}

func (s *server) addTimer(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	req.Key = ""
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	key := strings.ToUpper(strings.TrimSpace(jira.ParseKey(strings.TrimSpace(req.Key))))
	if !keyRe.MatchString(key) {
		fail(w, 400, "err.keyShape")
		return
	}
	iss, err := c.GetIssue(r.Context(), key)
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.timers.Add(iss.Key, iss.Summary, iss.Project, iss.Status); err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, iss)
}

func (s *server) startTimer(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	s.mu.RLock()
	po, c, names := s.cfg.PauseOthers, s.client, s.cfg.StartTransitions
	s.mu.RUnlock()
	if err := s.timers.Start(key, po); err != nil {
		fail(w, 404, err.Error())
		return
	}
	if c != nil && strings.TrimSpace(names) != "" {
		go applyStartTransition(c, key, names)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) pauseTimer(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	if err := s.timers.Pause(key); err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) pauseAll(w http.ResponseWriter, r *http.Request) {
	s.timers.PauseAll()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) removeTimer(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	s.timers.Remove(key)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) setTimer(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Time string `json:"time"`
	}
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	d, ok := jira.ParseDuration(req.Time, s.hours())
	if !ok {
		fail(w, 400, "err.timeFormat")
		return
	}
	if err := s.timers.SetElapsed(key, d); err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) logWork(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Time       string `json:"time"`
		Started    string `json:"started"` // local time in the chosen zone (2006-01-02T15:04)
		Comment    string `json:"comment"`
		Transition string `json:"transition"`
		Estimate   string `json:"estimate"`      // auto | leave | new | manual
		EstimateTo string `json:"estimateValue"` // for new / manual
	}
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	d, ok := jira.ParseDuration(req.Time, s.hours())
	if !ok || d <= 0 {
		fail(w, 400, "err.timeZero")
		return
	}
	s.mu.RLock()
	loc, mode := s.cfg.Location(), s.cfg.CommentMode
	s.mu.RUnlock()
	started, err := time.ParseInLocation(localLayout, req.Started, loc)
	if err != nil {
		fail(w, 400, "err.badStart")
		return
	}
	est := jira.Estimate{Method: req.Estimate}
	switch req.Estimate {
	case "new", "manual":
		if _, ok := jira.ParseDuration(req.EstimateTo, s.hours()); !ok || strings.TrimSpace(req.EstimateTo) == "" {
			fail(w, 400, "err.timeFormat")
			return
		}
		est.Value = strings.TrimSpace(req.EstimateTo)
	case "", "auto", "leave":
	default:
		fail(w, 400, "err.badRequest")
		return
	}
	comment := strings.TrimSpace(req.Comment)
	wlComment := comment
	if comment != "" && (mode == "comment" || mode == "both") {
		if err := c.PostComment(r.Context(), key, comment); err != nil {
			failErr(w, err)
			return
		}
		if mode == "comment" {
			wlComment = ""
		}
	}
	if err := c.PostWorklog(r.Context(), key, started, d, wlComment, est); err != nil {
		failErr(w, err)
		return
	}
	// The worklog is saved; reset now so a failed transition can't cause double logging.
	s.timers.Reset(key)
	resp := map[string]any{"ok": true}
	if req.Transition != "" {
		if err := c.DoTransition(r.Context(), key, req.Transition); err != nil {
			resp["warning"] = "warn.transition|" + err.Error()
		}
	}
	writeJSON(w, 200, resp)
}

// ---- issue browsing ----

func (s *server) issues(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	q := r.URL.Query()
	var jql string
	switch q.Get("src") {
	case "recent":
		jql = "issuekey in issueHistory() ORDER BY lastViewed DESC"
	case "assigned":
		jql = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"
	case "filter":
		id := q.Get("filter")
		if !regexp.MustCompile(`^\d+$`).MatchString(id) {
			fail(w, 400, "err.badFilter")
			return
		}
		jql = "filter = " + id
	case "search":
		text := strings.TrimSpace(q.Get("q"))
		if text == "" {
			writeJSON(w, 200, []jira.Issue{})
			return
		}
		esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text)
		jql = `text ~ "` + esc + `*"`
		if k := strings.ToUpper(text); keyRe.MatchString(k) {
			jql = `issuekey = "` + k + `" OR ` + jql
		}
		jql += " ORDER BY updated DESC"
	default:
		fail(w, 400, "err.badSource")
		return
	}
	res, err := c.Search(r.Context(), jql, 50)
	if err != nil {
		failErr(w, err)
		return
	}
	if res == nil {
		res = []jira.Issue{}
	}
	writeJSON(w, 200, res)
}

func (s *server) filters(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	f, err := c.Filters(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	if f == nil {
		f = []jira.Filter{}
	}
	writeJSON(w, 200, f)
}

func (s *server) transitions(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	t, err := c.Transitions(r.Context(), key)
	if err != nil {
		failErr(w, err)
		return
	}
	if t == nil {
		t = []jira.Transition{}
	}
	writeJSON(w, 200, t)
}

// applyStartTransition moves the issue to the first available status whose
// name contains one of the configured names (case-insensitive), like the
// original StopWatch "start transitions" setting. Best effort.
func applyStartTransition(c *jira.Client, key, names string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	want := []string{}
	for _, l := range strings.FieldsFunc(names, func(r rune) bool { return r == '\n' || r == ',' }) {
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			want = append(want, l)
		}
	}
	ts, err := c.Transitions(ctx, key)
	if err != nil {
		return
	}
	for _, t := range ts {
		n := strings.ToLower(t.Name)
		for _, w := range want {
			if strings.Contains(n, w) {
				_ = c.DoTransition(ctx, key, t.ID)
				return
			}
		}
	}
}

// ping is the page heartbeat; see waitForPageGone.
func (s *server) ping(w http.ResponseWriter, r *http.Request) {
	s.pingMu.Lock()
	s.lastPing = time.Now()
	s.pingMu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// waitForPageGone blocks until the page has pinged at least once and then
// stayed silent for grace (or never pinged for grace after startup).
func (s *server) waitForPageGone(grace time.Duration) {
	start := time.Now()
	for {
		time.Sleep(5 * time.Second)
		s.pingMu.Lock()
		last := s.lastPing
		s.pingMu.Unlock()
		if last.IsZero() {
			last = start
		}
		if time.Since(last) > grace {
			return
		}
	}
}

func (s *server) resetTimer(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	s.timers.Reset(key)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) moveTimer(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Dir int `json:"dir"`
	}
	if !decode(r, &req) || (req.Dir != -1 && req.Dir != 1) {
		fail(w, 400, "err.badRequest")
		return
	}
	if err := s.timers.Move(key, req.Dir); err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) estimate(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	e, err := c.Remaining(r.Context(), key)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (s *server) setComment(w http.ResponseWriter, r *http.Request) {
	key, ok := reqKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	if len(req.Comment) > 32000 {
		req.Comment = req.Comment[:32000]
	}
	if err := s.timers.SetComment(key, req.Comment); err != nil {
		fail(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// onLock pauses running timers when the session locks and, in "resume" mode,
// restarts them on unlock.
func (s *server) onLock(locked bool) {
	s.mu.RLock()
	mode := s.cfg.LockMode
	s.mu.RUnlock()
	if mode != "pause" && mode != "resume" {
		return
	}
	if locked {
		keys := s.timers.PauseRunning()
		s.mu.Lock()
		s.lockPaused = keys
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	keys := s.lockPaused
	s.lockPaused = nil
	s.mu.Unlock()
	if mode == "resume" {
		s.timers.Resume(keys)
	}
}

// onExit applies the "when the app closes" setting.
func (s *server) onExit() {
	s.mu.RLock()
	mode := s.cfg.ExitMode
	s.mu.RUnlock()
	switch mode {
	case "pause":
		s.timers.PauseAll()
	case "reset":
		s.timers.ResetAll()
	}
}

func (s *server) noUpdateCheck() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.NoUpdateCheck
}
