package secret

import "strings"

// Uses secret-tool (package libsecret-tools), which talks to GNOME Keyring / KWallet.
func osBackend() Backend {
	attrs := []string{"service", service, "account", account}
	return Backend{
		Get: func() (string, error) {
			out, err := run("", "secret-tool", append([]string{"lookup"}, attrs...)...)
			if err != nil {
				return "", err
			}
			if out == "" {
				return "", ErrNotFound
			}
			return strings.TrimRight(out, "\n"), nil
		},
		Set: func(v string) error {
			_, err := run(v, "secret-tool", append([]string{"store", "--label=" + label}, attrs...)...)
			if err == ErrNotFound { // non-zero exit while storing: the store is unusable
				return ErrUnavailable
			}
			return err
		},
		Delete: func() error {
			_, err := run("", "secret-tool", append([]string{"clear"}, attrs...)...)
			if err == ErrNotFound {
				return nil
			}
			return err
		},
	}
}
