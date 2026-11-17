// Package lock provides a per-feature, cross-process lock file
// (.sdd/locks/<feature>.lock) with abandoned-lock detection.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Info is the content of a lock file.
type Info struct {
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Started time.Time `json:"started"`
}

func (i Info) String() string {
	return fmt.Sprintf("pid %d on %s since %s", i.PID, i.Host, i.Started.Format(time.RFC3339))
}

// ErrLocked is returned when another live process holds the lock.
type ErrLocked struct {
	Path   string
	Holder Info
}

func (e *ErrLocked) Error() string {
	return fmt.Sprintf("%s is held by %s (use --force-unlock if that process is gone)", e.Path, e.Holder)
}

// Lock is a held lock file.
type Lock struct {
	path string
	info Info
	// TookOver reports that an abandoned lock was replaced while acquiring.
	TookOver *Info
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }

var hostname = os.Hostname

// Acquire takes the lock at path. An existing lock is taken over only when it was
// written on this host by a process that no longer exists.
func Acquire(path string) (*Lock, error) {
	host, _ := hostname()
	me := Info{PID: os.Getpid(), Host: host, Started: time.Now().UTC()}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	var tookOver *Info
	for attempt := 0; attempt < 2; attempt++ {
		err := create(path, me)
		if err == nil {
			return &Lock{path: path, info: me, TookOver: tookOver}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		holder, rerr := read(path)
		if rerr != nil {
			return nil, fmt.Errorf("reading existing lock %s: %w", path, rerr)
		}
		if !Abandoned(holder, host) {
			return nil, &ErrLocked{Path: path, Holder: holder}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		tookOver = &holder
	}
	return nil, fmt.Errorf("could not acquire %s: contended", path)
}

// Abandoned reports whether a lock was written on host by a process that is gone.
// Locks from other hosts are never considered abandoned automatically.
func Abandoned(holder Info, host string) bool {
	return holder.Host == host && !processAlive(holder.PID)
}

// Release removes the lock if it is still owned by this lock holder.
func (l *Lock) Release() error {
	cur, err := read(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if cur.PID != l.info.PID || cur.Host != l.info.Host {
		return fmt.Errorf("lock %s is now owned by %s; not releasing", l.path, cur)
	}
	return os.Remove(l.path)
}

// ForceRelease removes a lock regardless of its owner (`--force-unlock`).
func ForceRelease(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Holder returns the current lock holder, or fs.ErrNotExist when unlocked.
func Holder(path string) (Info, error) { return read(path) }

func create(path string, info Info) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(info); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func read(path string) (Info, error) {
	var info Info
	b, err := os.ReadFile(path)
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(b, &info)
	return info, err
}
