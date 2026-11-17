//go:build unix

package lock

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with pid exists (signal 0 probe).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
