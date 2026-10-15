package jira

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"1h 30m", 90 * time.Minute, true},
		{"1h30m", 90 * time.Minute, true},
		{"1.5h", 90 * time.Minute, true},
		{"1,5h", 90 * time.Minute, true},
		{"2d", 16 * time.Hour, true},
		{"45m", 45 * time.Minute, true},
		{"1:05", 65 * time.Minute, true},
		{"0", 0, true},
		{"-5m", 0, true},
		{"", 0, false},
		{"abc", 0, false},
		{"1h foo", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseDuration(c.in, 8)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseDuration(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	if got := FormatDuration(150 * time.Minute); got != "2h 30m" {
		t.Error(got)
	}
	if got := FormatDuration(5 * time.Minute); got != "5m" {
		t.Error(got)
	}
	if got := CeilMinute(61 * time.Second); got != 2*time.Minute {
		t.Error(got)
	}
}

func TestNormalizeSite(t *testing.T) {
	cases := map[string]string{
		"acme.atlassian.net":                             "https://acme.atlassian.net",
		"https://acme.atlassian.net/jira/for-you":        "https://acme.atlassian.net",
		"jira.example.com":                               "https://jira.example.com",
		"https://jira.example.com/browse/ABC-1":          "https://jira.example.com",
		"https://example.com/jira/secure/Dashboard.jspa": "https://example.com/jira",
		"http://127.0.0.1:8080/":                         "http://127.0.0.1:8080",
	}
	for in, want := range cases {
		if got, err := NormalizeSite(in); err != nil || got != want {
			t.Errorf("NormalizeSite(%q) = %q,%v want %q", in, got, err, want)
		}
	}
}

func TestParseDurationEdgeCases(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"1h30m", 90 * time.Minute, true},
		{"1,5h", 90 * time.Minute, true},
		{"2d", 16 * time.Hour, true},
		{"1d 2h", 10 * time.Hour, true},
		{"0", 0, true},
		{"-5m", 0, true},
		{"1:05", 65 * time.Minute, true},
		{"abc", 0, false},
		{"1h xyz", 0, false},
		{"", 0, false},
		{"1:75", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseDuration(c.in, 8)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseDuration(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
	if got, _ := ParseDuration("1d", 6); got != 6*time.Hour {
		t.Errorf("custom hoursPerDay: %v", got)
	}
}

func TestJiraTimeKeepsChosenOffset(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	if got := JiraTime(time.Date(2026, 1, 10, 9, 0, 0, 0, ny)); got != "2026-01-10T09:00:00.000-0500" {
		t.Errorf("winter: %s", got)
	}
	if got := JiraTime(time.Date(2026, 7, 10, 9, 0, 0, 0, ny)); got != "2026-07-10T09:00:00.000-0400" {
		t.Errorf("summer DST: %s", got)
	}
	ir, _ := time.LoadLocation("Asia/Tehran")
	if got := JiraTime(time.Date(2026, 7, 10, 9, 0, 0, 0, ir)); got != "2026-07-10T09:00:00.000+0330" {
		t.Errorf("tehran: %s", got)
	}
}

func TestNormalizeSiteVariants(t *testing.T) {
	cases := map[string]string{
		"acme.atlassian.net":                            "https://acme.atlassian.net",
		"https://acme.atlassian.net/jira/software/x":    "https://acme.atlassian.net",
		"jira.corp.com/jira/browse/ABC-1":               "https://jira.corp.com/jira",
		"http://localhost:8080/":                        "http://localhost:8080",
		"  https://jira.corp.com/secure/Dashboard.jspa": "https://jira.corp.com",
	}
	for in, want := range cases {
		if got, err := NormalizeSite(in); err != nil || got != want {
			t.Errorf("NormalizeSite(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x.com", "http://"} {
		if _, err := NormalizeSite(bad); err == nil {
			t.Errorf("NormalizeSite(%q) should fail", bad)
		}
	}
}

func TestParseKey(t *testing.T) {
	cases := map[string]string{
		"https://acme.atlassian.net/browse/ABC-123":                                             "ABC-123",
		"https://jira-2.corp.com/browse/proj_x-9?focusedId=1":                                   "proj_x-9",
		"https://acme.atlassian.net/jira/software/c/projects/ABC/boards/1?selectedIssue=ABC-77": "ABC-77",
		"see ABC-5 please": "ABC-5",
		"abc-12":           "abc-12",
		"not a key":        "not a key",
	}
	for in, want := range cases {
		if got := ParseKey(in); got != want {
			t.Errorf("ParseKey(%q) = %q want %q", in, got, want)
		}
	}
}
