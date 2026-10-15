package store

import (
	"testing"
	"time"
)

func TestLocationIranHasHalfHourOffset(t *testing.T) {
	_, off := time.Date(2026, 10, 15, 12, 0, 0, 0, (Config{Timezone: "Asia/Tehran"}).Location()).Zone()
	if off != 3*3600+1800 {
		t.Errorf("Tehran offset = %d", off)
	}
	if (Config{Timezone: "Nope/Nope"}).Location() == nil {
		t.Error("invalid zone must fall back")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	want := Config{Site: "https://x", Email: "e", Timezone: "Asia/Tehran", Country: "IR", Lang: "fa", CommentMode: "both", PauseOthers: false}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
	if Load().Token != "" {
		t.Error("token must not be stored unless remembered")
	}
}
