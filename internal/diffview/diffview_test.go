//go:build unix

package diffview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

func TestFind(t *testing.T) {
	found := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, m := range names {
				if m == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	display := func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	none := func(string) string { return "" }
	tests := []struct {
		name, configured string
		env              Env
		want, why        string
	}{
		{"detects in order", "", Env{LookPath: found("codium", "cursor"), Getenv: display, GOOS: "linux"}, "/usr/bin/codium", ""},
		{"configured", "cursor", Env{LookPath: found("code", "cursor"), Getenv: none, GOOS: "linux"}, "/usr/bin/cursor", ""},
		{"configured missing", "zed", Env{LookPath: found("code"), Getenv: display, GOOS: "linux"}, "", `"zed" not found`},
		{"disabled", None, Env{LookPath: found("code"), Getenv: display, GOOS: "linux"}, "", "none"},
		{"no display", "", Env{LookPath: found("code"), Getenv: none, GOOS: "linux"}, "", "no graphical session"},
		{"vscode terminal", "", Env{LookPath: found("code"), Getenv: func(k string) string {
			if k == "VSCODE_IPC_HOOK_CLI" {
				return "/run/x.sock"
			}
			return ""
		}, GOOS: "linux"}, "/usr/bin/code", ""},
		{"macOS", "", Env{LookPath: found("code"), Getenv: none, GOOS: "darwin"}, "/usr/bin/code", ""},
		{"nothing installed", "", Env{LookPath: found(), Getenv: display, GOOS: "linux"}, "", "review.editor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, why := Find(tt.configured, tt.env)
			if got != tt.want || (tt.why == "" && why != "") || !strings.Contains(why, tt.why) {
				t.Errorf("Find = %q, %q; want %q, %q", got, why, tt.want, tt.why)
			}
		})
	}
}

func TestOpen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := workspace.Git{Dir: dir}
	run := func(args ...string) string {
		t.Helper()
		out, err := g.Run(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	write := func(name, content string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "Ada")
	run("config", "user.email", "ada@example.com")
	run("config", "commit.gpgsign", "false")
	write("keep.go", "old\n")
	write("gone.go", "bye\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base := run("rev-parse", "HEAD")
	write("keep.go", "new\n")
	write("pkg/added.go", "hi\n")
	os.Remove(filepath.Join(dir, "gone.go"))

	// A fake editor that records each invocation.
	logf := filepath.Join(t.TempDir(), "calls")
	editor := filepath.Join(t.TempDir(), "code")
	os.WriteFile(editor, []byte("#!/bin/sh\necho \"$@\" >> "+logf+"\n"), 0o755)

	wt := workspace.Open(dir, "main")
	scratch := filepath.Join(t.TempDir(), "scratch")
	res, err := Open(ctx, Request{Editor: editor, Worktree: wt, Base: base, Dir: scratch})
	if err != nil || res.Opened != 3 || res.Total != 3 {
		t.Fatalf("Open (worktree) = %+v, %v", res, err)
	}
	calls, _ := os.ReadFile(logf)
	want := []string{
		"--diff " + filepath.Join(scratch, "old", "gone.go") + " " + filepath.Join(scratch, "new", "gone.go"),
		"--diff " + filepath.Join(scratch, "old", "keep.go") + " " + filepath.Join(dir, "keep.go"),
		"--diff " + filepath.Join(scratch, "old", "pkg", "added.go") + " " + filepath.Join(dir, "pkg", "added.go"),
	}
	if got := strings.Split(strings.TrimSpace(string(calls)), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for path, content := range map[string]string{"old/keep.go": "old\n", "old/gone.go": "bye\n", "old/pkg/added.go": "", "new/gone.go": ""} {
		if b, err := os.ReadFile(filepath.Join(scratch, path)); err != nil || string(b) != content {
			t.Errorf("%s = %q, %v; want %q", path, b, err, content)
		}
	}

	// A committed group opens both sides from git.
	run("add", "-A")
	run("commit", "-q", "-m", "group")
	head := run("rev-parse", "HEAD")
	write("keep.go", "later\n") // later work must not leak into the committed view
	os.Remove(logf)
	if res, err := Open(ctx, Request{Editor: editor, Worktree: wt, Base: base, Head: head, Dir: scratch}); err != nil || res.Opened != 3 {
		t.Fatalf("Open (commit) = %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(scratch, "new", "keep.go")); string(b) != "new\n" {
		t.Errorf("new/keep.go = %q, want the committed version", b)
	}
	if _, err := os.Stat(filepath.Join(scratch, "old", "pkg")); err != nil {
		t.Errorf("scratch must be rebuilt: %v", err)
	}

	// A failing editor is reported.
	os.WriteFile(editor, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755)
	if _, err := Open(ctx, Request{Editor: editor, Worktree: wt, Base: base, Head: head, Dir: scratch}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failing editor: %v", err)
	}
}
