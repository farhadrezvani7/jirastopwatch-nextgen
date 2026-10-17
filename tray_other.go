//go:build !linux && !windows

package main

// macOS: a menu-bar item needs cgo, which the cross-compiled release builds don't use.
func trayAvailable() bool             { return false }
func runTray(*server, func(), func()) {}
func stopTray()                       {}
