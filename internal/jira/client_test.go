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
