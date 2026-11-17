package implement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/journal"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

// Review is what the user sees before approving a group.
type Review struct {
	Group    GroupView
	Worktree string // absolute path
	DiffStat string
	Diff     string // only when requested
	Next     string
}

// ReviewGroup describes the group awaiting review (or, if none, the open one).
func ReviewGroup(ctx context.Context, s *Session, withDiff bool) (Review, error) {
	f := s.F
	if f.Phase != workflow.Implementing {
		return Review{}, s.phaseError()
	}
	g := s.active()
	if g == nil {
		return Review{}, errors.New("every group is approved: nothing to review")
	}
	if g.Status == workflow.GroupPending || g.BaseSHA == "" {
		return Review{}, fmt.Errorf("%s has not started yet", g.ID)
	}
	wt, err := s.worktree(ctx)
	if err != nil {
		return Review{}, err
	}
	out := Review{Group: s.view(g), Worktree: wt.Dir}
	if out.DiffStat, err = wt.DiffStat(ctx, g.BaseSHA); err != nil {
		return out, err
	}
	if withDiff {
		if out.Diff, err = wt.Diff(ctx, g.BaseSHA); err != nil {
			return out, err
		}
	}
	switch g.Status {
	case workflow.GroupAwaitingReview:
		out.Next = fmt.Sprintf("`sdd approve %s` to accept %s, or `sdd revise %s -m \"<what to change>\"`", f.Name, g.ID, f.Name)
	case workflow.GroupFailed:
		out.Next = fmt.Sprintf("%s failed verification: give the agent guidance with `sdd revise %s -m \"<guidance>\"`", g.ID, f.Name)
	default:
		out.Next = fmt.Sprintf("%s is still in progress; it can be reviewed once the agent has verified it", g.ID)
	}
	return out, nil
}

func (s *Session) human() error {
	if s.Who.Agent != "" {
		return ErrHumanOnly
	}
	return nil
}

// Approved is the result of ApproveGroup.
type Approved struct {
	Group string
	Last  bool // every group is now approved
	Next  string
}

// ApproveGroup accepts the group awaiting review. Human-only.
func ApproveGroup(ctx context.Context, s *Session) (Approved, error) {
	if err := s.human(); err != nil {
		return Approved{}, err
	}
	f := s.F
	if f.Phase != workflow.Implementing {
		return Approved{}, s.phaseError()
	}
	g := s.active()
	if g == nil || g.Status != workflow.GroupAwaitingReview {
		return Approved{}, errors.New("no group is awaiting review")
	}
	g.Status = workflow.GroupApproved
	f.State.Current = state.Cursor{}
	_ = s.record(journal.Event{Action: "group.approved", Status: journal.Info, IDs: []string{g.ID},
		Detail: fmt.Sprintf("%s approved by %s", short(g.CommitSHA), s.Who)})
	out := Approved{Group: g.ID}
	if s.active() == nil {
		out.Last = true
		f.SetPhase(workflow.Implemented)
		_ = s.record(journal.Event{Action: "implement.done", Status: journal.Info, Detail: "every group approved"})
	}
	if err := s.save(); err != nil {
		return out, err
	}
	out.Next = f.Next()
	return out, nil
}

// ReviseGroup sends the group under review (or a failed group) back to the
// agent with the user's feedback, resetting its fix attempts. Human-only.
func ReviseGroup(ctx context.Context, s *Session, feedback string) (GroupView, error) {
	if err := s.human(); err != nil {
		return GroupView{}, err
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return GroupView{}, errors.New("feedback is required: say what the agent should change")
	}
	f := s.F
	if f.Phase != workflow.Implementing {
		return GroupView{}, s.phaseError()
	}
	g := s.active()
	if g == nil || (g.Status != workflow.GroupAwaitingReview && g.Status != workflow.GroupFailed) {
		return GroupView{}, errors.New("no group is awaiting review or failed")
	}
	g.Status, g.Feedback, g.Attempts = workflow.GroupInProgress, feedback, 0
	g.Revisions++
	f.State.Current = state.Cursor{Group: g.ID}
	_ = s.record(journal.Event{Action: "group.revise", Status: journal.Info, IDs: []string{g.ID}, Detail: feedback})
	return s.view(g), s.save()
}

