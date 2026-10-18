//go:build darwin

package main

import (
	"os"
	"path/filepath"
)

func init() { detectBrowsers = detectMacBrowsers }

func detectMacBrowsers() []browserInfo {
	home, _ := os.UserHomeDir()
	var out []browserInfo
	for _, app := range []string{"Safari", "Google Chrome", "Firefox", "Microsoft Edge", "Brave Browser", "Arc", "Vivaldi", "Opera", "Chromium"} {
		for _, dir := range []string{"/Applications", "/System/Applications", filepath.Join(home, "Applications")} {
			if _, err := os.Stat(filepath.Join(dir, app+".app")); err == nil {
				out = append(out, browserInfo{ID: "mac:" + app, Name: app, cmd: []string{"open", "-a", app, urlToken}})
				break
			}
		}
	}
	return out
}
