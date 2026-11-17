//go:build unix

package verify

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	results, ok := Run(context.Background(), dir, 5*time.Second,
		Step{"build", "echo building && pwd"},
		Step{"lint", ""},
		Step{"test", "echo boom >&2; exit 3"},
		Step{"integration", "echo never"},
	)
	if ok || len(results) != 2 {
		t.Fatalf("ok=%v results=%+v", ok, results)
	}
	if !results[0].Passed || !strings.Contains(results[0].Output, "building") || !strings.Contains(results[0].Output, dir) {
		t.Errorf("build = %+v", results[0])
	}
	if results[1].Passed || results[1].ExitCode != 3 || !strings.Contains(results[1].Output, "boom") {
		t.Errorf("test = %+v", results[1])
	}
	s := Summary(results)
	if !strings.Contains(s, "build:      passed") || !strings.Contains(s, "test:       FAILED (exit 3)") || !strings.Contains(s, "--- output of test ---\nboom") {
		t.Errorf("summary:\n%s", s)
	}
}

func TestTimeoutKillsChildren(t *testing.T) {
	start := time.Now()
	results, ok := Run(context.Background(), t.TempDir(), 200*time.Millisecond, Step{"test", "sleep 30 & sleep 30; wait"})
	if ok || !results[0].TimedOut || time.Since(start) > 10*time.Second {
		t.Errorf("results = %+v after %s", results, time.Since(start))
	}
}

func TestTail(t *testing.T) {
	long := strings.Repeat("line\n", MaxOutput)
	if got := tail(long); len(got) > MaxOutput+40 || !strings.HasPrefix(got, "[… output truncated …]\nline") {
		t.Errorf("tail: %d bytes, %q", len(got), got[:40])
	}
}
