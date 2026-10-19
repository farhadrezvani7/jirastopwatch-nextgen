package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestMyWorklogsPagesAndFiltersByAuthorAndDay(t *testing.T) {
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
	logs, _, err := New(srv.URL, ModeBasic, "u", "p").MyWorklogs(context.Background(), &User{Name: "me"}, "2026-10-17", "2026-10-17")
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
			logs, _, err := cl.MyWorklogs(context.Background(), me, "2026-10-17", "2026-10-17")
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
	u := &User{Name: "Jane_Doe", Key: "JIRAUSER10001"}
	if !u.IsMine(Author{Key: "jirauser10001"}) || !u.IsMine(Author{Name: "jane_doe"}) {
		t.Error("the same user written in another case was not recognised")
	}
	if u.IsMine(Author{Key: "JIRAUSER1"}) {
		t.Error("a different user was taken for me")
	}
}

// Queues (Jira Service Management) must work with all three sign-in modes, page correctly, send the
// experimental opt-in header, and a Jira without Service Management simply has none.
func TestServiceDeskQueuesInAllThreeLoginModes(t *testing.T) {
	for _, c := range []struct{ name, mode, id, secret, auth string }{
		{"cloud", ModeCloud, "me@x.com", "tok", "Basic bWVAeC5jb206dG9r"},
		{"pat", ModePAT, "", "pat1", "Bearer pat1"},
		{"basic", ModeBasic, "me", "pw", "Basic bWU6cHc="},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				isQueueAPI := strings.HasPrefix(r.URL.Path, "/rest/servicedeskapi/")
				if r.Header.Get("Authorization") != c.auth || (isQueueAPI && r.Header.Get("X-ExperimentalApi") != "opt-in") {
					t.Errorf("%s: auth=%q experimental=%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-ExperimentalApi"))
				}
				start := r.URL.Query().Get("start")
				switch {
				case r.URL.Path == "/rest/servicedeskapi/servicedesk":
					_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"20","projectKey":"HD","projectName":"HelpDesk"},{"id":"5","projectKey":"MIS","projectName":"MIS"}]}`))
				case r.URL.Path == "/rest/servicedeskapi/servicedesk/20/queue" && start == "0":
					_, _ = w.Write([]byte(`{"isLastPage":false,"values":[{"id":"425","name":"Support","issueCount":8}]}`))
				case r.URL.Path == "/rest/servicedeskapi/servicedesk/20/queue":
					_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"400","name":"Other","issueCount":0}]}`))
				case r.URL.Path == "/rest/servicedeskapi/servicedesk/20/queue/425/issue":
					_, _ = w.Write([]byte(`{"isLastPage":false,"values":[{"key":"HD-2"},{"key":"HD-1"}]}`))
				case strings.HasSuffix(r.URL.Path, "/search") || strings.HasSuffix(r.URL.Path, "/search/jql"):
					if !strings.Contains(r.URL.Query().Get("jql"), `"HD-2","HD-1"`) {
						t.Errorf("details query: %q", r.URL.Query().Get("jql"))
					}
					// Jira answers in its own order; the queue's order must win
					_, _ = w.Write([]byte(`{"issues":[{"key":"HD-1","fields":{"summary":"one","project":{"name":"P"},"status":{"name":"Open"}}},{"key":"HD-2","fields":{"summary":"two","project":{"name":"P"},"status":{"name":"Open"}}}]}`))
				default:
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			cl := New(srv.URL, c.mode, c.id, c.secret)
			desks, err := cl.ServiceDesks(context.Background())
			if err != nil || len(desks) != 2 || desks[0].Key != "HD" || desks[0].Name != "HelpDesk" {
				t.Fatalf("desks: %+v %v", desks, err)
			}
			qs, err := cl.Queues(context.Background(), "20")
			if err != nil || len(qs) != 2 || qs[0].Count != 8 || qs[1].Name != "Other" {
				t.Fatalf("queues (paged): %+v %v", qs, err)
			}
			keys, next, err := cl.QueueIssueKeys(context.Background(), "20", "425", 2, "")
			if err != nil || len(keys) != 2 || next != "2" {
				t.Fatalf("queue keys: %v next=%q %v", keys, next, err)
			}
			issues, err := cl.IssuesByKeys(context.Background(), keys)
			if err != nil || len(issues) != 2 || issues[0].Key != "HD-2" || issues[1].Summary != "one" {
				t.Errorf("details in queue order: %+v %v", issues, err)
			}
			if got, _ := cl.IssuesByKeys(context.Background(), []string{`X-1") OR (1=1`}); len(got) != 0 {
				t.Error("an invalid key reached the query")
			}
		})
	}
}

