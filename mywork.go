package main

import (
	"net/http"
	"strings"
	"time"

	"jirastopwatch-nextgen/internal/jira"
)

// My work: the user's own worklogs for a range of days, straight from Jira, plus adding, editing and
// removing them. Nothing here touches the timers or the Log work dialog's endpoint.

const maxWorkDays = 62 // a month or two is plenty for a week view and keeps the number of requests sane

// myWork returns the user's worklogs with a started date in [from, to].
func (s *server) myWork(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !jira.ValidDay(from) || !jira.ValidDay(to) || to < from {
		fail(w, 400, "err.badRequest")
		return
	}
	f, _ := time.Parse("2006-01-02", from)
	t, _ := time.Parse("2006-01-02", to)
	if t.Sub(f) > maxWorkDays*24*time.Hour {
		fail(w, 400, "err.badRequest")
		return
	}
	s.mu.RLock()
	me := s.user
	s.mu.RUnlock()
	entries, err := c.MyWorklogs(r.Context(), me, from, to)
	if err != nil {
		failErr(w, err)
		return
	}
	if entries == nil {
		entries = []jira.WorklogEntry{}
	}
	writeJSON(w, 200, map[string]any{"entries": entries, "fetchedAt": time.Now().Unix()})
}

// addWorklog books time on an issue without any timer (the "Log time manually" dialog). It follows the
// same rules as the Log work dialog: the start time is the user's local clock in the chosen zone (or the
// server's clock in that mode), comments follow the comment setting, a retry never books the same entry twice.
func (s *server) addWorklog(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	var req struct {
		Key        string `json:"key"`
		Time       string `json:"time"`
		Started    string `json:"started"`
		Comment    string `json:"comment"`
		Transition string `json:"transition"`
		Estimate   string `json:"estimate"`
		EstimateTo string `json:"estimateValue"`
	}
	if !decode(r, &req) {
		fail(w, 400, "err.badRequest")
		return
	}
	key := strings.ToUpper(strings.TrimSpace(req.Key))
	if !keyRe.MatchString(key) {
		fail(w, 400, "err.keyShape")
		return
	}
	d, ok := jira.ParseDuration(req.Time, s.hours())
	if !ok || d <= 0 {
		fail(w, 400, "err.timeZero")
		return
	}
	s.mu.RLock()
	loc, mode, me := s.cfg.Location(), s.cfg.CommentMode, s.user
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
	if already, _ := c.HasWorklog(r.Context(), me, key, started, d); already {
		resp["warning"] = "warn.alreadyLogged|" + key
	} else if err := c.PostWorklog(r.Context(), key, started, d, wlComment, est); err != nil {
		failErr(w, err)
		return
	} else if sepComment != "" {
		if err := c.PostComment(r.Context(), key, sepComment); err != nil {
			resp["warning"] = "warn.comment|" + err.Error()
		}
	}
	s.todayGen.Add(1)
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

// worklogID reads the issue key and the numeric worklog id from the path.
func worklogID(w http.ResponseWriter, r *http.Request) (key, id string, ok bool) {
	key, ok = reqKey(w, r)
	if !ok {
		return "", "", false
	}
	id = r.PathValue("id")
	if !digitsRe.MatchString(id) {
		fail(w, 400, "err.badRequest")
		return "", "", false
	}
	return key, id, true
}

// updateWorklog edits a worklog. The start is the wall clock Jira showed (day and time) with the zone
// Jira reported, so editing never moves an entry to another day by itself.
func (s *server) updateWorklog(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	key, id, ok := worklogID(w, r)
	if !ok {
		return
	}
	var req struct {
		Time    string `json:"time"`
		Started string `json:"started"` // 2006-01-02T15:04
		Offset  string `json:"offset"`  // +0000, from the entry
		Comment string `json:"comment"`
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
	zone := time.UTC
	if req.Offset != "" {
		t, err := time.Parse("-0700", req.Offset)
		if err != nil {
			fail(w, 400, "err.badStart")
			return
		}
		_, off := t.Zone()
		zone = time.FixedZone("", off)
	}
	started, err := time.ParseInLocation(localLayout, req.Started, zone)
	if err != nil {
		fail(w, 400, "err.badStart")
		return
	}
	if err := c.UpdateWorklog(r.Context(), key, id, started, d, strings.TrimSpace(req.Comment), jira.Estimate{Method: "auto"}); err != nil {
		failErr(w, err)
		return
	}
	s.todayGen.Add(1)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// deleteWorklog removes a worklog from Jira (Jira decides whether the user may).
func (s *server) deleteWorklog(w http.ResponseWriter, r *http.Request) {
	c := s.jc(w)
	if c == nil {
		return
	}
	key, id, ok := worklogID(w, r)
	if !ok {
		return
	}
	if err := c.DeleteWorklog(r.Context(), key, id); err != nil {
		failErr(w, err)
		return
	}
	s.todayGen.Add(1)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
