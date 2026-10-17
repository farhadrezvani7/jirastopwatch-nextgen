// Package jira is a small Jira Cloud REST client.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Auth modes.
const (
	ModeCloud = "cloud" // Jira Cloud: email + API token (Basic)
	ModePAT   = "pat"   // Jira Server/Data Center: personal access token (Bearer)
	ModeBasic = "basic" // Jira Server/Data Center: username + password (Basic)
)

type Client struct {
	Base     string // https://site.atlassian.net or https://host[/context]
	Mode     string
	Identity string // email or username (unused for ModePAT)
	Secret   string // API token, PAT or password
	HTTP     *http.Client
}

type Issue struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Project string `json:"project"`
	Status  string `json:"status"`
}

type User struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
	TimeZone    string `json:"timeZone"`            // the zone Jira shows this user's times in (profile setting)
	AccountID   string `json:"accountId,omitempty"` // Cloud
	Name        string `json:"name,omitempty"`      // Server/DC username
	Key         string `json:"key,omitempty"`       // Server/DC user key
}

type Filter struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// APIError is returned for non-2xx responses.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

// IsCloudHost reports whether base points at Atlassian-hosted Jira Cloud.
func IsCloudHost(base string) bool {
	u, err := url.Parse(base)
	return err == nil && strings.HasSuffix(strings.ToLower(u.Hostname()), ".atlassian.net")
}

// DefaultMode picks the usual auth mode for a site.
func DefaultMode(base string) string {
	if IsCloudHost(base) {
		return ModeCloud
	}
	return ModePAT
}

// NormalizeSite turns "acme.atlassian.net" or a pasted page URL into the site
// root. Cloud sites lose any path; Server/DC keep an optional context path
// ("https://host/jira") but lose anything from the first app page segment on.
func NormalizeSite(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("err.addrEmpty")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", errors.New("err.addrInvalid")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("err.addrScheme")
	}
	base := u.Scheme + "://" + u.Host
	if IsCloudHost(base) {
		return base, nil
	}
	path := strings.TrimRight(u.Path, "/")
	for _, seg := range []string{"/secure", "/browse", "/projects", "/issues", "/rest", "/dashboard", "/plugins", "/login", "/servicedesk"} {
		if i := strings.Index(path, seg); i >= 0 {
			path = path[:i]
		}
	}
	return base + path, nil
}

func New(base, mode, identity, secret string) *Client {
	if mode == "" {
		mode = DefaultMode(base)
	}
	return &Client{Base: base, Mode: mode, Identity: identity, Secret: secret,
		// Don't follow redirects: Jira Cloud answers bad credentials with a 302 to
		// its login page, which would otherwise come back as "200 + HTML".
		HTTP: &http.Client{Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) cloud() bool { return c.Mode == ModeCloud || IsCloudHost(c.Base) }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u := c.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if c.Mode == ModePAT {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	} else {
		req.SetBasicAuth(c.Identity, c.Secret)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("err.unreachable|%v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 == 3 {
		return &APIError{http.StatusUnauthorized, "err.rejected"}
	}
	if resp.StatusCode/100 != 2 {
		return &APIError{resp.StatusCode, errMessage(resp.StatusCode, data)}
	}
	// Some Jira instances answer invalid credentials with 200 and an HTML login page.
	if out != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return &APIError{http.StatusUnauthorized, "err.rejected"}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func errMessage(status int, data []byte) string {
	var e struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(data, &e)
	var parts []string
	parts = append(parts, e.ErrorMessages...)
	for _, v := range e.Errors {
		parts = append(parts, v)
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	switch status {
	case 401:
		return "err.rejected"
	case 403:
		return "err.denied"
	case 404:
		return "err.notfound"
	}
	return fmt.Sprintf("err.http|%d", status)
}

func (c *Client) Myself(ctx context.Context) (*User, error) {
	var u User
	if err := c.do(ctx, "GET", "/rest/api/2/myself", nil, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ServerOffset returns the UTC offset (seconds) of the Jira server's clock, read from the
// timestamp it reports. ok is false if it can't be determined.
func (c *Client) ServerOffset(ctx context.Context) (off int, ok bool) {
	var info struct {
		ServerTime string `json:"serverTime"`
	}
	if err := c.do(ctx, "GET", "/rest/api/2/serverInfo", nil, nil, &info); err != nil {
		return 0, false
	}
	t, err := time.Parse("2006-01-02T15:04:05.000-0700", info.ServerTime)
	if err != nil {
		return 0, false
	}
	_, off = t.Zone()
	return off, true
}

// HoursPerDay returns Jira's configured working hours per day (8 on failure).
func (c *Client) HoursPerDay(ctx context.Context) float64 {
	var cfg struct {
		TT struct {
			Hours float64 `json:"workingHoursPerDay"`
		} `json:"timeTrackingConfiguration"`
	}
	if err := c.do(ctx, "GET", "/rest/api/2/configuration", nil, nil, &cfg); err != nil || cfg.TT.Hours <= 0 {
		return 8
	}
	return cfg.TT.Hours
}

type rawIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
	} `json:"fields"`
}

func (r rawIssue) issue() Issue {
	return Issue{r.Key, r.Fields.Summary, r.Fields.Project.Name, r.Fields.Status.Name}
}

const issueFields = "summary,project,status"

func (c *Client) Search(ctx context.Context, jql string, max int) ([]Issue, error) {
	var res struct {
		Issues []rawIssue `json:"issues"`
	}
	q := url.Values{"jql": {jql}, "fields": {issueFields}, "maxResults": {fmt.Sprint(max)}}
	// Cloud removed /search; Server/DC only has it.
	path := "/rest/api/2/search"
	if c.cloud() {
		path = "/rest/api/3/search/jql"
	}
	if err := c.do(ctx, "GET", path, q, nil, &res); err != nil {
		return nil, err
	}
	out := make([]Issue, len(res.Issues))
	for i, r := range res.Issues {
		out[i] = r.issue()
	}
	return out, nil
}

func (c *Client) GetIssue(ctx context.Context, key string) (*Issue, error) {
	var r rawIssue
	q := url.Values{"fields": {issueFields}}
	if err := c.do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, &r); err != nil {
		return nil, err
	}
	i := r.issue()
	return &i, nil
}

func (c *Client) Filters(ctx context.Context) ([]Filter, error) {
	var f []Filter
	err := c.do(ctx, "GET", "/rest/api/2/filter/favourite", nil, nil, &f)
	return f, err
}

// Estimate controls how Jira adjusts the remaining estimate on a worklog.
type Estimate struct {
	Method string // auto (default) | leave | new | manual
	Value  string // new remaining estimate (new) or amount to reduce by (manual)
}

// PostWorklog uses API v2 so the comment can be plain text.
func (c *Client) PostWorklog(ctx context.Context, key string, started time.Time, d time.Duration, comment string, est Estimate) error {
	body := map[string]any{"timeSpent": FormatDuration(d), "started": JiraTime(started)}
	if comment != "" {
		body["comment"] = comment
	}
	q := url.Values{}
	switch est.Method {
	case "leave":
		q.Set("adjustEstimate", "leave")
	case "new":
		q.Set("adjustEstimate", "new")
		q.Set("newEstimate", est.Value)
	case "manual":
		q.Set("adjustEstimate", "manual")
		q.Set("reduceBy", est.Value)
	case "auto":
		q.Set("adjustEstimate", "auto")
	}
	return c.do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/worklog", q, body, nil)
}

func (c *Client) PostComment(ctx context.Context, key, text string) error {
	return c.do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", nil, map[string]string{"body": text}, nil)
}

func (c *Client) Transitions(ctx context.Context, key string) ([]Transition, error) {
	var res struct {
		Transitions []Transition `json:"transitions"`
	}
	err := c.do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, nil, &res)
	return res.Transitions, err
}

func (c *Client) DoTransition(ctx context.Context, key, id string) error {
	body := map[string]any{"transition": map[string]string{"id": id}}
	return c.do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, body, nil)
}

// Estimate is an issue's remaining time estimate.
type RemainingEstimate struct {
	Text    string `json:"text"` // Jira's own formatting, e.g. "2h 30m"; empty if none
	Seconds int64  `json:"seconds"`
}

func (c *Client) Remaining(ctx context.Context, key string) (RemainingEstimate, error) {
	var r struct {
		Fields struct {
			TT struct {
				Text    string `json:"remainingEstimate"`
				Seconds int64  `json:"remainingEstimateSeconds"`
			} `json:"timetracking"`
		} `json:"fields"`
	}
	q := url.Values{"fields": {"timetracking"}}
	if err := c.do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, &r); err != nil {
		return RemainingEstimate{}, err
	}
	return RemainingEstimate{r.Fields.TT.Text, r.Fields.TT.Seconds}, nil
}

