//go:build linux || windows

package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

//go:embed packaging/icon.ico
var trayIconICO []byte

// trayAvailable reports whether a notification-area icon can be shown. Windows always has one;
// on Linux it needs a StatusNotifier host (KDE, XFCE, Cinnamon, GNOME with the AppIndicator
// extension...), otherwise the icon would be invisible and the app impossible to reopen.
func trayAvailable() bool {
	if runtime.GOOS == "windows" {
		return true
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	for _, name := range []string{"org.kde.StatusNotifierWatcher", "org.freedesktop.StatusNotifierWatcher"} {
		var has bool
		if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has) == nil && has {
			return true
		}
	}
	return false
}

// runTray shows the icon and blocks until stopTray is called. Left click and "Open" show the
// window; the tooltip lists the running timer.
func runTray(s *server, open, quit func()) {
	systray.Run(func() {
		if runtime.GOOS == "windows" {
			systray.SetIcon(trayIconICO)
		} else {
			systray.SetIcon(appIcon)
		}
		systray.SetTitle("Jira StopWatch NextGen")
		systray.SetTooltip("Jira StopWatch NextGen")
		systray.SetOnTapped(open)
		mOpen := systray.AddMenuItem("Open", "Show the window")
		mPause := systray.AddMenuItem("Pause all timers", "")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "")
		go func() {
			tick := time.NewTicker(5 * time.Second)
			defer tick.Stop()
			for {
				systray.SetTooltip(s.trayTooltip())
				select {
				case <-mOpen.ClickedCh:
					open()
				case <-mPause.ClickedCh:
					s.timers.PauseAll()
				case <-mQuit.ClickedCh:
					quit()
					return
				case <-tick.C:
				}
			}
		}()
	}, nil)
}

func stopTray() { systray.Quit() }

func (s *server) trayTooltip() string {
	now := time.Now()
	for _, t := range s.timers.Snapshot() {
		if t.Running() {
			d := t.Elapsed(now)
			return fmt.Sprintf("● %s  %d:%02d", t.Key, int(d.Hours()), int(d.Minutes())%60)
		}
	}
	return "Jira StopWatch NextGen"
}
