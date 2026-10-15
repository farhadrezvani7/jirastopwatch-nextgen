package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
