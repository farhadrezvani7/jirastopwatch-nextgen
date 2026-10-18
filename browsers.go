package main

import (
	"bufio"
	"errors"
	"net/url"
	"os/exec"
	"sort"
	"strings"
)

// A browser the user can pick for opening Jira issues (Settings). The list is read from the
// operating system (desktop entries on Linux, the registry on Windows, /Applications on macOS).
type browserInfo struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	cmd  []string // program and arguments; the token urlToken is replaced by the page address
}

const urlToken = "%URL%"

// detectBrowsers is filled in per platform (browsers_*.go).
var detectBrowsers = func() []browserInfo { return nil }

func listBrowsers() []browserInfo {
	out := detectBrowsers()
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func findBrowser(id string) (browserInfo, bool) {
	if id == "" {
		return browserInfo{}, false
	}
	for _, b := range listBrowsers() {
		if b.ID == id {
			return b, true
		}
	}
	return browserInfo{}, false
}

// launchIn opens address in the chosen browser. The address is passed as one argument (no shell).
func launchIn(b browserInfo, address string) error {
	if len(b.cmd) == 0 {
		return errors.New("no command")
	}
	args := make([]string, 0, len(b.cmd))
	for _, a := range b.cmd[1:] {
		args = append(args, strings.ReplaceAll(a, urlToken, address))
	}
	c := exec.Command(b.cmd[0], args...)
	detach(c) // the browser must outlive this app
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// issueURLAllowed: only pages of the signed-in Jira site may be opened through the app, so the
// local API can't be used to start a browser on an arbitrary address.
func issueURLAllowed(site, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	s, err := url.Parse(strings.TrimRight(site, "/"))
	if err != nil || s.Host == "" || !strings.EqualFold(u.Host, s.Host) || u.Scheme != s.Scheme {
		return false
	}
	return u.Path == s.Path || strings.HasPrefix(u.Path, strings.TrimRight(s.Path, "/")+"/")
}

// ---- freedesktop .desktop files (Linux) ----

// parseDesktopBrowser reads a .desktop file's text and returns the browser it describes, or
// ok=false if it is not a visible application that handles web links.
func parseDesktopBrowser(id, text string) (browserInfo, bool) {
	in := false
	kv := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "["):
			in = line == "[Desktop Entry]"
		case in && line != "" && !strings.HasPrefix(line, "#"):
			if i := strings.Index(line, "="); i > 0 {
				k, v := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
				if _, seen := kv[k]; !seen {
					kv[k] = v
				}
			}
		}
	}
	if kv["Type"] != "Application" || strings.EqualFold(kv["NoDisplay"], "true") || strings.EqualFold(kv["Hidden"], "true") {
		return browserInfo{}, false
	}
	if !strings.Contains(kv["MimeType"], "x-scheme-handler/http") && !strings.Contains(kv["MimeType"], "x-scheme-handler/https") {
		return browserInfo{}, false
	}
	name, exec := kv["Name"], kv["Exec"]
	if name == "" || exec == "" {
		return browserInfo{}, false
	}
	argv := splitExec(exec)
	if len(argv) == 0 {
		return browserInfo{}, false
	}
	withURL := false
	for i, a := range argv {
		switch a {
		case "%u", "%U", "%f", "%F":
			argv[i], withURL = urlToken, true
		default:
			argv[i] = stripFieldCodes(a)
		}
	}
	clean := argv[:0]
	for _, a := range argv {
		if a != "" {
			clean = append(clean, a)
		}
	}
	if !withURL {
		clean = append(clean, urlToken)
	}
	switch {
	case strings.Contains(exec, "flatpak"):
		name += " (Flatpak)"
	case strings.Contains(exec, "/snap/") || strings.Contains(exec, "snap run"):
		name += " (Snap)"
	}
	return browserInfo{ID: "desktop:" + id, Name: name, cmd: clean}, true
}

// stripFieldCodes drops the remaining %-codes of the Desktop Entry spec (%i %c %k ...), keeping a literal %%.
func stripFieldCodes(a string) string {
	var b strings.Builder
	for i := 0; i < len(a); i++ {
		if a[i] == '%' && i+1 < len(a) {
			if a[i+1] == '%' {
				b.WriteByte('%')
			}
			i++
			continue
		}
		b.WriteByte(a[i])
	}
	return b.String()
}

// splitExec splits an Exec= line into arguments, honouring double quotes and backslash escapes.
func splitExec(s string) []string {
	var out []string
	var cur strings.Builder
	inQ, have := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			have = true
		case c == '"':
			inQ, have = !inQ, true
		case (c == ' ' || c == '\t') && !inQ:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}
