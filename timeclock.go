package main

import (
	"fmt"
	"time"
)

// Jira stores a worklog's start as an exact moment, and shows it in the viewer's profile time zone.
// But its reports and day-based filters (worklogDate, time-sheet gadgets) group by the *server's*
// zone. If the server is set to a different zone than its users, work logged between local midnight
// and the zone difference is counted on the previous day. "Server clock" mode is an opt-in
// workaround: the time the user sees is written as if it were on the server's clock.

const (
	timeExact  = ""       // standard: the exact moment
	timeServer = "server" // workaround: local clock reading on the server's clock
	ackFixed   = "fixed"
)

type clockState struct {
	Known         bool // the server's offset could be read
	ServerOffset  int  // seconds east of UTC
	LocalOffset   int  // the zone worklog times are entered in
	ProfileOffset int  // the Jira profile zone's offset
	HasProfile    bool
}

// mismatch: Jira knows the user lives in the local zone, yet the server clock is elsewhere.
func (c clockState) mismatch() bool {
	return c.Known && c.HasProfile && c.ProfileOffset == c.LocalOffset && c.ServerOffset != c.LocalOffset
}

func mismatchKey(c clockState) string { return fmt.Sprintf("%d/%d", c.ServerOffset, c.LocalOffset) }

// timeAdvice says whether to bring up a notice: "ask" (a mismatch the user hasn't answered),
// "fixed" (server-clock mode is on but the server now matches), or "".
func timeAdvice(mode, ack string, c clockState) string {
	switch {
	case mode == timeExact && c.mismatch() && ack != mismatchKey(c):
		return "ask"
	case mode == timeServer && c.Known && c.ServerOffset == c.LocalOffset && ack != ackFixed:
		return "fixed"
	}
	return ""
}

// onServerClock re-reads t's wall-clock fields as a reading of the server's clock.
func onServerClock(t time.Time, serverOffset int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.FixedZone("", serverOffset))
}

func zoneOffset(name string, at time.Time) (int, bool) {
	if name == "" {
		return 0, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return 0, false
	}
	_, off := at.In(loc).Zone()
	return off, true
}
