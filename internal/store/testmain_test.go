package store

import (
	"os"
	"testing"

	"jirastopwatch-nextgen/internal/secret"
)

// Tests must never read or write the developer's real credential store.
func TestMain(m *testing.M) {
	secret.Use(secret.NewMemory())
	os.Exit(m.Run())
}
