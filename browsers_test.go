package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const chromeDesktop = `[Desktop Entry]
Version=1.0
Name=Google Chrome
Name[de]=Google Chrome Browser
Exec=/usr/bin/google-chrome-stable %U
Terminal=false
Type=Application
Categories=Network;WebBrowser;
MimeType=text/html;x-scheme-handler/http;x-scheme-handler/https;

[Desktop Action new-window]
Name=New Window
Exec=/usr/bin/google-chrome-stable
`

func TestParseDesktopBrowser(t *testing.T) {
	b, ok := parseDesktopBrowser("google-chrome.desktop", chromeDesktop)
	if !ok || b.Name != "Google Chrome" || b.ID != "desktop:google-chrome.desktop" ||
		!reflect.DeepEqual(b.cmd, []string{"/usr/bin/google-chrome-stable", urlToken}) {
		t.Fatalf("chrome: %+v ok=%v", b, ok)
	}
	// field codes other than the URL are dropped; quotes and a trailing-less Exec still get the URL
	b, ok = parseDesktopBrowser("ff.desktop", "[Desktop Entry]\nType=Application\nName=Firefox\nExec=env MOZ=1 \"/opt/Fire fox/firefox\" --new-tab %i %c\nMimeType=x-scheme-handler/https;\n")
	if !ok || !reflect.DeepEqual(b.cmd, []string{"env", "MOZ=1", "/opt/Fire fox/firefox", "--new-tab", urlToken}) {
		t.Errorf("firefox: %+v ok=%v", b, ok)
	}
	b, _ = parseDesktopBrowser("fp.desktop", "[Desktop Entry]\nType=Application\nName=Brave\nExec=/usr/bin/flatpak run com.brave.Browser @@u %U @@\nMimeType=x-scheme-handler/http;\n")
	if b.Name != "Brave (Flatpak)" {
		t.Errorf("flatpak label: %q", b.Name)
	}
	for name, text := range map[string]string{
		"not a browser":  "[Desktop Entry]\nType=Application\nName=Editor\nExec=edit %F\nMimeType=text/plain;\n",
		"hidden":         "[Desktop Entry]\nType=Application\nNoDisplay=true\nName=X\nExec=x %U\nMimeType=x-scheme-handler/http;\n",
		"not an app":     "[Desktop Entry]\nType=Link\nName=X\nExec=x %U\nMimeType=x-scheme-handler/http;\n",
		"only an action": "[Desktop Action a]\nName=X\nExec=x %U\nMimeType=x-scheme-handler/http;\n",
	} {
		if _, ok := parseDesktopBrowser("x.desktop", text); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestIssueURLAllowedOnlyForTheJiraSite(t *testing.T) {
	site := "https://jira.example.com/jira"
	for raw, want := range map[string]bool{
		"https://jira.example.com/jira/browse/ABC-1":     true,
		"https://JIRA.example.com/jira/browse/ABC-1?x=1": true,
		"https://jira.example.com/jira":                  true,
		"http://jira.example.com/jira/browse/ABC-1":      false, // other scheme
		"https://evil.example.com/jira/browse/ABC-1":     false,
		"https://jira.example.com.evil.com/jira/x":       false,
		"https://jira.example.com/jirax/browse/ABC-1":    false, // path prefix must end at a "/"
		"https://jira.example.com/other":                 false,
		"file:///etc/passwd":                             false,
		"javascript:alert(1)":                            false,
		"-https://jira.example.com/jira/x":               false,
		"":                                               false,
	} {
		if got := issueURLAllowed(site, raw); got != want {
			t.Errorf("issueURLAllowed(%q) = %v, want %v", raw, got, want)
		}
	}
	if !issueURLAllowed("https://acme.atlassian.net", "https://acme.atlassian.net/browse/ABC-1") {
		t.Error("a Cloud site root must allow its /browse pages")
	}
}

// fakeBrowserEnv installs a "browser" (a script that records its arguments) the way the desktop does,
// and a fake xdg-open as the system default. Unix only.
func fakeBrowserEnv(t *testing.T) (browserLog, defaultLog string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("uses .desktop files and shell scripts")
	}
	dir := t.TempDir()
	browserLog, defaultLog = filepath.Join(dir, "browser.log"), filepath.Join(dir, "default.log")
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	script := func(path, log string) {
		os.WriteFile(path, []byte("#!/bin/sh\nfor a in \"$@\"; do echo \"ARG:$a\" >> '"+log+"'; done\n"), 0o755)
	}
	script(filepath.Join(bin, "fakebrowser"), browserLog)
	script(filepath.Join(bin, "xdg-open"), defaultLog)
	apps := filepath.Join(dir, "share", "applications")
	os.MkdirAll(apps, 0o755)
	os.WriteFile(filepath.Join(apps, "fakebrowser.desktop"), []byte("[Desktop Entry]\nType=Application\nName=Fake Browser\nExec="+filepath.Join(bin, "fakebrowser")+" --new-tab %u\nMimeType=x-scheme-handler/http;x-scheme-handler/https;\n"), 0o644)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "share"))
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return
}

