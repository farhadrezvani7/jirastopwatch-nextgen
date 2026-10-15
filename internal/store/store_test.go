package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jirastopwatch-nextgen/internal/secret"
)

func resetKeychain() { keychain.known, keychain.val = false, "" }

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

func TestTokenGoesToCredentialStoreNotFile(t *testing.T) {
	resetKeychain()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	mem := secret.NewMemory()
	defer secret.Use(mem)()

	if err := Save(Config{Site: "x", Token: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(Dir(), "config.json"))
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("token leaked into the config file: %s", raw)
	}
	if got := Load(); got.Token != "s3cret" || got.Site != "x" {
		t.Fatalf("Load() = %+v", got)
	}

	// Signing out removes it everywhere.
	if err := Save(Config{Site: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Get(); err != secret.ErrNotFound {
		t.Errorf("token still in the credential store: %v", err)
	}
	if got := Load(); got.Token != "" {
		t.Errorf("token survived sign-out: %q", got.Token)
	}
}

func TestLegacyFileTokenIsMigrated(t *testing.T) {
	resetKeychain()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	defer secret.Use(secret.NewMemory())()
	_ = os.MkdirAll(Dir(), 0o700)
	_ = os.WriteFile(filepath.Join(Dir(), "config.json"), []byte(`{"site":"x","token":"old"}`), 0o600)

	if got := Load(); got.Token != "old" {
		t.Fatalf("legacy token not returned: %+v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(Dir(), "config.json"))
	if strings.Contains(string(raw), "old") {
		t.Errorf("legacy token left in the file: %s", raw)
	}
	if v, _ := secret.Get(); v != "old" {
		t.Errorf("legacy token not moved to the credential store: %q", v)
	}
}

func TestFileFallbackWhenNoCredentialStore(t *testing.T) {
	resetKeychain()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	defer secret.Use(secret.Backend{
		Get:    func() (string, error) { return "", secret.ErrUnavailable },
		Set:    func(string) error { return secret.ErrUnavailable },
		Delete: func() error { return secret.ErrUnavailable },
	})()
	if err := Save(Config{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Token != "tok" {
		t.Fatalf("token lost without a credential store: %+v", got)
	}
}
