package secret

import (
	"os"
	"testing"
)

// Talks to the machine's real credential store, so it only runs on request:
//
//	JSW_REAL_KEYCHAIN=1 go test ./internal/secret
func TestRealCredentialStoreRoundTrip(t *testing.T) {
	if os.Getenv("JSW_REAL_KEYCHAIN") == "" {
		t.Skip("set JSW_REAL_KEYCHAIN=1 to test the real credential store")
	}
	_ = Delete()
	if _, err := Get(); err != ErrNotFound {
		t.Fatalf("Get on empty store = %v, want ErrNotFound", err)
	}
	for _, v := range []string{"abc123", "päss wörd/with spaces & ünïcode ✓", "x"} {
		if err := Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
		got, err := Get()
		if err != nil || got != v {
			t.Fatalf("Get after Set(%q) = %q, %v", v, got, err)
		}
	}
	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(); err != ErrNotFound {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("Delete of a missing secret should succeed: %v", err)
	}
}
