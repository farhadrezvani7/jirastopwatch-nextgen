//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func init() { detectBrowsers = detectLinuxBrowsers }

func detectLinuxBrowsers() []browserInfo {
	home, _ := os.UserHomeDir()
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" && home != "" {
		data = filepath.Join(home, ".local", "share")
	}
	dirs := []string{
		filepath.Join(data, "applications"),
		"/usr/local/share/applications", "/usr/share/applications",
		"/var/lib/snapd/desktop/applications",
		"/var/lib/flatpak/exports/share/applications",
		filepath.Join(data, "flatpak", "exports", "share", "applications"),
	}
	if xdg := os.Getenv("XDG_DATA_DIRS"); xdg != "" {
		for _, d := range strings.Split(xdg, ":") {
			if d != "" {
				dirs = append(dirs, filepath.Join(d, "applications"))
			}
		}
	}
	var out []browserInfo
	seenID, seenName := map[string]bool{}, map[string]bool{}
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.desktop"))
		for _, f := range files {
			id := filepath.Base(f)
			if seenID[id] {
				continue // an earlier directory (the user's own) wins, as in the desktop spec
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if info, ok := parseDesktopBrowser(id, string(b)); ok && !seenName[info.Name] {
				seenID[id], seenName[info.Name] = true, true
				out = append(out, info)
			}
		}
	}
	return out
}
