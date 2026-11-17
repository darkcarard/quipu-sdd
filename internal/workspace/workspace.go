// Package workspace manages the isolated git worktree in which a feature is
// implemented (.sdd/worktrees/<feature>, branch sdd/<feature>): creating and
// repairing it, WIP checkpoint commits, the per-group squash with SDD
// trailers, diffs and resets. It drives the git CLI.
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Trailers are the SDD commit trailers that link a commit to the spec chain.
type Trailers struct {
	Feature string
	Group   string
	Tasks   []string
	Covers  []string
	RunID   string
}

// String formats the trailers block appended to a group commit message.
func (t Trailers) String() string {
	var sb strings.Builder
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&sb, "%s: %s\n", k, v)
		}
	}
	line("SDD-Feature", t.Feature)
	line("SDD-Group", t.Group)
	line("SDD-Tasks", strings.Join(t.Tasks, ", "))
	line("SDD-Covers", strings.Join(t.Covers, ", "))
	line("SDD-Run", t.RunID)
	return sb.String()
}

// CommitMessage builds a group commit message: subject, blank line, trailers.
func CommitMessage(subject string, t Trailers) string {
	return strings.TrimSpace(subject) + "\n\n" + t.String()
}

// Branch returns the private branch of a feature.
func Branch(feature string) string { return "sdd/" + feature }

// Git runs git commands in Dir.
type Git struct{ Dir string }

// Run executes git with args and returns its trimmed stdout.
func (g Git) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Worktree is a feature's worktree.
type Worktree struct {
	Dir    string
	Branch string
	git    Git
}

// ErrNoCommits is returned when the repository has no commit to branch from.
var ErrNoCommits = errors.New("the repository has no commits yet: commit your project (including specs/ and sdd.yaml) before `sdd implement`")

// Open returns the worktree at dir without checking it.
func Open(dir, branch string) *Worktree {
	return &Worktree{Dir: dir, Branch: branch, git: Git{Dir: dir}}
}

// Ensure creates the worktree of feature at dir on its branch, or repairs it:
// an existing healthy worktree is reused, a missing one is recreated from the
// branch (or, the first time, from the repository's HEAD). It reports whether
// the worktree was (re)created.
func Ensure(ctx context.Context, repoRoot, dir, feature string) (*Worktree, bool, error) {
	repo := Git{Dir: repoRoot}
	branch := Branch(feature)
	wt := Open(dir, branch)
	if top, err := wt.git.Run(ctx, "rev-parse", "--show-toplevel"); err == nil && sameDir(top, dir) {
		cur, err := wt.git.Run(ctx, "symbolic-ref", "--short", "HEAD")
		if err != nil || cur != branch {
			return nil, false, fmt.Errorf("worktree %s is not on branch %s (found %q): fix it or remove it with `sdd clean`", dir, branch, cur)
		}
		return wt, false, nil
	}
	if _, err := repo.Run(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		if _, gerr := repo.Run(ctx, "rev-parse", "--git-dir"); gerr != nil {
			return nil, false, fmt.Errorf("%s is not a git repository: run `git init` and commit your project first", repoRoot)
		}
		return nil, false, ErrNoCommits
	}
	_, _ = repo.Run(ctx, "worktree", "prune") // forget worktrees whose directory is gone
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, false, err
	}
	var err error
	if _, berr := repo.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); berr == nil {
		_, err = repo.Run(ctx, "worktree", "add", dir, branch)
	} else {
		_, err = repo.Run(ctx, "worktree", "add", "-b", branch, dir, "HEAD")
	}
	if err != nil {
		return nil, false, err
	}
	return wt, true, nil
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && filepath.Clean(ra) == filepath.Clean(rb)
}

// Exists reports whether the worktree directory exists.
func (w *Worktree) Exists() bool {
	_, err := os.Stat(w.Dir)
	return err == nil
}

// Head returns the SHA of the worktree's HEAD.
func (w *Worktree) Head(ctx context.Context) (string, error) {
	return w.git.Run(ctx, "rev-parse", "HEAD")
}

