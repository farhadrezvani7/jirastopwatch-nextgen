//go:build !linux && !darwin && !windows

package lockwatch

func probe() (locked, ok bool) { return false, false }