func TestJiraWithoutServiceManagementHasNoQueues(t *testing.T) {
	for _, status := range []int{404, 403, 401} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		desks, err := New(srv.URL, ModePAT, "", "x").ServiceDesks(context.Background())
		srv.Close()
		if err != nil || len(desks) != 0 {
			t.Errorf("status %d: desks=%v err=%v (want none, no error)", status, desks, err)
		}
	}
}

// My work: the day of a worklog is the date part of "started" exactly as Jira wrote it, only the user's own
// worklogs are returned, Jira's own text is kept, and every sign-in mode works.
func TestMyWorklogsDayRuleAndOwnershipInAllThreeLoginModes(t *testing.T) {
	for _, c := range []struct{ name, mode, id, secret, auth, search, user, author string }{
		{"cloud", ModeCloud, "me@x.com", "tok", "Basic bWVAeC5jb206dG9r", "/rest/api/3/search/jql", `{"accountId":"acc-1"}`, `{"accountId":"acc-1"}`},
		{"pat", ModePAT, "", "pat1", "Bearer pat1", "/rest/api/2/search", `{"name":"me","key":"U1"}`, `{"name":"me","key":"U1"}`},
		{"basic", ModeBasic, "me", "pw", "Basic bWU6cHc=", "/rest/api/2/search", `{"name":"me","key":"U1"}`, `{"name":"me","key":"U1"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != c.auth {
					t.Errorf("%s: auth %q", r.URL.Path, r.Header.Get("Authorization"))
				}
				switch r.URL.Path {
				case c.search:
					_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{"summary":"First","project":{"name":"P"},"status":{"name":"Open"}}}],"total":1}`))
				case "/rest/api/2/issue/A-1/worklog":
					_, _ = w.Write([]byte(`{"total":5,"worklogs":[
					 {"id":"11","author":` + c.author + `,"started":"2026-10-15T23:11:00.000+0000","timeSpentSeconds":5400,"timeSpent":"1h 30m","comment":"late one"},
					 {"id":"12","author":` + c.author + `,"started":"2026-10-16T09:00:00.000+0330","timeSpentSeconds":600,"timeSpent":"10m"},
					 {"id":"13","author":{"accountId":"x","name":"other","key":"OTHER"},"started":"2026-10-16T10:00:00.000+0000","timeSpentSeconds":999,"timeSpent":"16m"},
					 {"id":"14","author":` + c.author + `,"started":"2026-10-09T10:00:00.000+0000","timeSpentSeconds":60,"timeSpent":"1m"},
					 {"id":"15","author":` + c.author + `,"started":"2026-10-18T00:00:00.000+0000","timeSpentSeconds":60,"timeSpent":"1m"}]}`))
				default:
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			var me User
			_ = json.Unmarshal([]byte(c.user), &me)
			got, _, err := New(srv.URL, c.mode, c.id, c.secret).MyWorklogs(context.Background(), &me, "2026-10-15", "2026-10-17")
			if err != nil || len(got) != 2 {
				t.Fatalf("entries: %+v err=%v", got, err)
			}
			// 23:11 UTC stays on the 15th (no conversion to the user's zone); the +0330 entry keeps its own date and zone
			a, b := got[0], got[1]
			if a.ID != "11" || a.Day != "2026-10-15" || a.Time != "23:11" || a.Offset != "+0000" || a.TimeSpent != "1h 30m" || a.Seconds != 5400 || a.Comment != "late one" || a.Key != "A-1" || a.Summary != "First" {
				t.Errorf("first entry: %+v", a)
			}
			if b.ID != "12" || b.Day != "2026-10-16" || b.Offset != "+0330" || b.TimeSpent != "10m" {
				t.Errorf("second entry: %+v", b)
			}
		})
	}
}

