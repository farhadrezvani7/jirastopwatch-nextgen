// Package store persists user config under the OS config dir
// (~/.config/jirastopwatch-nextgen, %AppData%\\jirastopwatch-nextgen, ~/Library/Application Support/jirastopwatch-nextgen).
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"jirastopwatch-nextgen/internal/secret"

	_ "time/tzdata" // Windows has no system zoneinfo; embed it so named zones always load
)

type Config struct {
	Site        string `json:"site,omitempty"`
	Mode        string `json:"mode,omitempty"`  // cloud | pat | basic
	Email       string `json:"email,omitempty"` // email or username
	Token       string `json:"token,omitempty"` // API token / PAT / password; only if "remember me". Lives in the OS credential store; this field is the file fallback when none is available
	PauseOthers bool   `json:"pauseOthers"`

	LockMode         string `json:"lockMode,omitempty"` // "" (ignore) | pause | resume: what to do with running timers when the session locks
	ExitMode         string `json:"exitMode,omitempty"` // "" (keep running) | pause | reset: what to do with timers when the app closes
	NoUpdateCheck    bool   `json:"noUpdateCheck,omitempty"`
	Theme            string `json:"theme,omitempty"`            // light | dark; empty = follow the OS
	Lang             string `json:"lang,omitempty"`             // UI language code; empty = browser language
	Country          string `json:"country,omitempty"`          // ISO 3166 code picked for the time zone; empty = system
	Timezone         string `json:"timezone,omitempty"`         // IANA zone used for worklog start times; empty = system
	CommentMode      string `json:"commentMode,omitempty"`      // worklog (default) | comment | both
	TimeMode         string `json:"timeMode,omitempty"`         // "" = send the exact moment (standard) | server = write the local clock time on the Jira server's clock
	TimeAck          string `json:"timeAck,omitempty"`          // which clock-difference notice the user has already answered
	StartTransitions string `json:"startTransitions,omitempty"` // status names to move an issue to when its timer starts, one per line
	Shadows          bool   `json:"shadows,omitempty"`          // draw drop shadows (off by default)
	FontSize         int    `json:"fontSize,omitempty"`         // base font size in px (10-16); 0 = the default, 13
	AsideClosed      bool   `json:"asideClosed,omitempty"`      // the issue panel is collapsed (wide windows)
}

// Location returns the configured time zone, or the system zone.
func (c Config) Location() *time.Location {
	if c.Timezone != "" {
		if l, err := time.LoadLocation(c.Timezone); err == nil {
			return l
		}
	}
	return time.Local
}

func Dir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, ".config")
	}
	return filepath.Join(d, "jirastopwatch-nextgen")
}

// keychain mirrors what is in the OS credential store so Save only talks to it when the token changes.
var keychain struct {
	mu    sync.Mutex
	known bool
	val   string
}

func Load() Config {
	c := Config{PauseOthers: true}
	if b, err := os.ReadFile(filepath.Join(Dir(), "config.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	keychain.mu.Lock()
	defer keychain.mu.Unlock()
	if c.Token == "" {
		v, err := secret.Get()
		keychain.known, keychain.val = err == nil || err == secret.ErrNotFound, v
		c.Token = v
		return c
	}
	// A token found in the file (older versions, or no credential store): move it into the store if possible.
	if secret.Set(c.Token) == nil {
		keychain.known, keychain.val = true, c.Token
		_ = write(withoutToken(c))
	}
	return c
}

func withoutToken(c Config) Config { c.Token = ""; return c }

// Save writes the config. The token goes to the OS credential store when one
// works; otherwise it stays in the (owner-only) config file.
func Save(c Config) error {
	keychain.mu.Lock()
	defer keychain.mu.Unlock()
	disk := c
	switch {
	case c.Token == "":
		if !keychain.known || keychain.val != "" {
			if err := secret.Delete(); err == nil {
				keychain.known, keychain.val = true, ""
			}
		}
	case keychain.known && keychain.val == c.Token:
		disk.Token = ""
	default:
		if err := secret.Set(c.Token); err == nil {
			keychain.known, keychain.val = true, c.Token
			disk.Token = ""
		}
	}
	return write(disk)
}

func write(c Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", " ")
	p := filepath.Join(Dir(), "config.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