// Resumed is the result of Resume.
type Resumed struct {
	Reconciled []string // interrupted actions closed
	Recreated  bool
	Discarded  []string // uncommitted files thrown away
	Kept       []string // uncommitted files kept for the agent
	Warnings   []string
	Next       string
}

// Resume repairs the implementation after an interruption: it closes
// journal actions that never finished, recreates a missing worktree and either
// keeps the interrupted task's uncommitted changes (the default; task_next
// hands them to the agent) or discards them back to the last checkpoint.
func Resume(ctx context.Context, s *Session, discard bool) (Resumed, error) {
	f := s.F
	var out Resumed
	events, err := journal.Read(f.Layout.JournalFile(f.Name))
	if err != nil {
		return out, err
	}
	if open := journal.Interrupted(events); len(open) > 0 {
		jr, err := f.Journal()
		if err != nil {
			return out, err
		}
		for _, ev := range open {
			if _, err := jr.End(ev, journal.Failed, journal.Event{Actor: s.Who.Actor(), Detail: "interrupted; closed by sdd resume"}); err != nil {
				return out, err
			}
			out.Reconciled = append(out.Reconciled, fmt.Sprintf("#%d %s", ev.Seq, ev.Action))
		}
		_ = f.RenderLog()
	}
	if f.Phase != workflow.Implementing {
		out.Next = f.Next()
		return out, nil
	}
	existed := workspace.Open(s.worktreeDir(), workspace.Branch(f.Name)).Exists()
	wt, err := s.worktree(ctx)
	if err != nil {
		return out, err
	}
	out.Recreated = !existed
	if head, err := wt.Head(ctx); err == nil && f.State.CheckpointSHA != "" && head != f.State.CheckpointSHA {
		out.Warnings = append(out.Warnings, fmt.Sprintf("the worktree HEAD (%s) is not the last checkpoint (%s): commits were made outside sdd", short(head), short(f.State.CheckpointSHA)))
	}
	dirty, err := wt.Changed(ctx)
	if err != nil {
		return out, err
	}
	if len(dirty) > 0 {
		if discard {
			target := f.State.CheckpointSHA
			if target == "" {
				if g := s.active(); g != nil {
					target = g.BaseSHA
				}
			}
			if target == "" {
				return out, errors.New("no checkpoint to reset to")
			}
			if err := wt.Reset(ctx, target); err != nil {
				return out, err
			}
			out.Discarded = dirty
		} else {
			out.Kept = dirty
		}
	}
	_ = s.record(journal.Event{Action: "implement.resume", Status: journal.Info,
		Detail: fmt.Sprintf("reconciled %d, discarded %d, kept %d", len(out.Reconciled), len(out.Discarded), len(out.Kept))})
	if err := s.save(); err != nil {
		return out, err
	}
	out.Next = f.Next()
	return out, nil
}

// Clean removes the feature's worktree; the branch and its commits are kept.
// Uncommitted changes block it unless force is set.
func Clean(ctx context.Context, s *Session, force bool) (string, error) {
	f := s.F
	wt := workspace.Open(s.worktreeDir(), workspace.Branch(f.Name))
	if !wt.Exists() {
		return "", fmt.Errorf("there is no worktree at %s", s.worktreeDir())
	}
	if !force {
		dirty, err := wt.Changed(ctx)
		if err != nil {
			return "", err
		}
		if len(dirty) > 0 {
			return "", fmt.Errorf("the worktree has uncommitted changes (%s): commit them through the workflow or use --force", strings.Join(dirty, ", "))
		}
	}
	if err := workspace.Remove(ctx, f.Layout.Root, wt.Dir, force); err != nil {
		return "", err
	}
	_ = s.record(journal.Event{Action: "worktree.clean", Status: journal.Info, Detail: "removed " + f.State.Worktree})
	return fmt.Sprintf("removed %s; branch %s is kept", f.State.Worktree, wt.Branch), nil
}