// Worklog is one time entry; only what the day totals need.
type Worklog struct {
	Key     string
	Started time.Time
	Seconds int64
}

// IsMine reports whether a worklog author (as Jira reports it) is this user.
func (u *User) IsMine(a Author) bool {
	switch {
	case u.AccountID != "" && a.AccountID != "":
		return u.AccountID == a.AccountID
	case u.Key != "" && a.Key != "":
		return u.Key == a.Key
	case u.Name != "" && a.Name != "":
		return u.Name == a.Name
	}
	return u.Email != "" && strings.EqualFold(u.Email, a.Email)
}

type Author struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	Email     string `json:"emailAddress"`
}

// WorklogsSince returns the current user's worklogs started at or after since, across all issues.
// Jira's day filter is coarse (it uses the profile time zone), so the caller filters exactly.
func (c *Client) WorklogsSince(ctx context.Context, me *User, since time.Time) ([]Worklog, error) {
	jql := fmt.Sprintf(`worklogAuthor = currentUser() AND worklogDate >= "%s"`, since.AddDate(0, 0, -1).Format("2006-01-02"))
	issues, err := c.Search(ctx, jql, 100)
	if err != nil {
		return nil, err
	}
	var out []Worklog
	for _, is := range issues {
		for startAt, pages := 0, 0; pages < 50; pages++ { // a ticket can hold thousands of worklogs
			var res struct {
				Total    int `json:"total"`
				Worklogs []struct {
					Author  Author `json:"author"`
					Started string `json:"started"`
					Seconds int64  `json:"timeSpentSeconds"`
				} `json:"worklogs"`
			}
			q := url.Values{"startAt": {fmt.Sprint(startAt)}, "maxResults": {"1000"}}
			if err := c.do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(is.Key)+"/worklog", q, nil, &res); err != nil {
				var ae *APIError
				if errors.As(err, &ae) && (ae.Status == 403 || ae.Status == 404) {
					break // one issue we may not read must not hide the rest of the day
				}
				return nil, err
			}
			for _, w := range res.Worklogs {
				st, err := time.Parse("2006-01-02T15:04:05.000-0700", w.Started)
				if err != nil || st.Before(since) || !me.IsMine(w.Author) {
					continue
				}
				out = append(out, Worklog{is.Key, st, w.Seconds})
			}
			startAt += len(res.Worklogs)
			if len(res.Worklogs) == 0 || startAt >= res.Total {
				break
			}
		}
	}
	return out, nil
}
