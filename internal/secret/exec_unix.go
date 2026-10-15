//go:build linux || darwin

package secret

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// run executes a credential-store CLI with a time limit (a locked keyring may wait for a prompt).
func run(stdin string, name string, args ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if stdin != "" {
		cmd.Stdin = bytes.NewBufferString(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", ErrNotFound
		}
		return "", ErrUnavailable
	}
	return out.String(), nil
}
