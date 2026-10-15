package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallDesktopRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", "")

	for i := 0; i < 2; i++ { // second run must also succeed (re-install / upgrade)
		if _, err := installDesktop(false); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(home, ".local", "bin", "jirastopwatch-nextgen")
	desktop := filepath.Join(home, ".local", "share", "applications", "jirastopwatch-nextgen.desktop")
	icon := filepath.Join(home, ".local", "share", "icons", "hicolor", "512x512", "apps", "jirastopwatch-nextgen.png")
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("binary missing: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode()&0o111 == 0 { // Windows has no exec bit
		t.Fatal("binary is not executable")
	}
	b, err := os.ReadFile(desktop)
	if err != nil || !strings.Contains(string(b), "Exec="+bin+"\n") {
		t.Fatalf("bad desktop entry: %v\n%s", err, b)
	}
	if st, err := os.Stat(icon); err != nil || st.Size() == 0 {
		t.Fatalf("icon missing: %v", err)
	}

	if _, err := installDesktop(true); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{bin, desktop, icon} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed", f)
		}
	}
	if _, err := installDesktop(true); err != nil { // uninstall when nothing is installed
		t.Fatal(err)
	}
}