func waitFor(t *testing.T, path string) string {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func TestChosenBrowserOpensTheIssueAndDefaultStaysTheFallback(t *testing.T) {
	browserLog, defaultLog := fakeBrowserEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	s := newTestServer(t)
	s.mu.Lock()
	s.cfg.Site = "https://jira.example.com"
	s.mu.Unlock()
	do := func(method, path, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1"
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	if c, b := do("GET", "/api/browsers", ""); c != 200 || !strings.Contains(b, `"Fake Browser"`) || !strings.Contains(b, `desktop:fakebrowser.desktop`) {
		t.Fatalf("list: %d %s", c, b)
	}
	if c, _ := do("POST", "/api/settings", `{"browser":"desktop:nothing.desktop"}`); c != 400 {
		t.Errorf("an unknown browser was accepted: %d", c)
	}
	// nothing chosen: the system default opens the page, exactly as before
	url := "https://jira.example.com/browse/ABC-1?a=1&b=two words"
	if c, b := do("POST", "/api/open", `{"url":"`+url+`"}`); c != 200 || waitFor(t, defaultLog) == "" {
		t.Fatalf("default open: %d %s", c, b)
	}
	if got := waitFor(t, defaultLog); got != "ARG:"+url+"\n" {
		t.Errorf("default browser got %q", got)
	}
	// chosen: only that browser, the address as one argument
	if c, _ := do("POST", "/api/settings", `{"browser":"desktop:fakebrowser.desktop"}`); c != 200 {
		t.Fatal("could not choose the browser")
	}
	if c, b := do("POST", "/api/open", `{"url":"`+url+`"}`); c != 200 || strings.Contains(b, "fellBack") {
		t.Fatalf("open: %d %s", c, b)
	}
	if got := waitFor(t, browserLog); got != "ARG:--new-tab\nARG:"+url+"\n" {
		t.Errorf("chosen browser got %q", got)
	}
	// only the Jira site is allowed
	if c, _ := do("POST", "/api/open", `{"url":"https://evil.example.com/x"}`); c != 400 {
		t.Errorf("a foreign address was opened: %d", c)
	}
	// the chosen browser disappears: fall back to the default and say so
	os.Remove(browserLog)
	os.RemoveAll(os.Getenv("XDG_DATA_HOME"))
	os.Remove(defaultLog)
	if c, b := do("POST", "/api/open", `{"url":"https://jira.example.com/browse/ABC-2"}`); c != 200 || !strings.Contains(b, `"fellBack":true`) {
		t.Errorf("fallback: %d %s", c, b)
	}
	if got := waitFor(t, defaultLog); got != "ARG:https://jira.example.com/browse/ABC-2\n" {
		t.Errorf("fallback opened %q", got)
	}
}
