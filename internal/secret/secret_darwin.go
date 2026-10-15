package secret

import "strings"

// Uses the built-in `security` tool and the login keychain.
func osBackend() Backend {
	return Backend{
		Get: func() (string, error) {
			out, err := run("", "security", "find-generic-password", "-s", service, "-a", account, "-w")
			if err != nil {
				return "", err
			}
			return strings.TrimRight(out, "\n"), nil
		},
		Set: func(v string) error {
			_, err := run("", "security", "add-generic-password", "-U", "-s", service, "-a", account, "-l", label, "-w", v)
			if err == ErrNotFound {
				return ErrUnavailable
			}
			return err
		},
		Delete: func() error {
			_, err := run("", "security", "delete-generic-password", "-s", service, "-a", account)
			if err == ErrNotFound {
				return nil
			}
			return err
		},
	}
}
