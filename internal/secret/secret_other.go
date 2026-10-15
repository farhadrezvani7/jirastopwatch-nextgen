//go:build !linux && !darwin && !windows

package secret

func osBackend() Backend {
	return Backend{
		Get:    func() (string, error) { return "", ErrUnavailable },
		Set:    func(string) error { return ErrUnavailable },
		Delete: func() error { return ErrUnavailable },
	}
}
