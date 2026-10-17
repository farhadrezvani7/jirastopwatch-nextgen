package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	for _, c := range []struct{ os, arch, want string }{
		{"linux", "amd64", "jirastopwatch-nextgen-linux-amd64"},
		{"darwin", "arm64", "jirastopwatch-nextgen-darwin-arm64"},
		{"windows", "arm64", "jirastopwatch-nextgen-windows-arm64.exe"},
	} {
		if got := assetName(c.os, c.arch); got != c.want {
			t.Errorf("assetName(%s,%s) = %s", c.os, c.arch, got)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	h := "ab" + hex.EncodeToString(make([]byte, 31))
	sums := h + "  jirastopwatch-nextgen-linux-amd64\n" + h + " *jirastopwatch-nextgen-windows-amd64.exe\nnot a line\n"
	if got, ok := checksumFor(sums, "jirastopwatch-nextgen-linux-amd64"); !ok || got != h {
		t.Errorf("linux: %q %v", got, ok)
	}
	if _, ok := checksumFor(sums, "jirastopwatch-nextgen-windows-amd64.exe"); !ok {
		t.Error("binary-mode '*name' entry not found")
	}
	if _, ok := checksumFor(sums, "missing"); ok {
		t.Error("found a checksum that is not there")
	}
}

// fakeGitHub serves one release (tag) whose binary for this platform is `bin`.
func fakeGitHub(t *testing.T, tag string, bin []byte, sum string) {
	t.Helper()
	if sum == "" {
		h := sha256.Sum256(bin)
		sum = hex.EncodeToString(h[:])
	}
	name := assetName(runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.test/rel"}`, tag)
	})
	mux.HandleFunc("/o/r/releases/download/"+tag+"/SHA256SUMS.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum, name)
	})
	mux.HandleFunc("/o/r/releases/download/"+tag+"/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(bin) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oa, od, or, ov := apiBase, downloadBase, updateRepo, version
	apiBase, downloadBase, updateRepo, version = srv.URL, srv.URL, "o/r", "1.0.0"
	t.Cleanup(func() { apiBase, downloadBase, updateRepo, version = oa, od, or, ov })
}

func TestDownloadAndSwapUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake 'binary' is a shell script")
	}
	good := []byte("#!/bin/sh\necho jirastopwatch-nextgen 2.0.0\n")
	dir := t.TempDir()
	exe := filepath.Join(dir, "app")
	os.WriteFile(exe, []byte("old"), 0o755)

	// corrupted download is refused and leaves nothing behind
	fakeGitHub(t, "v2.0.0", good, "00"+hex.EncodeToString(make([]byte, 31)))
	if _, err := downloadUpdate(t.Context(), "v2.0.0", exe); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".update-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}

	// a file that does not report the expected version is refused
	fakeGitHub(t, "v2.0.0", []byte("#!/bin/sh\necho something else\n"), "")
	if _, err := downloadUpdate(t.Context(), "v2.0.0", exe); err == nil {
		t.Fatal("wrong-version binary accepted")
	}

	// the good one is installed over the old file
	fakeGitHub(t, "v2.0.0", good, "")
	p, err := downloadUpdate(t.Context(), "v2.0.0", exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(p, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(good) {
		t.Errorf("binary not replaced: %q", b)
	}
	if st, _ := os.Stat(exe); st.Mode()&0o111 == 0 {
		t.Error("new binary is not executable")
	}
}

func TestDownloadRefusedWhenFolderNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("cannot make a folder read-only here")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "app")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.Chmod(dir, 0o555)
	oc := canElevate
	canElevate = func() bool { return false }
	defer func() { canElevate = oc }()
	defer os.Chmod(dir, 0o755)
	fakeGitHub(t, "v2.0.0", []byte("x"), "")
	if _, err := downloadUpdate(t.Context(), "v2.0.0", exe); err != errNotWritable {
		t.Errorf("got %v, want errNotWritable", err)
	}
}

func TestUpdateEndpointsRestartOnlyWhenNewer(t *testing.T) {
	s := newTestServer(t)
	fakeGitHub(t, "v1.0.0", []byte("x"), "") // same as the running version
	post := func(path string) int {
		req := httptest.NewRequest("POST", path, nil)
		req.Host = "127.0.0.1"
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, req)
		return w.Code
	}
	if c := post("/api/update/check"); c != 200 {
		t.Fatalf("check: %d", c)
	}
	if c := post("/api/update/apply"); c != 409 {
		t.Errorf("apply with nothing newer = %d, want 409", c)
	}
	if s.restartTarget() != "" {
		t.Error("restart was scheduled although there is no update")
	}
}

func TestUpdateRefusedWhileTimersRun(t *testing.T) {
	s := newTestServer(t)
	fakeGitHub(t, "v2.0.0", []byte("x"), "") // newer than the running 1.0.0
	_ = s.timers.Add("ABC-1", "s", "p", "st")
	_ = s.timers.Start("ABC-1", false)
	do := func(path string) (int, string) {
		req := httptest.NewRequest("POST", path, nil)
		req.Host = "127.0.0.1"
		req.AddCookie(&http.Cookie{Name: "sw", Value: s.token})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	if c, body := do("/api/update/check"); c != 200 || !strings.Contains(body, `"running":["ABC-1"]`) {
		t.Errorf("check: %d %s", c, body)
	}
	if c, body := do("/api/update/apply"); c != 409 || !strings.Contains(body, "err.updRunning") {
		t.Errorf("apply with a running timer: %d %s", c, body)
	}
	if s.restartTarget() != "" {
		t.Error("restart scheduled while a timer is running")
	}
	s.timers.PauseAll()
	if c, body := do("/api/update/apply"); c == 409 && strings.Contains(body, "err.updRunning") {
		t.Errorf("still refused after pausing: %s", body)
	}
}
