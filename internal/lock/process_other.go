//go:build !unix

package lock

// processAlive conservatively assumes the process is alive on platforms without
// a portable liveness probe; users can recover with --force-unlock.
func processAlive(pid int) bool { return pid > 0 }
