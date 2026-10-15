package lockwatch

import "testing"

func TestParseLockedHint(t *testing.T) {
	if !parseLockedHint("yes\n") || parseLockedHint("no\n") || parseLockedHint("") {
		t.Error("parse")
	}
}
