package lock

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func writeLock(t *testing.T, path string, info Info) {
	t.Helper()
	b, _ := json.Marshal(info)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "f.lock")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.TookOver != nil {
		t.Fatal("fresh lock should not report a takeover")
	}
	// Same live process (this one) holds it: second acquire must fail.
	var locked *ErrLocked
	if _, err := Acquire(path); !errors.As(err, &locked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := Holder(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lock file should be gone: %v", err)
	}
}

func TestAbandonedLockIsTakenOver(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("cannot spawn helper process:", err)
	}
	host, _ := os.Hostname()
	path := filepath.Join(t.TempDir(), "f.lock")
	dead := Info{PID: cmd.Process.Pid, Host: host, Started: time.Now().Add(-time.Hour)}
	writeLock(t, path, dead)

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("abandoned lock should be taken over: %v", err)
	}
	if l.TookOver == nil || l.TookOver.PID != dead.PID {
		t.Fatalf("TookOver = %+v", l.TookOver)
	}
	l.Release()
}

func TestForeignHostLockIsRespected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.lock")
	writeLock(t, path, Info{PID: 1, Host: "some-other-host", Started: time.Now()})
	var locked *ErrLocked
	if _, err := Acquire(path); !errors.As(err, &locked) {
		t.Fatalf("lock from another host must not be taken over: %v", err)
	}
	if err := ForceRelease(path); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire after force release: %v", err)
	}
	l.Release()
}

func TestReleaseDoesNotRemoveForeignLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.lock")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	writeLock(t, path, Info{PID: 1, Host: "other", Started: time.Now()})
	if err := l.Release(); err == nil {
		t.Fatal("release of a lock now owned by someone else should fail")
	}
}
