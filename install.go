package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed packaging/icon.png
var appIcon []byte

// installDesktop copies the running binary to ~/.local/bin and adds a launcher
// (menu entry + icon), so the app can be opened from the applications menu
// without a terminal. No root needed. Returns a message for the user.
func installDesktop(uninstall bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	bin := filepath.Join(home, ".local", "bin", "jirastopwatch-nextgen")
	desktop := filepath.Join(data, "applications", "jirastopwatch-nextgen.desktop")
	icon := filepath.Join(data, "icons", "hicolor", "512x512", "apps", "jirastopwatch-nextgen.png")

	if uninstall {
		for _, f := range []string{bin, desktop, icon} {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		return "Removed Jira StopWatch NextGen (your saved data was kept).", nil
	}

	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	if err := copyExecutable(self, bin); err != nil {
		return "", err
	}
	for _, d := range []string{filepath.Dir(desktop), filepath.Dir(icon)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(icon, appIcon, 0o644); err != nil {
		return "", err
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Jira StopWatch NextGen
Comment=Track time on Jira issues
Exec=%s
Icon=jirastopwatch-nextgen
Terminal=false
Categories=Office;ProjectManagement;
StartupWMClass=jirastopwatch-nextgen
`, bin)
	if err := os.WriteFile(desktop, []byte(entry), 0o644); err != nil {
		return "", err
	}
	return "Installed. Open \"Jira StopWatch NextGen\" from your applications menu.", nil
}

// copyExecutable writes src to dst via a temp file + rename, so it also works
// when dst is the binary that is currently running.
func copyExecutable(src, dst string) error {
	if a, b := filepath.Clean(src), filepath.Clean(dst); a == b {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".install-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
