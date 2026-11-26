package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/implement"
	"github.com/darkcarard/quipu-sdd/internal/lock"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/validate"
	"github.com/darkcarard/quipu-sdd/internal/verify"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

// implementing runs fn on a fresh session of the feature, under its lock.
func (a *App) implementing(ctx context.Context, arg string, fn func(s *implement.Session) error) error {
	name, err := a.Resolve(arg)
	if err != nil {
		return err
	}
	return a.session(ctx, name, "", fn)
}

// reviewing is implementing for the review commands, whose argument may also
// be a group ID or nothing (see resolveGroup).
func (a *App) reviewing(ctx context.Context, arg string, fn func(s *implement.Session) error) error {
	name, group, err := a.resolveGroup(arg)
	if err != nil {
		return err
	}
	return a.session(ctx, name, group, fn)
}

func (a *App) session(ctx context.Context, name, group string, fn func(s *implement.Session) error) error {
	who, err := a.identity(ctx)
	if err != nil {
		return err
	}
	return a.withLock(name, func() error {
		f, err := feature.Load(a.Layout, a.Config, name)
		if err != nil {
			return err
		}
		return fn(&implement.Session{F: f, Who: who, Now: a.now(), Limits: a.Config.Limits, Group: group})
	})
}

var groupID = regexp.MustCompile(`^[Gg][0-9]+$`)

// IsGroupID reports whether arg names a group (G2) rather than a feature.
func IsGroupID(arg string) bool { return groupID.MatchString(arg) }

// resolveGroup maps the argument of `sdd review|approve|revise` to a feature
// and, for a group ID, the group: a feature name, a group ID (the feature
// being implemented whose current group it is), or nothing (the only feature
// being implemented).
func (a *App) resolveGroup(arg string) (name, group string, err error) {
	if arg != "" && !groupID.MatchString(arg) {
		name, err = a.Resolve(arg)
		return name, "", err
	}
	group = strings.ToUpper(arg)
	hits, err := a.featuresWhere(func(st state.FeatureState) bool {
		if st.Phase != workflow.Implementing {
			return false
		}
		if group == "" {
			return true
		}
		for _, g := range st.Groups {
			if g.Status != workflow.GroupApproved {
				return g.ID == group
			}
		}
		return false
	})
	switch {
	case err != nil:
		return "", "", err
	case len(hits) == 1:
		return hits[0], group, nil
	case len(hits) > 1 && group == "":
		return "", "", fmt.Errorf("several features are being implemented (%s): name the feature or its group", strings.Join(hits, ", "))
	case len(hits) > 1:
		return "", "", fmt.Errorf("%s is the current group of several features (%s): name the feature instead", group, strings.Join(hits, ", "))
	case group == "":
		return "", "", errors.New("no feature is being implemented (see `sdd status`)")
	}
	return "", "", fmt.Errorf("no feature being implemented has %s as its current group (see `sdd status`)", group)
}

// featuresWhere lists the features whose stored state satisfies ok.
func (a *App) featuresWhere(ok func(state.FeatureState) bool) ([]string, error) {
	names, err := feature.List(a.Layout)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if st, err := a.Layout.Load(n); err == nil && ok(st) {
			out = append(out, n)
		}
	}
	return out, nil
}

// commitRecords commits the feature's specs and sdd records (specs/<feature>,
// .sdd/features/<feature>) in the project checkout, and nothing else. It never
// fails the operation that called it: a problem comes back as a note.
func (a *App) commitRecords(ctx context.Context, name, subject string) (sha, note string) {
	paths := []string{"specs/" + name, ".sdd/features/" + name}
	msg := workspace.CommitMessage(subject, workspace.Trailers{Feature: name})
	sha, err := workspace.Git{Dir: a.Root}.CommitPaths(ctx, msg, paths...)
	switch {
	case errors.Is(err, workspace.ErrNotRepo):
		return "", "not a git repository: the specs and records were not committed"
	case err != nil:
		return "", fmt.Sprintf("the specs and records were not committed (%v): commit %s yourself", err, strings.Join(paths, " and "))
	}
	return sha, ""
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
	err = a.reviewing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.ReviewGroup(ctx, s, diff); return })
	return
}