// Changed returns the files with uncommitted changes (modified, added,
// deleted or untracked), relative to the worktree root, sorted.
func (w *Worktree) Changed(ctx context.Context) ([]string, error) {
	out, err := w.git.Run(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var files []string
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		status, path := e[:2], e[3:]
		if status[0] == 'R' || status[0] == 'C' { // rename/copy: the source path follows
			i++
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

// Checkpoint stages every change and commits it; it returns the new HEAD.
// With no changes it makes no commit and returns the current HEAD.
func (w *Worktree) Checkpoint(ctx context.Context, message string) (string, error) {
	changed, err := w.Changed(ctx)
	if err != nil {
		return "", err
	}
	if len(changed) == 0 {
		return w.Head(ctx)
	}
	if _, err := w.git.Run(ctx, "add", "--all"); err != nil {
		return "", err
	}
	if _, err := w.git.Run(ctx, "commit", "--quiet", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	return w.Head(ctx)
}

// Squash stages every change, then replaces all commits after base with one
// commit carrying message, and returns its SHA.
func (w *Worktree) Squash(ctx context.Context, base, message string) (string, error) {
	if _, err := w.git.Run(ctx, "add", "--all"); err != nil {
		return "", err
	}
	if _, err := w.git.Run(ctx, "reset", "--soft", base); err != nil {
		return "", err
	}
	if _, err := w.git.Run(ctx, "commit", "--quiet", "--no-verify", "--allow-empty", "-m", message); err != nil {
		return "", err
	}
	return w.Head(ctx)
}

// DiffStat summarizes the changes from base to the working tree.
func (w *Worktree) DiffStat(ctx context.Context, base string) (string, error) {
	if _, err := w.git.Run(ctx, "add", "--all", "--intent-to-add"); err != nil {
		return "", err
	}
	return w.git.Run(ctx, "diff", "--stat", base)
}

// Diff returns the full diff from base to the working tree.
func (w *Worktree) Diff(ctx context.Context, base string) (string, error) {
	if _, err := w.git.Run(ctx, "add", "--all", "--intent-to-add"); err != nil {
		return "", err
	}
	return w.git.Run(ctx, "diff", base)
}

// Reset discards every uncommitted change and moves the branch to sha.
func (w *Worktree) Reset(ctx context.Context, sha string) error {
	if _, err := w.git.Run(ctx, "reset", "--hard", sha); err != nil {
		return err
	}
	_, err := w.git.Run(ctx, "clean", "-fd")
	return err
}

// Remove deletes the worktree directory; the branch is kept.
func Remove(ctx context.Context, repoRoot, dir string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := Git{Dir: repoRoot}.Run(ctx, append(args, dir)...)
	return err
}

// Commit is one commit of the branch with its SDD trailers.
type Commit struct {
	SHA      string
	Subject  string
	Trailers map[string]string
}

// Log returns the commits reachable from HEAD but not from base, newest first.
func (w *Worktree) Log(ctx context.Context, base string) ([]Commit, error) {
	return w.git.Log(ctx, base+"..HEAD")
}

// Log returns the commits of a revision range (e.g. "base..branch"), newest first.
func (g Git) Log(ctx context.Context, revRange string) ([]Commit, error) {
	const sep = "\x1e"
	out, err := g.Run(ctx, "log", "--format=%H%x1f%s%x1f%(trailers:only,unfold)"+sep, revRange)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, sep) {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 3)
		c := Commit{SHA: parts[0], Trailers: map[string]string{}}
		if len(parts) > 1 {
			c.Subject = parts[1]
		}
		if len(parts) > 2 {
			for _, line := range strings.Split(parts[2], "\n") {
				if k, v, ok := strings.Cut(line, ": "); ok {
					c.Trailers[k] = strings.TrimSpace(v)
				}
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// ErrNotRepo is returned when a directory is not inside a git work tree.
var ErrNotRepo = errors.New("not a git repository")

// CommitPaths commits the current content of paths (relative to Dir; missing
// ones are skipped) and nothing else: changes the user staged elsewhere stay
// staged. It returns the new commit's SHA, or "" when the paths had nothing to commit.
func (g Git) CommitPaths(ctx context.Context, message string, paths ...string) (string, error) {
	if _, err := g.Run(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", ErrNotRepo
	}
	var present []string
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(g.Dir, filepath.FromSlash(p))); err == nil {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return "", nil
	}
	if _, err := g.Run(ctx, append([]string{"add", "--all", "--"}, present...)...); err != nil {
		return "", err
	}
	if _, err := g.Run(ctx, append([]string{"diff", "--cached", "--quiet", "--"}, present...)...); err == nil {
		return "", nil // nothing staged under these paths
	}
	if _, err := g.Run(ctx, append([]string{"commit", "--quiet", "--no-verify", "-m", message, "--only", "--"}, present...)...); err != nil {
		return "", err
	}
	return g.Run(ctx, "rev-parse", "HEAD")
}

// CurrentBranch returns the branch checked out in Dir; it fails on a detached HEAD.
func (g Git) CurrentBranch(ctx context.Context) (string, error) {
	b, err := g.Run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%s is on a detached HEAD: check out the branch to work on", g.Dir)
	}
	return b, nil
}

// Merge merges branch into the branch checked out in Dir: a fast-forward when
// possible, otherwise a merge commit with message. A merge that stops on
// conflicts is aborted, leaving the checkout as it was. It reports whether it
// fast-forwarded.
func (g Git) Merge(ctx context.Context, branch, message string) (bool, error) {
	if _, err := g.Run(ctx, "merge", "--ff-only", "--quiet", branch); err == nil {
		return true, nil
	}
	if _, err := g.Run(ctx, "merge", "--no-ff", "--no-edit", "--quiet", "-m", message, branch); err != nil {
		_, _ = g.Run(ctx, "merge", "--abort")
		return false, err
	}
	return false, nil
}
