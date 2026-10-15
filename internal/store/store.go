// Package store persists user config under the OS config dir
// (~/.config/jirastopwatch-nextgen, %AppData%\\jirastopwatch-nextgen, ~/Library/Application Support/jirastopwatch-nextgen).
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "time/tzdata" // Windows has no system zoneinfo; embed it so named zones always load
)

type Config struct {
	Site        string `json:"site,omitempty"`
	Mode        string `json:"mode,omitempty"`  // cloud | pat | basic
	Email       string `json:"email,omitempty"` // email or username
	Token       string `json:"token,omitempty"` // API token / PAT / password; only if "remember me"
	PauseOthers bool   `json:"pauseOthers"`

	LockMode         string `json:"lockMode,omitempty"` // "" (ignore) | pause | resume: what to do with running timers when the session locks
	ExitMode         string `json:"exitMode,omitempty"` // "" (keep running) | pause | reset: what to do with timers when the app closes
	NoUpdateCheck    bool   `json:"noUpdateCheck,omitempty"`
	Theme            string `json:"theme,omitempty"`            // light | dark; empty = follow the OS
	Lang             string `json:"lang,omitempty"`             // UI language code; empty = browser language
	Country          string `json:"country,omitempty"`          // ISO 3166 code picked for the time zone; empty = system
	Timezone         string `json:"timezone,omitempty"`         // IANA zone used for worklog start times; empty = system
	CommentMode      string `json:"commentMode,omitempty"`      // worklog (default) | comment | both
	StartTransitions string `json:"startTransitions,omitempty"` // status names to move an issue to when its timer starts, one per line
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

func Load() Config {
	c := Config{PauseOthers: true}
	if b, err := os.ReadFile(filepath.Join(Dir(), "config.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func Save(c Config) error {
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