// GroupReviewBrief returns the brief for an expert review of a group. With a
// feature, group (optional) must be its current group; without one, group
// finds the feature as Review does (empty: the only feature being implemented).
func (a *App) GroupReviewBrief(ctx context.Context, feat, group string) (out string, err error) {
	fn := func(s *implement.Session) (e error) { out, e = implement.ReviewBrief(ctx, s); return }
	if feat == "" {
		err = a.reviewing(ctx, group, fn)
		return
	}
	name, err := a.Resolve(feat)
	if err != nil {
		return "", err
	}
	err = a.session(ctx, name, strings.ToUpper(group), fn)
	return
}

// ApproveGroup accepts the group under review (human-only).
func (a *App) ApproveGroup(ctx context.Context, arg string) (out implement.Approved, err error) {
	err = a.reviewing(ctx, arg, func(s *implement.Session) (e error) { out, e = implement.ApproveGroup(ctx, s); return })
	return
}

// Revised is the result of Revise.
type Revised struct {
	Feature string
	implement.GroupView
}

// Revise sends the group back to the agent with feedback (human-only).
func (a *App) Revise(ctx context.Context, arg, feedback string) (out Revised, err error) {
	err = a.reviewing(ctx, arg, func(s *implement.Session) (e error) {
		out.Feature = s.F.Name
		out.GroupView, e = implement.ReviseGroup(ctx, s, feedback)
		return
	})
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
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) {
		if msg, e = implement.Clean(ctx, s, force); e == nil && s.F.Merged() {
			sha, note := a.commitRecords(ctx, s.F.Name, fmt.Sprintf("sdd(%s): records of the worktree cleanup", s.F.Name))
			msg += "\n" + strings.TrimSuffix(FormatRecords(sha, note), "\n")
		}
		return
	})
	return
}

// Merged is the result of Merge.
type Merged struct {
	implement.Merged
	Records string // the commit of the feature's records, if one was made
	Note    string // why the records were not committed
}

// Merge merges a validated feature into the branch checked out in the project,
// removes its worktree and commits its records (human-only). Without an
// argument it merges the only validated feature not merged yet.
func (a *App) Merge(ctx context.Context, arg string) (out Merged, err error) {
	name := arg
	if arg == "" {
		hits, err := a.featuresWhere(func(st state.FeatureState) bool { return st.Phase == workflow.Validated && st.Merged == nil })
		switch {
		case err != nil:
			return out, err
		case len(hits) == 0:
			return out, errors.New("no validated feature is waiting to be merged (see `sdd status`)")
		case len(hits) > 1:
			return out, fmt.Errorf("several features are validated (%s): name the one to merge", strings.Join(hits, ", "))
		}
		name = hits[0]
	} else if name, err = a.Resolve(arg); err != nil {
		return out, err
	}
	err = a.session(ctx, name, "", func(s *implement.Session) (e error) {
		if out.Merged, e = implement.Merge(ctx, s); e != nil {
			return e
		}
		out.Records, out.Note = a.commitRecords(ctx, name, fmt.Sprintf("sdd(%s): records of the merge into %s", name, out.Into))
		return nil
	})
	return
}

// Validate runs the integration tests and every sync check of an implemented
// feature and writes its report.
func (a *App) Validate(ctx context.Context, arg string) (out Validation, err error) {
	err = a.implementing(ctx, arg, func(s *implement.Session) (e error) {
		out.Result, e = validate.Validate(ctx, s.F, s.Who, s.Now, verify.Options{Timeout: a.Config.Limits.CommandTimeout, Tail: a.Config.Limits.OutputTail})
		if e == nil && out.Merged {
			// Nothing else will commit the records of a merged feature.
			out.Records, out.Note = a.commitRecords(ctx, s.F.Name, fmt.Sprintf("sdd(%s): records of a validation after the merge", s.F.Name))
		}
		return
	})
	return
}

// Validation is the result of Validate.
type Validation struct {
	validate.Result
	Records string // the commit of a merged feature's records, if one was made
	Note    string // why they were not committed
}

// ReviewContext returns the brief for an agent-assisted deep review.
func (a *App) ReviewContext(ctx context.Context, arg string) (string, error) {
	f, err := a.Load(arg)
	if err != nil {
		return "", err
	}
	return validate.ReviewContext(ctx, f)
}
