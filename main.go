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
	"sync"
	"syscall"
	"time"

	"jirastopwatch-nextgen/internal/lockwatch"
	"jirastopwatch-nextgen/internal/store"
)

func main() {
	noOpen := flag.Bool("no-open", false, "don't open a window; just print the URL")
	install := flag.Bool("install", false, "add to the applications menu (Linux)")
	uninstall := flag.Bool("uninstall", false, "remove from the applications menu (Linux)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("jirastopwatch-nextgen " + version)
		return
	}
	removeOldBinary()
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

	if !acquireLock() { // already running: show that copy instead of starting a second one
		u := waitForInstance(6 * time.Second)
		switch {
		case u == "":
			fmt.Println("Jira StopWatch NextGen is already running.")
		case *noOpen:
			fmt.Println(u)
		default:
			if cmd := appWindow(u); cmd == nil && !openDefaultBrowser(u) {
				fmt.Println("Already running. Open this in a browser:\n" + u)
			}
		}
		return
	}
	crashed := previousRunEndedBadly()
	s := newServer()
	if crashed {
		s.recoverFromCrash(lastAlive())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", ln.Addr().(*net.TCPAddr).Port, s.token)
	go func() { log.Fatal(http.Serve(ln, s.handler())) }()
	writeInstance(url)
	stopBeat := make(chan struct{})
	go heartbeat(stopBeat)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go lockwatch.Watch(ctx, 3*time.Second, s.onLock)
	if !s.noUpdateCheck() {
		go s.checkUpdate()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	finish := sync.OnceFunc(func() { close(done) })
	s.setQuit(finish) // "install update" shuts the app down; main then starts the new version
	if *noOpen {
		fmt.Println(url)
		go func() { <-sig; finish() }()
	} else if cmd := appWindow(url); cmd != nil {
		// With a tray icon, closing the window keeps the app (and its timers) running in the
		// tray; "Open" brings the window back and "Quit" ends everything. Without one, closing
		// the window quits, as there would be no way to reopen it.
		tray := trayAvailable()
		var mu sync.Mutex
		cmds := []*exec.Cmd{cmd}
		killAll := func() {
			mu.Lock()
			defer mu.Unlock()
			for _, c := range cmds {
				_ = c.Process.Kill()
			}
		}
		watch := func(c *exec.Cmd) {
			_ = c.Wait()
			if !tray {
				finish()
			}
		}
		go watch(cmd)
		go func() { <-sig; killAll(); finish() }()
		s.setQuit(func() { killAll(); finish() })
		if tray {
			go runTray(s, func() {
				// A running window just takes focus (Chrome hands the request to it); otherwise a new one opens.
				if c := appWindow(url); c != nil {
					mu.Lock()
					cmds = append(cmds, c)
					mu.Unlock()
					go watch(c)
				}
			}, func() { killAll(); finish() })
			defer stopTray()
		}
	} else if openDefaultBrowser(url) {
		// No app-mode browser: the page is in a normal tab, so quit once it stops pinging.
		go func() { s.waitForPageGone(3 * time.Minute); finish() }()
		go func() { <-sig; finish() }()
	} else {
		fmt.Println("No Chrome/Edge/Chromium found. Open this in a browser:\n" + url)
		go func() { <-sig; finish() }()
	}
	<-done // by default running timers keep counting (wall-clock) and are restored on next launch
	s.onExit()
	close(stopBeat)
	removeInstance()
	releaseLock()                            // before the updated copy starts below, which must be able to take it
	if exe := s.restartTarget(); exe != "" { // an update was installed: start the new version
		cmd := exec.Command(exe, os.Args[1:]...)
		detach(cmd)
		if err := cmd.Start(); err != nil {
			log.Println("restart failed:", err)
		}
	}
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
