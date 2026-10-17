package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginRedirectAndHTMLAreAuthFailures(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer redirect.Close()
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>login</html>"))
	}))
	defer html.Close()

	for name, url := range map[string]string{"302 to login": redirect.URL, "200 html": html.URL} {
		_, err := New(url, ModeBasic, "u", "p").Myself(context.Background())
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != 401 || ae.Message != "err.rejected" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestRemainingEstimate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"fields":{"timetracking":{"remainingEstimate":"2h","remainingEstimateSeconds":7200}}}`))
	}))
	defer srv.Close()
	e, err := New(srv.URL, ModeBasic, "u", "p").Remaining(context.Background(), "A-1")
	if err != nil || e.Text != "2h" || e.Seconds != 7200 {
		t.Errorf("%+v %v", e, err)
	}
}

func TestWorklogsSincePagesAndFiltersByAuthorAndTime(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{}}]}`))
	})
	mux.HandleFunc("/rest/api/2/issue/A-1/worklog", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startAt") == "0" {
			_, _ = w.Write([]byte(`{"total":3,"worklogs":[
			 {"author":{"name":"me"},"started":"2026-10-17T09:00:00.000+0000","timeSpentSeconds":600},
			 {"author":{"name":"other"},"started":"2026-10-17T09:00:00.000+0000","timeSpentSeconds":9999}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"total":3,"worklogs":[
		 {"author":{"name":"me"},"started":"2026-10-17T10:00:00.000+0000","timeSpentSeconds":1200},
		 {"author":{"name":"me"},"started":"2026-10-16T10:00:00.000+0000","timeSpentSeconds":5000}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	since := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	logs, err := New(srv.URL, ModeBasic, "u", "p").WorklogsSince(context.Background(), &User{Name: "me"}, since)
	var sum int64
	for _, l := range logs {
		sum += l.Seconds
	}
	if err != nil || sum != 1800 {
		t.Errorf("sum=%d err=%v logs=%+v", sum, err, logs)
	}
}

func TestSearchPageCursorServerAndCloud(t *testing.T) {
	var gotStart, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStart, gotToken = r.URL.Query().Get("startAt"), r.URL.Query().Get("nextPageToken")
		if r.URL.Path == "/rest/api/3/search/jql" {
			_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{}}],"nextPageToken":"tok2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"startAt":50,"total":120,"issues":[{"key":"A-51","fields":{}},{"key":"A-52","fields":{}}]}`))
	}))
	defer srv.Close()
	_, next, err := New(srv.URL, ModeBasic, "u", "p").SearchPage(context.Background(), "x", 50, "50")
	if err != nil || next != "52" || gotStart != "50" {
		t.Errorf("server paging: next=%q start=%q err=%v", next, gotStart, err)
	}
	_, next, err = New(srv.URL, ModeCloud, "u", "p").SearchPage(context.Background(), "x", 50, "tok1")
	if err != nil || next != "tok2" || gotToken != "tok1" {
		t.Errorf("cloud paging: next=%q token=%q err=%v", next, gotToken, err)
	}
}

// Every feature that reads from Jira must work with all three ways of signing in.
func TestReadFeaturesWorkInAllThreeLoginModes(t *testing.T) {
	type tc struct {
		name, mode, identity, secret string
		wantAuth, searchPath         string
		myself, author               string // JSON of the user and of a worklog author, as that Jira flavour reports them
	}
	cases := []tc{
		{"cloud: email + API token", ModeCloud, "me@x.com", "tok", "Basic bWVAeC5jb206dG9r", "/rest/api/3/search/jql",
			`{"displayName":"Me","accountId":"acc-1","emailAddress":"me@x.com"}`, `{"accountId":"acc-1"}`},
		{"server: personal access token", ModePAT, "", "pat123", "Bearer pat123", "/rest/api/2/search",
			`{"displayName":"Me","name":"me","key":"JIRAUSER1"}`, `{"name":"me","key":"JIRAUSER1"}`},
		{"server: username + password", ModeBasic, "me", "pw", "Basic bWU6cHc=", "/rest/api/2/search",
			`{"displayName":"Me","name":"me","key":"JIRAUSER1"}`, `{"name":"me","key":"JIRAUSER1"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var searched []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != c.wantAuth {
					t.Errorf("%s: Authorization = %q, want %q", r.URL.Path, got, c.wantAuth)
				}
				switch r.URL.Path {
				case "/rest/api/2/myself":
					_, _ = w.Write([]byte(c.myself))
				case c.searchPath:
					searched = append(searched, r.URL.Query().Get("jql"))
					_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{"summary":"s","project":{"name":"P"},"status":{"name":"Open"}}}],"total":1,"nextPageToken":""}`))
				case "/rest/api/2/issue/A-1/worklog":
					_, _ = w.Write([]byte(`{"total":2,"worklogs":[
					 {"author":` + c.author + `,"started":"2026-10-17T09:00:00.000+0000","timeSpentSeconds":900},
					 {"author":{"accountId":"someone-else","name":"other","key":"OTHER"},"started":"2026-10-17T09:00:00.000+0000","timeSpentSeconds":7777}]}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			cl := New(srv.URL, c.mode, c.identity, c.secret)
			me, err := cl.Myself(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			logs, err := cl.WorklogsSince(context.Background(), me, time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
			if err != nil || len(logs) != 1 || logs[0].Seconds != 900 {
				t.Errorf("today's worklogs (only mine): %+v err=%v", logs, err)
			}
			issues, _, err := cl.SearchPage(context.Background(), `created >= "2000-01-01" ORDER BY updated DESC`, 50, "")
			if err != nil || len(issues) != 1 || issues[0].Key != "A-1" {
				t.Errorf("all-issues search: %+v err=%v", issues, err)
			}
			if len(searched) != 2 {
				t.Errorf("expected the worklog search and the all-issues search on %s, got %v", c.searchPath, searched)
			}
		})
	}
}

func TestHasWorklogMatchesMineByStartMinuteAndLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":2,"worklogs":[
		 {"author":{"name":"me"},"started":"2026-10-17T09:00:30.000+0000","timeSpentSeconds":5400},
		 {"author":{"name":"other"},"started":"2026-10-17T11:00:00.000+0000","timeSpentSeconds":3600}]}`))
	}))
	defer srv.Close()
	cl, me := New(srv.URL, ModeBasic, "u", "p"), &User{Name: "me"}
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		at   time.Time
		d    time.Duration
		want bool
	}{
		{"same entry", at, 90 * time.Minute, true},
		{"different length", at, 60 * time.Minute, false},
		{"different start", at.Add(time.Hour), 90 * time.Minute, false},
		{"someone else's entry", time.Date(2026, 10, 17, 11, 0, 0, 0, time.UTC), time.Hour, false},
	} {
		if got, err := cl.HasWorklog(context.Background(), me, "A-1", c.at, c.d); err != nil || got != c.want {
			t.Errorf("%s: got %v err=%v, want %v", c.name, got, err, c.want)
		}
	}
}

func TestIsMineIgnoresCaseOfNamesAndKeys(t *testing.T) {
	u := &User{Name: "Farhad_Rezvani", Key: "JIRAUSER29505"}
	if !u.IsMine(Author{Key: "jirauser29505"}) || !u.IsMine(Author{Name: "farhad_rezvani"}) {
		t.Error("the same user written in another case was not recognised")
	}
	if u.IsMine(Author{Key: "JIRAUSER1"}) {
		t.Error("a different user was taken for me")
	}
}
