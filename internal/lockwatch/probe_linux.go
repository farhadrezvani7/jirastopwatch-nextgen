package lockwatch

import (
	"os"
	"os/exec"
	"strings"
)

// systemd-logind keeps a LockedHint on the session; GNOME, KDE and most
// lockers set it. Machines without logind simply report "unsupported".
func probe() (locked, ok bool) {
	id := os.Getenv("XDG_SESSION_ID")
	if id == "" {
		id = "auto"
	}
	out, err := exec.Command("loginctl", "show-session", id, "-p", "LockedHint", "--value").Output()
	if err != nil {
		return false, false
	}
	return parseLockedHint(string(out)), true
}

func parseLockedHint(s string) bool { return strings.TrimSpace(s) == "yes" }
