package app

import (
	"context"

	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/implement"
	"github.com/darkcarard/quipu-sdd/internal/lock"
	"github.com/darkcarard/quipu-sdd/internal/validate"
	"github.com/darkcarard/quipu-sdd/internal/verify"
)

// implementing runs fn on a fresh session of the feature, under its lock.
func (a *App) implementing(ctx context.Context, arg string, fn func(s *implement.Session) error) error {
	name, err := a.Resolve(arg)
	if err != nil {
		return err
	}
	who, err := a.identity(ctx)
	if err != nil {
		return err
	}
	return a.withLock(name, func() error {
		f, err := feature.Load(a.Layout, a.Config, name)
		if err != nil {
			return err
		}
		return fn(&implement.Session{F: f, Who: who, Now: a.now(), Limits: a.Config.Limits})
	})
}

// StartImplement creates or repairs the worktree and starts the implementation.
func (a *App) StartImplement(ctx context.Context, arg string) (out implement.Started, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.Start(ctx, s); return })
	return
}

// TaskNext returns the agent's next step.
func (a *App) TaskNext(ctx context.Context, arg string) (out implement.Next, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.TaskNext(ctx, s); return })
	return
}

// TaskDone verifies and checkpoints the task in progress.
func (a *App) TaskDone(ctx context.Context, arg, summary string) (out implement.Done, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.TaskDone(ctx, s, summary); return })
	return
}

// GroupVerify verifies the group in progress and squashes it into one commit.
func (a *App) GroupVerify(ctx context.Context, arg string) (out implement.Verified, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.GroupVerify(ctx, s); return })
	return
}

// Review describes the group awaiting review.
func (a *App) Review(ctx context.Context, arg string, diff bool) (out implement.Review, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.ReviewGroup(ctx, s, diff); return })
	return
}

// ApproveGroup accepts the group under review (human-only).
func (a *App) ApproveGroup(ctx context.Context, arg string) (out implement.Approved, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.ApproveGroup(ctx, s); return })
	return
}

// Revise sends the group back to the agent with feedback (human-only).
func (a *App) Revise(ctx context.Context, arg, feedback string) (out implement.GroupView, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.ReviseGroup(ctx, s, feedback); return })
	return
}

// Resume repairs the implementation after an interruption. forceUnlock first
// removes the feature lock whoever holds it.
func (a *App) Resume(ctx context.Context, arg string, discard, forceUnlock bool) (out implement.Resumed, err error) {
	if forceUnlock {
		name, err := a.Resolve(arg)
		if err != nil {
			return out, err
		}
		if err := lock.ForceRelease(a.Layout.LockFile(name)); err != nil {
			return out, err
		}
	}
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.Resume(ctx, s, discard); return })
	return
}

// Clean removes the feature's worktree (the branch is kept).
func (a *App) Clean(ctx context.Context, arg string, force bool) (msg string, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) { msg, e = implement.Clean(ctx, s, force); return })
	return
}

// Validate runs the integration tests and every sync check of an implemented
// feature and writes its report.
func (a *App) Validate(ctx context.Context, arg string) (out validate.Result, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) {
		out, e = validate.Validate(ctx, s.F, s.Who, s.Now, verify.Options{Timeout: a.Config.Limits.CommandTimeout, Tail: a.Config.Limits.OutputTail})
		return
	})
	return
}

// ReviewContext returns the brief for an agent-assisted deep review.
func (a *App) ReviewContext(ctx context.Context, arg string) (string, error) {
	f, err := a.Load(arg)
	if err != nil {
		return "", err
	}
	return validate.ReviewContext(ctx, f)
}
