// Package diffview opens a group's changes side by side in a VS Code-compatible
// editor (code, codium, cursor, windsurf), one `--diff <old> <new>` per file,
// so the user reviews them visually instead of reading a patch in a terminal.
package diffview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

// Editors are the commands detected when none is configured, in order.
var Editors = []string{"code", "codium", "cursor", "windsurf"}

// None disables the editor: the diff is printed instead.
const None = "none"

// MaxFiles bounds how many diff tabs one review opens.
const MaxFiles = 30

// Env is what Find consults; the zero value uses the process environment.
type Env struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	GOOS     string
}

func (e Env) lookPath(name string) (string, error) {
	if e.LookPath != nil {
		return e.LookPath(name)
	}
	return exec.LookPath(name)
}

func (e Env) getenv(key string) string {
	if e.Getenv != nil {
		return e.Getenv(key)
	}
	return os.Getenv(key)
}

func (e Env) goos() string {
	if e.GOOS != "" {
		return e.GOOS
	}
	return runtime.GOOS
}

// Find returns the editor to open diffs with, or "" and the reason there is
// none. configured is review.editor: empty detects one, None disables it.
func Find(configured string, env Env) (string, string) {
	switch configured {
	case None:
		return "", "review.editor is none"
	case "":
	default:
		path, err := env.lookPath(configured)
		if err != nil {
			return "", fmt.Sprintf("review editor %q not found", configured)
		}
		return path, ""
	}
	if !graphical(env) {
		return "", "no graphical session to open an editor in"
	}
	for _, name := range Editors {
		if path, err := env.lookPath(name); err == nil {
			return path, ""
		}
	}
	return "", "no VS Code-compatible editor (" + strings.Join(Editors, ", ") + ") on PATH; set review.editor in sdd.yaml"
}

// graphical reports whether a detected editor can show a window: always on
// macOS and Windows; elsewhere with a display, or inside a VS Code terminal
// (including remote sessions, where the CLI talks to the client window).
func graphical(env Env) bool {
	switch env.goos() {
	case "darwin", "windows":
		return true
	}
	for _, k := range []string{"DISPLAY", "WAYLAND_DISPLAY", "VSCODE_IPC_HOOK_CLI"} {
		if env.getenv(k) != "" {
			return true
		}
	}
	return false
}

// Request is one group's changes to open.
type Request struct {
	Editor   string
	Worktree *workspace.Worktree
	Base     string // the commit the group started from
	Head     string // the group's commit; empty: the worktree, whose files open in place (editable)
	Dir      string // scratch directory for the old (and committed new) versions; emptied first
}

// Result reports what Open did.
type Result struct {
	Opened int
	Total  int // changed files; above MaxFiles only the first MaxFiles are opened
}

// Open opens every changed file of the request side by side in the editor.
func Open(ctx context.Context, r Request) (Result, error) {
	changes, err := r.Worktree.Changes(ctx, r.Base, r.Head)
	if err != nil {
		return Result{}, err
	}
	out := Result{Total: len(changes)}
	if len(changes) == 0 {
		return out, nil
	}
	if err := os.RemoveAll(r.Dir); err != nil {
		return out, err
	}
	for _, c := range changes {
		if out.Opened == MaxFiles {
			break
		}
		left, err := r.version(ctx, "old", r.Base, c, c.Status == 'A')
		if err != nil {
			return out, err
		}
		var right string
		if r.Head == "" && c.Status != 'D' {
			right = filepath.Join(r.Worktree.Dir, filepath.FromSlash(c.Path))
		} else if right, err = r.version(ctx, "new", r.Head, c, c.Status == 'D'); err != nil {
			return out, err
		}
		if err := launch(ctx, r.Editor, left, right); err != nil {
			return out, err
		}
		out.Opened++
	}
	return out, nil
}

// version writes the file of c at rev (empty when absent) under Dir/side.
func (r Request) version(ctx context.Context, side, rev string, c workspace.Change, absent bool) (string, error) {
	path := filepath.Join(r.Dir, side, filepath.FromSlash(c.Path))
	var data []byte
	if !absent {
		var err error
		if data, err = r.Worktree.FileAt(ctx, rev, c.Path); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// launch opens one diff tab. The editor CLI hands the request to the running
// window and returns, so the terminal is not blocked.
func launch(ctx context.Context, editor, left, right string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, editor, "--diff", left, right)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(filepath.Base(editor) + " --diff: " + msg)
	}
	return nil
}

// ScratchDir is where a review's file versions are written: the user cache
// directory, outside the project, so nothing needs to be ignored by git.
func ScratchDir(feature, group string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "sdd", "review", feature, group)
}
