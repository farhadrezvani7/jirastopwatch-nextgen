package lockwatch

import "testing"

func TestParseIoreg(t *testing.T) {
	if !parseIoreg(`    "CGSSessionScreenIsLocked" = Yes`) || parseIoreg(`"x" = 1`) {
		t.Error("parse")
	}
}
