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
	"sync/atomic"
	"time"

	"jirastopwatch-nextgen/internal/jira"
	"jirastopwatch-nextgen/internal/store"
	"jirastopwatch-nextgen/internal/timer"
)

//go:embed web
var webFS embed.FS

const localLayout = "2006-01-02T15:04"

const maxJQL = 2000 // longest advanced search we accept

var keyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

type server struct {
	token string // per-launch secret; guards the local API against other web pages

	mu          sync.RWMutex
	client      *jira.Client
	user        *jira.User
	hoursPerDay float64
	serverOff   int  // Jira server clock's UTC offset, seconds
	serverKnown bool // ...if it could be read
	cfg         store.Config

	timers *timer.Manager

	todayMu   sync.Mutex
	todayGen  atomic.Int64 // bumped whenever the day's totals may have changed (never blocks)
	todayFor  int64        // the generation todayData belongs to
	todayAt   time.Time    // when todayData was fetched
	todayDay  string       // the date it is for
	todayData map[string]any

	lockPaused []string // timers paused by a session lock, for "resume on unlock"
	update     updateInfo

	updating   bool   // an update is being downloaded
	restartExe string // set once an update is installed: main starts this after shutting down
	quit       func() // asks main to shut down (set by main)

	recovered *crashInfo // set at start-up after an unclean end; shown to the user once

	pingMu   sync.Mutex
	lastPing time.Time
}

type crashInfo struct {
	Keys []string `json:"keys"`
	At   string   `json:"at"` // when the app was last running, local time
}

// recoverFromCrash runs when the previous copy never exited cleanly. Timers that were running
// are paused at the moment the app was last seen alive, rather than counting the time the computer
// was off (the page tells the user, who can correct the time).
func (s *server) recoverFromCrash(alive time.Time) {
	if alive.IsZero() || time.Since(alive) < 90*time.Second {
		return // too recent to be a long outage: normal restart, keep counting
	}
	if keys := s.timers.PauseRunningAt(alive); len(keys) > 0 {
		s.recovered = &crashInfo{keys, alive.In(s.cfg.Location()).Format("2006-01-02 15:04")}
	}
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
	mux.HandleFunc("GET /zones.json", serveZones)
	mux.HandleFunc("GET /icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(appIcon)
	})
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		_, _ = w.Write([]byte(`{"name":"Jira StopWatch NextGen","short_name":"StopWatch","start_url":"/","display":"standalone",` +
			`"icons":[{"src":"/icon.png","sizes":"512x512","type":"image/png","purpose":"any"}]}`))
	})

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
	mux.HandleFunc("GET /api/started", s.startedFor)
	mux.HandleFunc("GET /api/today", s.today)
	mux.HandleFunc("POST /api/update/check", s.updateCheck)
	mux.HandleFunc("POST /api/update/apply", s.updateApply)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.recovered
	s.recovered = nil // told once
	writeJSON(w, 200, map[string]any{
		"recovered":   rec,
		"loggedIn":    s.client != nil,
		"user":        s.user,
		"site":        s.cfg.Site,
		"email":       s.cfg.Email,
		"hasSaved":    s.cfg.Token != "",
		"version":     version,
		"time":        s.timeInfo(),
		"pauseOthers": s.cfg.PauseOthers,
		"prefs":       s.prefs(),
	})
}

