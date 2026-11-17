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
	results, ok := Run(context.Background(), dir, Options{Timeout: 5 * time.Second},
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
	results, ok := Run(context.Background(), t.TempDir(), Options{Timeout: 200 * time.Millisecond}, Step{"test", "sleep 30 & sleep 30; wait"})
	if ok || !results[0].TimedOut || time.Since(start) > 10*time.Second {
		t.Errorf("results = %+v after %s", results, time.Since(start))
	}
}

func TestTail(t *testing.T) {
	long := strings.Repeat("line\n", DefaultTail)
	if got := tail(long, 0); len(got) > DefaultTail+40 || !strings.HasPrefix(got, "[… output truncated …]\nline") {
		t.Errorf("tail: %d bytes, %q", len(got), got[:40])
	}
	if got := tail(long, 100); len(got) > 140 {
		t.Errorf("a configured tail must be honoured: %d bytes", len(got))
	}
	results, _ := Run(context.Background(), t.TempDir(), Options{Tail: 50}, Step{"test", "seq 1 1000; exit 1"})
	if out := results[0].Output; !strings.HasSuffix(strings.TrimSpace(out), "1000") || len(out) > 100 {
		t.Errorf("tail of a failing step = %q", out)
	}
}
