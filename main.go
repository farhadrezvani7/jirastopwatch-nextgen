// JiraStopWatch NextGen for Linux, Windows and macOS: a local Go server with an embedded web UI,
// shown in a standalone browser app window.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"jirastopwatch-nextgen/internal/lockwatch"
	"jirastopwatch-nextgen/internal/store"
)

func main() {
	noOpen := flag.Bool("no-open", false, "don't open a window; just print the URL")
	install := flag.Bool("install", false, "add to the applications menu (Linux)")
	uninstall := flag.Bool("uninstall", false, "remove from the applications menu (Linux)")
	flag.Parse()
	if *install || *uninstall {
		if runtime.GOOS != "linux" {
			fmt.Println("--install is only needed on Linux; use the installer or .app from the Releases page.")
			return
		}
		msg, err := installDesktop(*uninstall)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(msg)
		return
	}

	s := newServer()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", ln.Addr().(*net.TCPAddr).Port, s.token)
	go func() { log.Fatal(http.Serve(ln, s.handler())) }()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go lockwatch.Watch(ctx, 3*time.Second, s.onLock)
	if !s.noUpdateCheck() {
		go s.checkUpdate()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	if *noOpen {
		fmt.Println(url)
		go func() { <-sig; close(done) }()
	} else if cmd := appWindow(url); cmd != nil {
		go func() { _ = cmd.Wait(); close(done) }()
		go func() { <-sig; _ = cmd.Process.Kill() }()
	} else if openDefaultBrowser(url) {
		// No app-mode browser: the page is in a normal tab, so quit once it stops pinging.
		go func() { s.waitForPageGone(3 * time.Minute); close(done) }()
		go func() { <-sig; close(done) }()
	} else {
		fmt.Println("No Chrome/Edge/Chromium found. Open this in a browser:\n" + url)
		go func() { <-sig; close(done) }()
	}
	<-done // by default running timers keep counting (wall-clock) and are restored on next launch
	s.onExit()
}

// browserCandidates lists Chromium-family browsers (they all support --app) to
// try for the given OS. Edge ships with Windows 10/11, so a Windows machine
// always has at least one.
func browserCandidates(goos string, getenv func(string) string) []string {
	switch goos {
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			base := getenv(env)
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(base, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
				filepath.Join(base, "Vivaldi", "Application", "vivaldi.exe"),
				filepath.Join(base, "Chromium", "Application", "chrome.exe"),
			)
		}
		return append(out, "msedge.exe", "chrome.exe")
	case "darwin":
		var out []string
		home := getenv("HOME")
		for _, app := range []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Vivaldi", "Chromium"} {
			rel := "Applications/" + app + ".app/Contents/MacOS/" + app
			out = append(out, "/"+rel)
			if home != "" {
				out = append(out, home+"/"+rel)
			}
		}
		return out
	}
	return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser", "brave",
		"microsoft-edge", "microsoft-edge-stable", "vivaldi", "vivaldi-stable", "/snap/bin/chromium", "/opt/google/chrome/chrome"}
}

// browserArgs are the flags for the app window. Besides sizing, they keep the
// private profile quiet: no "browser is out of date" / "didn't shut down
// correctly" bars, no first-run pages, no background update or sync traffic.
func browserArgs(goos, url, profile string) []string {
	args := []string{
		"--app=" + url,
		"--user-data-dir=" + profile,
		"--window-size=1040,760",
		"--no-first-run", "--no-default-browser-check",
		"--check-for-update-interval=31536000",
		"--disable-component-update", "--disable-background-networking", "--disable-sync",
		"--disable-session-crashed-bubble", "--hide-crash-restore-bubble", "--noerrdialogs",
		"--disable-features=Translate,InfiniteSessionRestore,MediaRouter",
		"--password-store=basic", "--disable-infobars",
	}
	if goos == "linux" {
		args = append(args, "--class=jirastopwatch-nextgen")
	}
	return args
}

// appWindow starts a Chromium-family browser in chromeless "app" mode with a
// private profile, so the process lives exactly as long as the window does.
func appWindow(url string) *exec.Cmd {
	for _, bin := range browserCandidates(runtime.GOOS, os.Getenv) {
		path := bin
		if filepath.IsAbs(bin) {
			if st, err := os.Stat(bin); err != nil || st.IsDir() {
				continue
			}
		} else if p, err := exec.LookPath(bin); err == nil {
			path = p
		} else {
			continue
		}
		cmd := exec.Command(path, browserArgs(runtime.GOOS, url, filepath.Join(store.Dir(), "window-profile"))...)
		if cmd.Start() == nil {
			return cmd
		}
	}
	return nil
}

// openDefaultBrowser opens url in the user's default browser.
func openDefaultBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start() == nil
}