// timeInfo describes the Jira-server-vs-local clock situation for the page. Callers hold s.mu.
func (s *server) timeInfo() map[string]any {
	now := time.Now()
	_, local := now.In(s.cfg.Location()).Zone()
	st := clockState{Known: s.serverKnown, ServerOffset: s.serverOff, LocalOffset: local}
	if s.user != nil {
		st.ProfileOffset, st.HasProfile = zoneOffset(s.user.TimeZone, now)
	}
	return map[string]any{
		"mode": s.cfg.TimeMode, "advice": timeAdvice(s.cfg.TimeMode, s.cfg.TimeAck, st),
		"known": st.Known, "serverOffsetSec": st.ServerOffset, "localOffsetSec": local,
		"mismatch": st.mismatch(), "key": mismatchKey(st),
	}
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
		"startOnTop":       s.cfg.StartOnTop,
		"shadows":          s.cfg.Shadows,
		"fontSize":         s.cfg.FontSize,
		"asideClosed":      s.cfg.AsideClosed,
		"lastJql":          s.cfg.LastJQL,
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
		req.Remember = true // signing in with what was remembered keeps it remembered
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
	srvOff, srvKnown := c.ServerOffset(ctx)

	s.mu.Lock()
	s.client, s.user, s.hoursPerDay = c, user, hpd
	s.todayGen.Add(1)
	s.serverOff, s.serverKnown = srvOff, srvKnown
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
		TimeMode         *string `json:"timeMode"`
		TimeAck          *string `json:"timeAck"`
		StartOnTop       *bool   `json:"startOnTop"`
		Shadows          *bool   `json:"shadows"`
		FontSize         *int    `json:"fontSize"`
		AsideClosed      *bool   `json:"asideClosed"`
		LastJQL          *string `json:"lastJql"`
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
	if req.LastJQL != nil && len(*req.LastJQL) > maxJQL {
		fail(w, 400, "err.badRequest")
		return
	}
	if req.FontSize != nil && *req.FontSize != 0 && (*req.FontSize < 10 || *req.FontSize > 16) {
		fail(w, 400, "err.badRequest")
		return
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
	if req.TimeMode != nil && *req.TimeMode != timeExact && *req.TimeMode != timeServer {
		fail(w, 400, "err.badRequest")
		return
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
	if req.StartOnTop != nil {
		s.cfg.StartOnTop = *req.StartOnTop
	}
	if req.Shadows != nil {
		s.cfg.Shadows = *req.Shadows
	}
	if req.FontSize != nil {
		s.cfg.FontSize = *req.FontSize
	}
	if req.AsideClosed != nil {
		s.cfg.AsideClosed = *req.AsideClosed
	}
	if req.LastJQL != nil {
		s.cfg.LastJQL = strings.TrimSpace(*req.LastJQL)
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
	if req.TimeAck != nil {
		s.cfg.TimeAck = *req.TimeAck
	}
	if req.TimeMode != nil && *req.TimeMode != s.cfg.TimeMode {
		s.cfg.TimeMode = *req.TimeMode
		// Answering with an explicit choice settles the current notice; a later change of situation asks again.
		s.cfg.TimeAck = ""
		if s.cfg.TimeMode == timeExact && s.client != nil {
			_, local := time.Now().In(s.cfg.Location()).Zone()
			s.cfg.TimeAck = mismatchKey(clockState{ServerOffset: s.serverOff, LocalOffset: local})
		}
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
			int64(t.Elapsed(now).Seconds()), t.Running(), t.Comment, defaultStart(now, t.Elapsed(now)).In(loc).Format(localLayout)}
	}
	_, off := now.In(loc).Zone()
	writeJSON(w, 200, map[string]any{"timers": out, "pauseOthers": po, "saveError": s.timers.SaveError(),
		"zone": map[string]any{"name": loc.String(), "offsetSec": off, "nowUnix": now.Unix()}})
}

// defaultStart is when a worklog of the given length would have started if it ended now
// (the log dialog rounds the length up to whole minutes, so this does too).
func defaultStart(now time.Time, d time.Duration) time.Time {
	mins := (d + time.Minute - 1) / time.Minute
	if mins < 1 {
		mins = 1
	}
	return now.Add(-mins * time.Minute)
}

// startedFor answers "if I log this much time right now, when did it start?" in the chosen zone.
func (s *server) startedFor(w http.ResponseWriter, r *http.Request) {
	d, ok := jira.ParseDuration(r.URL.Query().Get("time"), s.hours())
	if !ok || d <= 0 {
		fail(w, 400, "err.timeFormat")
		return
	}
	s.mu.RLock()
	loc := s.cfg.Location()
	s.mu.RUnlock()
	writeJSON(w, 200, map[string]string{"started": defaultStart(time.Now(), d).In(loc).Format(localLayout)})
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
	po, c, names, top := s.cfg.PauseOthers, s.client, s.cfg.StartTransitions, s.cfg.StartOnTop
	s.mu.RUnlock()
	if err := s.timers.Start(key, po); err != nil {
		fail(w, 404, err.Error())
		return
	}
	if top {
		_ = s.timers.MoveAfter(key, "") // "Move to the top when started" (Settings)
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
	srvClock, srvOff := s.cfg.TimeMode == timeServer && s.serverKnown, s.serverOff
	s.mu.RUnlock()
	started, err := time.ParseInLocation(localLayout, req.Started, loc)
	if err != nil {
		fail(w, 400, "err.badStart")
		return
	}
	if srvClock {
		started = onServerClock(started, srvOff)
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
	wlComment, sepComment := comment, ""
	if comment != "" && (mode == "comment" || mode == "both") {
		sepComment = comment
		if mode == "comment" {
			wlComment = ""
		}
	}
	resp := map[string]any{"ok": true}
	// If an earlier attempt was cut off by the network after Jira had already saved it, retrying must
	// not book the same time twice: an identical entry of yours (same start minute and length) counts as done.
	s.mu.RLock()
	me := s.user
	s.mu.RUnlock()
	already, _ := c.HasWorklog(r.Context(), me, key, started, d)
	if already {
		resp["warning"] = "warn.alreadyLogged|" + key
	} else if err := c.PostWorklog(r.Context(), key, started, d, wlComment, est); err != nil {
		failErr(w, err)
		return
	}
	// The worklog is saved: reset right away (before anything that can fail or stall) so the same
	// time can never be logged again, and the day's totals are stale.
	s.timers.Reset(key)
	s.todayGen.Add(1)
	// The separate comment goes after the worklog, so a failure here can't lose time or duplicate a comment on retry.
	if sepComment != "" && !already {
		if err := c.PostComment(r.Context(), key, sepComment); err != nil {
			resp["warning"] = "warn.comment|" + err.Error()
			_ = s.timers.SetComment(key, sepComment) // keep the text as the timer's draft so it is not lost
		}
	}
	if req.Transition != "" {
		if err := c.DoTransition(r.Context(), key, req.Transition); err != nil {
			if resp["warning"] == nil {
				resp["warning"] = "warn.transition|" + err.Error()
			} else {
				resp["warning2"] = "warn.transition|" + err.Error()
			}
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
	case "all":
		// Every issue the user may see, any team: optionally narrowed by their own JQL, which Jira runs as
		// them (permissions apply, syntax errors come back in Jira's words). Cloud refuses a query that
		// has only ORDER BY, so the default carries a harmless date bound.
		jql = strings.TrimSpace(q.Get("jql"))
		if len(jql) > maxJQL {
			fail(w, 400, "err.badRequest")
			return
		}
		if jql == "" {
			jql = `created >= "2000-01-01" ORDER BY updated DESC`
		}
	case "search":
		text := strings.TrimSpace(q.Get("q"))
		if text == "" {
			writeJSON(w, 200, map[string]any{"issues": []jira.Issue{}, "next": ""})
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
	res, next, err := c.SearchPage(r.Context(), jql, 50, q.Get("cursor"))
	if err != nil {
		failErr(w, err)
		return
	}
	if res == nil {
		res = []jira.Issue{}
	}
	writeJSON(w, 200, map[string]any{"issues": res, "next": next})
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
		Dir   int     `json:"dir"`
		After *string `json:"after"` // drag and drop: place after this timer ("" = first)
	}
	if !decode(r, &req) || (req.After == nil && req.Dir != -1 && req.Dir != 1) {
		fail(w, 400, "err.badRequest")
		return
	}
	var err error
	if req.After != nil {
		err = s.timers.MoveAfter(key, *req.After)
	} else {
		err = s.timers.Move(key, req.Dir)
	}
	if err != nil {
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
	mode, restarting := s.cfg.ExitMode, s.restartExe != ""
	s.mu.RUnlock()
	if restarting { // an update restart is not "closing the app": timers keep running
		return
	}
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

var (
	zonesOnce sync.Once
	zonesJSON []byte
)

// serveZones serves the country/zone list, minus any zone this machine's Go
// runtime can't load. The list is generated from a recent tz database; the
// embedded copy (used on Windows) or an older system copy may lack the newest
// zone names, and offering one would make "Save" fail.
func serveZones(w http.ResponseWriter, r *http.Request) {
	zonesOnce.Do(func() {
		raw, _ := webFS.ReadFile("web/zones.json")
		zonesJSON = loadableZones(raw)
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(zonesJSON)
}

func loadableZones(raw []byte) []byte {
	type country struct {
		C string   `json:"c"`
		N string   `json:"n"`
		Z []string `json:"z"`
	}
	var in, out []country
	if json.Unmarshal(raw, &in) != nil {
		return raw
	}
	for _, c := range in {
		var ok []string
		for _, z := range c.Z {
			if _, err := time.LoadLocation(z); err == nil {
				ok = append(ok, z)
			}
		}
		if len(ok) > 0 {
			c.Z = ok
			out = append(out, c)
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// today answers how much the user has logged on Jira today (in the chosen zone), in total and per issue.
func (s *server) today(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	s.mu.RLock()
	loc, me := s.cfg.Location(), s.user
	srvClock, srvOff := s.cfg.TimeMode == timeServer && s.serverKnown, s.serverOff
	s.mu.RUnlock()
	now := time.Now().In(loc)
	day := now.Format("2006-01-02")
	// In "match the server clock" mode worklogs carry the local wall time on the server's clock,
	// so "today" is that same calendar day measured in the server's zone.
	dayLoc := loc
	if srvClock {
		dayLoc = time.FixedZone("jira-server", srvOff)
	}
	s.todayMu.Lock()
	defer s.todayMu.Unlock()
	gen := s.todayGen.Load()
	if s.todayDay == day && s.todayFor == gen && time.Since(s.todayAt) < 20*time.Second && s.todayData != nil {
		writeJSON(w, 200, s.todayData)
		return
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, dayLoc)
	logs, err := c.WorklogsSince(r.Context(), me, start)
	if err != nil {
		failErr(w, err)
		return
	}
	end := start.AddDate(0, 0, 1)
	var total int64
	by := map[string]int64{}
	for _, l := range logs {
		if l.Started.Before(end) {
			total += l.Seconds
			by[l.Key] += l.Seconds
		}
	}
	s.todayData = map[string]any{"total": total, "byKey": by, "date": day}
	s.todayDay, s.todayAt, s.todayFor = day, time.Now(), gen
	writeJSON(w, 200, s.todayData)
}
