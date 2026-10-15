package jira

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FormatDuration renders d as "Xh Ym" (never days, so Jira's own
// working-day configuration can't change the meaning of what we send).
func FormatDuration(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 0 {
		m = 0
	}
	h, m := m/60, m%60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// CeilMinute rounds d up to the next whole minute.
func CeilMinute(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(math.Ceil(d.Minutes())) * time.Minute
}

var (
	partRe  = regexp.MustCompile(`(?i)(-?\d+(?:[.,]\d+)?)\s*([dhm])`)
	clockRe = regexp.MustCompile(`^(\d+):([0-5]\d)$`)
)

// ParseDuration parses Jira style durations ("1h 30m", "1.5h", "2d", "0")
// and "H:MM". hoursPerDay is Jira's working hours per day (default 8).
// Negative totals clamp to zero. ok is false for unparseable input.
func ParseDuration(s string, hoursPerDay float64) (d time.Duration, ok bool) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, true
	}
	if hoursPerDay <= 0 {
		hoursPerDay = 8
	}
	if m := clockRe.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		return time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute, true
	}
	matches := partRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	// Everything outside the matched parts must be whitespace.
	rest, last := "", 0
	var minutes float64
	for _, loc := range matches {
		rest += s[last:loc[0]]
		last = loc[1]
		n, err := strconv.ParseFloat(strings.Replace(s[loc[2]:loc[3]], ",", ".", 1), 64)
		if err != nil {
			return 0, false
		}
		switch strings.ToLower(s[loc[4]:loc[5]]) {
		case "m":
			minutes += n
		case "h":
			minutes += n * 60
		case "d":
			minutes += n * 60 * hoursPerDay
		}
	}
	rest += s[last:]
	if strings.TrimSpace(rest) != "" {
		return 0, false
	}
	if minutes < 0 {
		minutes = 0
	}
	return time.Duration(int64(minutes)) * time.Minute, true
}

// JiraTime formats t the way Jira expects worklog "started" values:
// 2006-01-02T15:04:05.000-0700
func JiraTime(t time.Time) string {
	return t.Format("2006-01-02T15:04:05.000-0700")
}