func TestUpdateAndDeleteWorklogRequests(t *testing.T) {
	var got []string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		body = string(b)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	cl := New(srv.URL, ModePAT, "", "x")
	at := time.Date(2026, 10, 15, 9, 30, 0, 0, time.FixedZone("", 3*3600+1800))
	if err := cl.UpdateWorklog(context.Background(), "A-1", "11", at, 90*time.Minute, "note", Estimate{}); err != nil {
		t.Fatal(err)
	}
	if got[0] != "PUT /rest/api/2/issue/A-1/worklog/11?adjustEstimate=auto" || !strings.Contains(body, `"timeSpent":"1h 30m"`) || !strings.Contains(body, `"started":"2026-10-15T09:30:00.000+0330"`) || !strings.Contains(body, `"comment":"note"`) {
		t.Errorf("update: %v %s", got, body)
	}
	if err := cl.DeleteWorklog(context.Background(), "A-1", "11"); err != nil || got[1] != "DELETE /rest/api/2/issue/A-1/worklog/11?adjustEstimate=auto" {
		t.Errorf("delete: %v %v", got, err)
	}
	if !ValidDay("2026-10-17") || ValidDay("2026-13-01") || ValidDay("2026-1-1") || ValidDay("") {
		t.Error("ValidDay")
	}
}

// A reply that stops short (the connection drops after the headers) must be retried for reads and, if it
// keeps failing, reported as a clear error, never as "unexpected end of JSON input".
func TestTruncatedReplyIsRetriedAndReportedClearly(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"total":1,"worklo`))
		if n < 3 {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return // the handler returns with 82 bytes still owed: the client sees an unexpected EOF
		}
	}))
	defer srv.Close()
	_, err := New(srv.URL, ModeBasic, "u", "p").Remaining(context.Background(), "A-1")
	if err == nil || strings.Contains(err.Error(), "unexpected end of JSON") {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != attempts {
		t.Errorf("calls = %d, want %d", calls.Load(), attempts)
	}
	if !strings.HasPrefix(err.Error(), "err.truncated") {
		t.Errorf("err = %v", err)
	}
}

func TestReadIsRetriedAfterGatewayError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"fields":{"timetracking":{"remainingEstimate":"2h","remainingEstimateSeconds":7200}}}`))
	}))
	defer srv.Close()
	e, err := New(srv.URL, ModeBasic, "u", "p").Remaining(context.Background(), "A-1")
	if err != nil || e.Seconds != 7200 || calls.Load() != 2 {
		t.Errorf("%+v %v calls=%d", e, err, calls.Load())
	}
}

// Changing data is never repeated automatically: a retried POST could book the same time twice.
func TestWriteIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()
	err := New(srv.URL, ModeBasic, "u", "p").DeleteWorklog(context.Background(), "A-1", "10")
	if err == nil || calls.Load() != 1 {
		t.Errorf("err=%v calls=%d", err, calls.Load())
	}
}

func TestBodyThatIsNotJSONIsReportedClearly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fields":`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, ModeBasic, "u", "p").Remaining(context.Background(), "A-1")
	if err == nil || !strings.HasPrefix(err.Error(), "err.badResponse|") {
		t.Errorf("err = %v", err)
	}
}

// One issue whose worklogs cannot be read must not hide the rest of the week.
func TestMyWorklogsKeepsGoodIssuesWhenOneFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{}},{"key":"B-2","fields":{}}]}`))
	})
	mux.HandleFunc("/rest/api/2/issue/A-1/worklog", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":1,"worklogs":[{"author":{"name":"me"},"started":"2026-10-17T09:00:00.000+0000","timeSpentSeconds":600}]}`))
	})
	mux.HandleFunc("/rest/api/2/issue/B-2/worklog", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	logs, skipped, err := New(srv.URL, ModeBasic, "u", "p").MyWorklogs(context.Background(), &User{Name: "me"}, "2026-10-17", "2026-10-17")
	if err != nil || len(logs) != 1 || len(skipped) != 1 || skipped[0] != "B-2" {
		t.Errorf("logs=%+v skipped=%v err=%v", logs, skipped, err)
	}
}

func TestMyWorklogsFailsWhenEveryIssueFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"key":"A-1","fields":{}}]}`))
	})
	mux.HandleFunc("/rest/api/2/issue/A-1/worklog", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := New(srv.URL, ModeBasic, "u", "p").MyWorklogs(context.Background(), &User{Name: "me"}, "2026-10-17", "2026-10-17")
	if err == nil {
		t.Error("expected an error")
	}
}
