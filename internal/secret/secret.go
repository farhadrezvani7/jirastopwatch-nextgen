// Package secret keeps the Jira token in the operating system's credential
// store (Windows Credential Manager, macOS Keychain, Linux Secret Service via
// secret-tool) instead of a plain file. Where no store is available every call
// returns ErrUnavailable and the caller falls back to the config file.
package secret

import "errors"

const (
	service = "jirastopwatch-nextgen"
	account = "jira-token"
	label   = "Jira StopWatch NextGen"
)

var (
	ErrNotFound    = errors.New("secret not found")
	ErrUnavailable = errors.New("no credential store available")
)

// Backend is the OS-specific store; tests swap it with Use.
type Backend struct {
	Get    func() (string, error)
	Set    func(string) error
	Delete func() error
}

var cur = osBackend()

func Get() (string, error) { return cur.Get() }
func Set(v string) error   { return cur.Set(v) }
func Delete() error        { return cur.Delete() }

// Use replaces the backend and returns a function that restores the previous one.
func Use(b Backend) (restore func()) {
	prev := cur
	cur = b
	return func() { cur = prev }
}

// NewMemory returns an in-process backend for tests, so they never touch the real credential store.
func NewMemory() Backend {
	var v string
	var have bool
	return Backend{
		Get: func() (string, error) {
			if !have {
				return "", ErrNotFound
			}
			return v, nil
		},
		Set:    func(s string) error { v, have = s, true; return nil },
		Delete: func() error { v, have = "", false; return nil },
	}
}
