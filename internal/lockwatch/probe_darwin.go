package lockwatch

import (
	"os/exec"
	"strings"
)

func probe() (locked, ok bool) {
	out, err := exec.Command("ioreg", "-n", "Root", "-d1").Output()
	if err != nil {
		return false, false
	}
	return parseIoreg(string(out)), true
}

func parseIoreg(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "CGSSessionScreenIsLocked") {
			return strings.Contains(line, "Yes") || strings.Contains(line, "= 1")
		}
	}
	return false
}
