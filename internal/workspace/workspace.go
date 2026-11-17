// Package workspace manages the isolated git worktree in which a feature is
// implemented (.sdd/worktrees/<feature>, branch sdd/<feature>), including WIP
// checkpoint commits and the per-group squash with SDD trailers. Git operations
// are implemented in M5; trailer formatting is final.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNotImplemented is returned by operations scheduled for a later milestone.
var ErrNotImplemented = errors.New("workspace: not implemented")

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

// Workspace is the git worktree contract used by the orchestrator.
type Workspace interface {
	// Ensure creates the worktree for feature (or repairs a missing one) and returns its path.
	Ensure(ctx context.Context, feature string) (string, error)
	// Head returns the current commit SHA of the worktree.
	Head(ctx context.Context) (string, error)
	// Dirty reports whether the worktree has uncommitted changes.
	Dirty(ctx context.Context) (bool, error)
	// Checkpoint commits all changes as a WIP commit and returns its SHA.
	Checkpoint(ctx context.Context, message string) (string, error)
	// SquashGroup squashes everything since base into one commit and returns its SHA.
	SquashGroup(ctx context.Context, base, subject string, t Trailers) (string, error)
	// ResetTo discards changes and moves the branch to sha (used by `sdd resume --discard`).
	ResetTo(ctx context.Context, sha string) error
	// Remove deletes the worktree (`sdd clean`).
	Remove(ctx context.Context) error
}

// Git is the git-CLI implementation of Workspace.
type Git struct {
	RepoRoot     string // main repository root
	WorktreesDir string // .sdd/worktrees
	dir          string // active worktree path
}

var _ Workspace = (*Git)(nil)

func (g *Git) Ensure(ctx context.Context, feature string) (string, error) {
	return "", ErrNotImplemented
}
func (g *Git) Head(ctx context.Context) (string, error) { return "", ErrNotImplemented }
func (g *Git) Dirty(ctx context.Context) (bool, error)  { return false, ErrNotImplemented }
func (g *Git) Checkpoint(ctx context.Context, message string) (string, error) {
	return "", ErrNotImplemented
}
func (g *Git) SquashGroup(ctx context.Context, base, subject string, t Trailers) (string, error) {
	return "", ErrNotImplemented
}
func (g *Git) ResetTo(ctx context.Context, sha string) error { return ErrNotImplemented }
func (g *Git) Remove(ctx context.Context) error              { return ErrNotImplemented }
