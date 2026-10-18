package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestBrowserCandidatesPerOS(t *testing.T) {
	env := map[string]string{"ProgramFiles": `C:\Program Files`, "ProgramFiles(x86)": `C:\Program Files (x86)`, "LocalAppData": `C:\Users\u\AppData\Local`, "HOME": "/Users/u"}
	get := func(k string) string { return env[k] }

	win := strings.Join(browserCandidates("windows", get), "\n")
	for _, want := range []string{"msedge.exe", "chrome.exe", "Edge"} {
		if !strings.Contains(win, want) {
			t.Errorf("windows candidates missing %q", want)
		}
	}
	if strings.Contains(win, "google-chrome") {
		t.Error("windows must not list linux names")
	}
	// Windows with no env still falls back to PATH lookups.
	if c := browserCandidates("windows", func(string) string { return "" }); len(c) != 2 {
		t.Errorf("windows fallback: %v", c)
	}

	mac := browserCandidates("darwin", get)
	if mac[0] != "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" {
		t.Errorf("mac[0] = %q", mac[0])
	}
	if !strings.Contains(strings.Join(mac, "\n"), "/Users/u/Applications/Microsoft Edge.app") {
		t.Error("mac per-user Applications dir missing")
	}

	lin := strings.Join(browserCandidates("linux", get), "\n")
	for _, want := range []string{"google-chrome", "chromium", "microsoft-edge", "/snap/bin/chromium"} {
		if !strings.Contains(lin, want) {
			t.Errorf("linux candidates missing %q", want)
		}
	}
}

func TestBrowserArgs(t *testing.T) {
	a := strings.Join(browserArgs("linux", "http://x/", "/p"), " ")
	for _, want := range []string{"--app=http://x/", "--user-data-dir=/p", "--class=jirastopwatch-nextgen", "--check-for-update-interval", "--hide-crash-restore-bubble"} {
		if !strings.Contains(a, want) {
			t.Errorf("linux args missing %s", want)
		}
	}
	if w := strings.Join(browserArgs("windows", "http://x/", `C:\p`), " "); strings.Contains(w, "--class") {
		t.Error("--class is linux only")
	}
}

func TestLanguagePacksAreCompleteAndConsistent(t *testing.T) {
	js, _ := webFS.ReadFile("web/i18n.js")
	// LANGS entries look like `  de: ["Deutsch", "German"],` or `ckb: [...]`.
	codes := regexp.MustCompile(`(?m)\b([a-z]{2,3}): \["`).FindAllStringSubmatch(string(js[:bytes.Index(js, []byte("const RTL"))]), -1)
	if len(codes) < 40 {
		t.Fatalf("only %d languages declared", len(codes))
	}
	var ref map[string]string
	for _, c := range codes {
		code := c[1]
		if code == "en" {
			continue
		}
		b, err := webFS.ReadFile("web/lang/" + code + ".json")
		if err != nil {
			t.Errorf("missing pack for %s", code)
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Errorf("%s: %v", code, err)
			continue
		}
		if ref == nil {
			ref = m
		}
		if len(m) != len(ref) {
			t.Errorf("%s has %d keys, want %d", code, len(m), len(ref))
		}
		for k := range ref {
			x, ok := m[k]
			if !ok || strings.TrimSpace(x) == "" {
				t.Errorf("%s: key %q missing/empty", code, k)
				continue
			}
		}
	}
}

func TestCountryLanguageMapPointsToRealPacksAndZones(t *testing.T) {
	var cl map[string]string
	b, _ := webFS.ReadFile("web/country-lang.json")
	if err := json.Unmarshal(b, &cl); err != nil {
		t.Fatal(err)
	}
	var zones []struct {
		C string   `json:"c"`
		Z []string `json:"z"`
	}
	zb, _ := webFS.ReadFile("web/zones.json")
	if err := json.Unmarshal(zb, &zones); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, z := range zones {
		have[z.C] = true
	}
	if len(zones) < 240 {
		t.Errorf("only %d countries", len(zones))
	}
	// The app serves only zones this runtime can load. A recent tz database may
	// name zones an older Go or OS copy lacks; a few is expected, many is a bug.
	var served []struct {
		C string   `json:"c"`
		Z []string `json:"z"`
	}
	if err := json.Unmarshal(loadableZones(zb), &served); err != nil {
		t.Fatal(err)
	}
	total, kept := 0, 0
	for _, z := range zones {
		total += len(z.Z)
	}
	for _, z := range served {
		kept += len(z.Z)
		for _, name := range z.Z {
			if _, err := time.LoadLocation(name); err != nil {
				t.Errorf("served zone %s does not load", name)
			}
		}
	}
	if total-kept > 5 {
		t.Errorf("%d of %d zones can't be loaded here", total-kept, total)
	}
	if len(served) < 240 {
		t.Errorf("only %d countries remain loadable", len(served))
	}
	for c, l := range cl {
		if !have[c] {
			t.Errorf("country %s not in zones.json", c)
		}
		if _, err := webFS.ReadFile("web/lang/" + l + ".json"); err != nil && l != "en" {
			t.Errorf("country %s -> %s has no pack", c, l)
		}
	}
}
