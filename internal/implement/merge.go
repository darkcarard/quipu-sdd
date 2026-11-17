package implement

import (
	"context"
	"fmt"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/journal"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

// Merged is the result of Merge.
type Merged struct {
	Branch  string // sdd/<feature>
	Into    string // the branch of the project checkout
	SHA     string // its HEAD after the merge
	FF      bool
	Removed bool   // the worktree was removed
	Warning string // why the worktree was kept, if it was
	Next    string
}

// Merge merges a validated feature's branch into the branch checked out in the
// project (not the worktree, wherever the command runs from): a fast-forward
// when possible, otherwise a merge commit; a conflicting merge is aborted.
// Then it removes the worktree (the branch is kept). Human-only.
func Merge(ctx context.Context, s *Session) (Merged, error) {
	if err := s.human(); err != nil {
		return Merged{}, err
	}
	f := s.F
	out := Merged{Branch: workspace.Branch(f.Name)}
	if f.State.Merged != nil {
		return out, fmt.Errorf("%s was already merged into %s (%s)", f.Name, f.State.Merged.Into, short(f.State.Merged.SHA))
	}
	if f.Phase != workflow.Validated {
		return out, fmt.Errorf("%s is in phase %s: only a validated feature can be merged (next: %s)", f.Name, f.Phase, f.Next())
	}
	repo := workspace.Git{Dir: f.Layout.Root}
	into, err := repo.CurrentBranch(ctx)
	if err != nil {
		return out, err
	}
	out.Into = into
	head, err := repo.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+out.Branch)
	if err != nil {
		return out, fmt.Errorf("branch %s not found", out.Branch)
	}
	if v := f.State.ValidatedSHA; v != "" && v != head {
		return out, fmt.Errorf("%s moved since it was validated (%s, now %s): run `sdd validate %s` again", out.Branch, short(v), short(head), f.Name)
	}
	wt := workspace.Open(s.worktreeDir(), out.Branch)
	if wt.Exists() {
		dirty, err := wt.Changed(ctx)
		if err != nil {
			return out, err
		}
		if len(dirty) > 0 {
			return out, fmt.Errorf("the worktree has uncommitted changes (%s): they are not part of the validated branch; remove them or use `sdd clean %s --force`", strings.Join(dirty, ", "), f.Name)
		}
	}

	msg := fmt.Sprintf("Merge %s: %s", out.Branch, f.Title())
	if out.FF, err = repo.Merge(ctx, out.Branch, msg); err != nil {
		return out, fmt.Errorf("could not merge %s into %s, nothing was changed: %w\nResolve it by hand (e.g. `git -C %s merge %s`), then run `sdd clean %s`", out.Branch, into, err, f.Layout.Root, out.Branch, f.Name)
	}
	if out.SHA, err = repo.Run(ctx, "rev-parse", "HEAD"); err != nil {
		return out, err
	}
	how := "merge commit"
	if out.FF {
		how = "fast-forward"
	}
	f.State.Merged = &state.Merge{Into: into, SHA: out.SHA, FF: out.FF, By: s.Who.Actor(), At: s.Now}
	_ = f.Record(journal.Event{Actor: s.Who.Actor(), RunID: f.State.RunID, Phase: string(f.Phase), Action: "feature.merge",
		Status: journal.Info, Detail: fmt.Sprintf("%s merged into %s at %s (%s)", out.Branch, into, short(out.SHA), how)})

	if wt.Exists() {
		if err := workspace.Remove(ctx, f.Layout.Root, wt.Dir, false); err != nil {
			out.Warning = fmt.Sprintf("the worktree was kept: %v", err)
		} else {
			out.Removed = true
			_ = f.Record(journal.Event{Actor: s.Who.Actor(), RunID: f.State.RunID, Phase: string(f.Phase), Action: "worktree.clean",
				Status: journal.Info, Detail: "removed " + f.State.Worktree})
		}
	}
	if err := s.save(); err != nil {
		return out, err
	}
	out.Next = f.Next()
	return out, nil
}
