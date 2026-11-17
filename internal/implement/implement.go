// Package implement runs Phase 3: it hands plan tasks to the agent one at a
// time, verifies them with the technical spec's commands in the feature
// worktree, checkpoints finished tasks, squashes each verified group into one
// commit with SDD trailers and stops for the user's review
// (docs/WORKFLOW.md, Phase 3). Every step is saved in state.json and journaled.
package implement

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/journal"
	"github.com/darkcarard/quipu-sdd/internal/plan"
	"github.com/darkcarard/quipu-sdd/internal/spec"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/verify"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

// Session is one operation on a feature's implementation, run under the feature lock.
type Session struct {
	F      *feature.Feature
	Who    feature.Identity
	Now    time.Time
	Limits config.Limits
	// Profile reads a language profile (nil: .sdd/profiles/<language>.md).
	Profile func(language string) (string, error)
}

// Errors for the operations a feature's phase does not allow.
var (
	ErrNotImplementing = errors.New("the feature is not being implemented")
	ErrHumanOnly       = feature.ErrHumanOnly
)

func (s *Session) phaseError() error {
	return fmt.Errorf("%w: it is in phase %s (next: %s)", ErrNotImplementing, s.F.Phase, s.F.Next())
}

func (s *Session) worktreeDir() string { return s.F.Layout.WorktreeDir(s.F.Name) }

// worktree returns the feature worktree, recreating it from its branch if the
// directory is gone.
func (s *Session) worktree(ctx context.Context) (*workspace.Worktree, error) {
	wt, created, err := workspace.Ensure(ctx, s.F.Layout.Root, s.worktreeDir(), s.F.Name)
	if err != nil {
		return nil, err
	}
	if created && s.F.State.Worktree != "" {
		_ = s.record(journal.Event{Action: "worktree.repair", Status: journal.Info, Detail: "recreated " + s.F.State.Worktree + " from " + wt.Branch})
	}
	return wt, nil
}

func (s *Session) record(ev journal.Event) error {
	if ev.Actor == "" {
		ev.Actor = s.Who.Actor()
	}
	ev.RunID = s.F.State.RunID
	ev.Phase = string(workflow.Implementing)
	return s.F.Record(ev)
}

func (s *Session) save() error { return s.F.SaveState() }

// group returns the state and plan entry of a group.
func (s *Session) group(id string) (*state.GroupState, plan.Group) {
	pg, _ := s.F.Plan.Group(id)
	for i := range s.F.State.Groups {
		if s.F.State.Groups[i].ID == id {
			return &s.F.State.Groups[i], pg
		}
	}
	return nil, pg
}

// syncGroups aligns the group and task states with the approved plan, keeping
// the progress of groups and tasks that still exist.
func (s *Session) syncGroups() {
	old := map[string]state.GroupState{}
	for _, g := range s.F.State.Groups {
		old[g.ID] = g
	}
	var groups []state.GroupState
	for _, pg := range s.F.Plan.Groups {
		g, ok := old[pg.ID]
		if !ok {
			g = state.GroupState{ID: pg.ID, Status: workflow.GroupPending}
		}
		tasks := map[string]state.TaskState{}
		for _, t := range g.Tasks {
			tasks[t.ID] = t
		}
		g.Tasks = nil
		for _, pt := range pg.Tasks {
			t, ok := tasks[pt.ID]
			if !ok {
				t = state.TaskState{ID: pt.ID, Status: workflow.TaskPending}
			}
			g.Tasks = append(g.Tasks, t)
		}
		groups = append(groups, g)
	}
	s.F.State.Groups = groups
}

// active returns the first group that is not approved, or nil when all are.
func (s *Session) active() *state.GroupState {
	for i := range s.F.State.Groups {
		if s.F.State.Groups[i].Status != workflow.GroupApproved {
			return &s.F.State.Groups[i]
		}
	}
	return nil
}

func newRunID(now time.Time) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// Started is the result of Start.
type Started struct {
	Worktree string // relative to the project root
	Branch   string
	Created  bool
	RunID    string
	Group    *GroupView // the group to work on, if any
	Message  string
}

// Start begins (or continues) the implementation: it creates or repairs the
// worktree, aligns the group states with the approved plan and moves the
// feature to implementing. Only the first pending group is started, and only
// when no other group is open.
func Start(ctx context.Context, s *Session) (Started, error) {
	f := s.F
	switch f.Phase {
	case workflow.PlanApproved, workflow.Implementing:
	case workflow.Implemented:
		return Started{Message: "every group is approved; next: " + f.Next()}, nil
	default:
		return Started{}, s.phaseError()
	}
	wt, created, err := workspace.Ensure(ctx, f.Layout.Root, s.worktreeDir(), f.Name)
	if err != nil {
		return Started{}, err
	}
	first := f.Phase == workflow.PlanApproved
	if first || f.State.RunID == "" {
		f.State.RunID = newRunID(s.Now)
	}
	f.State.Worktree = filepath.ToSlash(filepath.Join(".sdd", "worktrees", f.Name))
	f.State.Branch = wt.Branch
	s.syncGroups()
	f.SetPhase(workflow.Implementing)
	if first {
		_ = s.record(journal.Event{Action: "implement.start", Status: journal.Info,
			Hashes: map[string]string{"plan": f.Files[workflow.Plan].Hash},
			Detail: fmt.Sprintf("worktree %s on branch %s", f.State.Worktree, wt.Branch)})
	}
	out := Started{Worktree: f.State.Worktree, Branch: wt.Branch, Created: created, RunID: f.State.RunID}
	g, msg, err := s.openGroup(ctx, wt)
	if err != nil {
		return out, err
	}
	out.Message = msg
	if g != nil {
		v := s.view(g)
		out.Group = &v
	}
	return out, s.save()
}

// openGroup returns the group to work on, starting the next pending one when
// no other group is open; msg explains why there is none.
func (s *Session) openGroup(ctx context.Context, wt *workspace.Worktree) (*state.GroupState, string, error) {
	g := s.active()
	if g == nil {
		s.F.SetPhase(workflow.Implemented)
		_ = s.record(journal.Event{Action: "implement.done", Status: journal.Info, Detail: "every group approved"})
		return nil, "every group is approved; next: " + s.F.Next(), nil
	}
	switch g.Status {
	case workflow.GroupAwaitingReview:
		return nil, fmt.Sprintf("%s awaits the user's review: stop and ask the user to run `sdd review %s`", g.ID, s.F.Name), nil
	case workflow.GroupFailed:
		return nil, fmt.Sprintf("%s failed verification %d times: stop and ask the user to inspect it and run `sdd revise %s -m \"<guidance>\"`",
			g.ID, g.Attempts, s.F.Name), nil
	case workflow.GroupPending:
		head, err := wt.Head(ctx)
		if err != nil {
			return nil, "", err
		}
		g.Status, g.BaseSHA = workflow.GroupInProgress, head
		s.F.State.Current = state.Cursor{Group: g.ID}
		_, pg := s.group(g.ID)
		_ = s.record(journal.Event{Action: "group.start", Status: journal.Info, IDs: []string{g.ID}, Detail: pg.Title})
	}
	return g, "", nil
}

// GroupView describes a group and its progress.
type GroupView struct {
	ID, Title, Layer, Goal string
	Status                 string
	Tasks                  []TaskView
	Commit                 string
	Attempts               int
	Revisions              int
	Feedback               string
	Verification           string
}

// TaskView describes a task and its progress.
type TaskView struct {
	ID, Title, Status string
	Covers            []string
	Files             []string
	Summary           string
	Undeclared        []string
	Checkpoint        string
}

func (s *Session) view(g *state.GroupState) GroupView {
	_, pg := s.group(g.ID)
	v := GroupView{ID: g.ID, Title: pg.Title, Layer: pg.Layer, Goal: pg.Goal, Status: string(g.Status),
		Commit: g.CommitSHA, Attempts: g.Attempts, Revisions: g.Revisions, Feedback: g.Feedback, Verification: g.Verification}
	for _, t := range g.Tasks {
		pt, _, _ := s.F.Plan.Task(t.ID)
		v.Tasks = append(v.Tasks, TaskView{ID: t.ID, Title: pt.Title, Status: string(t.Status), Covers: pt.Covers,
			Files: pt.Files, Summary: t.Summary, Undeclared: t.Undeclared, Checkpoint: t.CheckpointSHA})
	}
	return v
}

// Progress returns the view of every group, for status displays.
func Progress(f *feature.Feature) []GroupView {
	if f.Plan == nil {
		return nil
	}
	s := &Session{F: f}
	var out []GroupView
	for i := range f.State.Groups {
		out = append(out, s.view(&f.State.Groups[i]))
	}
	return out
}

// Kinds of Next results.
const (
	NextTask     = "task"     // implement Task
	NextRevision = "revision" // address the user's feedback, then call group_verify
	NextVerify   = "verify"   // every task is done: call group_verify
	NextStop     = "stop"     // the user must act (review, failed group, nothing left)
)

// Next is what the agent should do now.
type Next struct {
	Kind     string
	Group    *GroupView
	Task     *Brief
	Resumed  bool     // the task was already in progress (e.g. after an interruption)
	Dirty    []string // uncommitted files in the worktree
	DiffStat string   // for revisions: the group's changes so far
	Feedback string
	Message  string
	Worktree string // absolute path
}

// Brief is everything the agent needs to implement one task.
type Brief struct {
	ID, Title, Description string
	Files                  []string
	DoneWhen, Verify       string
	Covers                 []Covered
	Language               string
	ProfilePath            string
	Profile                string // empty when the agent already got it with an earlier task
	Commands               spec.Commands
}

// Covered is one spec ID a task covers, with its text and technical mapping.
type Covered struct {
	ID        string
	Parent    string   // the FR of an AC
	Text      string   // requirement statement or criterion
	Technical []string // components (FR/NFR) or test levels (AC)
}

// TaskNext returns the next step for the agent: the task in progress (after an
// interruption), the next pending task, the user's revision feedback, the
// group verification, or a reason to stop. Starting a task or the next group
// is recorded.
func TaskNext(ctx context.Context, s *Session) (Next, error) {
	f := s.F
	if f.Phase != workflow.Implementing {
		return Next{}, s.phaseError()
	}
	wt, err := s.worktree(ctx)
	if err != nil {
		return Next{}, err
	}
	s.syncGroups()
	out := Next{Worktree: wt.Dir}
	g, msg, err := s.openGroup(ctx, wt)
	if err != nil {
		return out, err
	}
	if g == nil {
		out.Kind, out.Message = NextStop, msg
		return out, s.save()
	}
	v := s.view(g)
	out.Group = &v
	dirty, err := wt.Changed(ctx)
	if err != nil {
		return out, err
	}
	out.Dirty = dirty

	for i := range g.Tasks {
		t := &g.Tasks[i]
		switch t.Status {
		case workflow.TaskInProgress, workflow.TaskInterrupted, workflow.TaskFailed:
			t.Status = workflow.TaskInProgress
			out.Kind, out.Resumed = NextTask, true
		case workflow.TaskPending:
			t.Status = workflow.TaskInProgress
			out.Kind = NextTask
			_ = s.record(journal.Event{Action: "task.start", Status: journal.Info, IDs: []string{t.ID}})
		default:
			continue
		}
		f.State.Current = state.Cursor{Group: g.ID, Task: t.ID}
		// The language profile goes with the group's first task and with a
		// resumed task (a new session); other tasks get only its path.
		brief, err := s.brief(t.ID, i == 0 || out.Resumed)
		if err != nil {
			return out, err
		}
		out.Task = &brief
		out.Message = "implement this task in the worktree, then call task_done with a one-line summary"
		if out.Resumed {
			out.Message = "this task is already in progress: continue it, then call task_done with a one-line summary"
			if len(dirty) > 0 {
				out.Message = "this task was already in progress; the uncommitted changes listed are from that attempt: continue from them, then call task_done"
			}
		}
		return out, s.save()
	}

	f.State.Current = state.Cursor{Group: g.ID}
	if g.Feedback != "" {
		out.Kind, out.Feedback = NextRevision, g.Feedback
		out.DiffStat, _ = wt.DiffStat(ctx, g.BaseSHA)
		out.Message = "the user asked for changes to this group: address the feedback in the worktree, then call group_verify"
	} else {
		out.Kind = NextVerify
		out.Message = "every task of this group is done: call group_verify"
	}
	return out, s.save()
}

func (s *Session) brief(taskID string, withProfile bool) (Brief, error) {
	f := s.F
	pt, _, ok := f.Plan.Task(taskID)
	if !ok {
		return Brief{}, fmt.Errorf("task %s is not in the plan", taskID)
	}
	b := Brief{ID: pt.ID, Title: pt.Title, Description: pt.Description, Files: pt.Files, DoneWhen: pt.DoneWhen, Verify: pt.Verify}
	for _, id := range pt.Covers {
		c := Covered{ID: id}
		if f.Functional != nil {
			if r, ok := f.Functional.Requirement(id); ok {
				c.Text = strings.TrimSpace(strings.TrimSpace(r.Title+". ") + " " + r.Text)
			} else if ac, ok := f.Functional.Criterion(id); ok {
				c.Text, c.Parent = ac.Text, ac.RequirementID
			}
		}
		if f.Technical != nil {
			if spec.KindOf(id) == spec.KindAC {
				c.Technical = f.Technical.TestLevels(id)
			} else {
				c.Technical = f.Technical.Components(id)
			}
		}
		b.Covers = append(b.Covers, c)
	}
	if f.Technical != nil {
		b.Commands = f.Technical.Commands
		if lang := f.Technical.Language; lang != "" {
			b.Language = lang
			b.ProfilePath = filepath.ToSlash(filepath.Join(".sdd", "profiles", lang+".md"))
			read := s.Profile
			if read == nil {
				read = func(l string) (string, error) {
					data, err := os.ReadFile(filepath.Join(f.Layout.ProfilesDir(), l+".md"))
					return string(data), err
				}
			}
			if p, err := read(lang); err == nil && withProfile {
				b.Profile = p
			}
		}
	}
	return b, nil
}

// current returns the in-progress group and task.
func (s *Session) current() (*state.GroupState, *state.TaskState, error) {
	g := s.active()
	if g == nil || g.Status != workflow.GroupInProgress {
		return g, nil, nil
	}
	for i := range g.Tasks {
		if g.Tasks[i].Status == workflow.TaskInProgress || g.Tasks[i].Status == workflow.TaskInterrupted {
			return g, &g.Tasks[i], nil
		}
	}
	return g, nil, nil
}

func (s *Session) verifyOptions() verify.Options {
	return verify.Options{Timeout: s.Limits.CommandTimeout, Tail: s.Limits.OutputTail}
}

func (s *Session) steps(names ...string) []verify.Step {
	c := s.F.Technical.Commands
	all := map[string]string{"build": c.Build, "lint": c.Lint, "test": c.Test, "integration": c.Integration}
	var out []verify.Step
	for _, n := range names {
		out = append(out, verify.Step{Name: n, Command: all[n]})
	}
	return out
}

// Done is the result of TaskDone.
type Done struct {
	Task       string
	Passed     bool
	Commit     string
	Undeclared []string
	NoChanges  bool
	Results    []verify.Result
	Next       string
}

// TaskDone verifies the task in progress with the build command. On success it
// makes a WIP checkpoint commit, marks the task done and reports changed files
// the plan did not declare for it. On failure the task stays in progress.
func TaskDone(ctx context.Context, s *Session, summary string) (Done, error) {
	f := s.F
	if f.Phase != workflow.Implementing {
		return Done{}, s.phaseError()
	}
	if f.Technical == nil {
		return Done{}, errors.New("technical.md is missing")
	}
	wt, err := s.worktree(ctx)
	if err != nil {
		return Done{}, err
	}
	g, t, err := s.current()
	if err != nil {
		return Done{}, err
	}
	if t == nil {
		return Done{}, errors.New("no task is in progress: call task_next first")
	}
	pt, _, _ := f.Plan.Task(t.ID)
	out := Done{Task: t.ID}

	jr, err := f.Journal()
	if err != nil {
		return out, err
	}
	started, err := jr.Start(journal.Event{Actor: s.Who.Actor(), RunID: f.State.RunID, Phase: string(workflow.Implementing),
		Action: "task.verify", IDs: []string{t.ID}})
	if err != nil {
		return out, err
	}
	results, ok := verify.Run(ctx, wt.Dir, s.verifyOptions(), s.steps("build")...)
	out.Results, out.Passed = results, ok
	if !ok {
		t.Attempts++
		_, _ = jr.End(started, journal.Failed, journal.Event{Detail: lastLine(results)})
		_ = f.RenderLog()
		out.Next = "fix the build in the worktree, then call task_done again"
		return out, s.save()
	}

	changed, err := wt.Changed(ctx)
	if err != nil {
		return out, err
	}
	out.NoChanges = len(changed) == 0
	for _, c := range changed {
		if !slices.Contains(pt.Files, c) {
			out.Undeclared = append(out.Undeclared, c)
		}
	}
	sha, err := wt.Checkpoint(ctx, fmt.Sprintf("wip(%s): %s %s", f.Name, t.ID, pt.Title))
	if err != nil {
		_, _ = jr.End(started, journal.Failed, journal.Event{Detail: err.Error()})
		return out, err
	}
	out.Commit = sha
	t.Status, t.CheckpointSHA, t.Summary, t.Undeclared = workflow.TaskDone, sha, strings.TrimSpace(summary), out.Undeclared
	f.State.CheckpointSHA = sha
	f.State.Current = state.Cursor{Group: g.ID}
	detail := "checkpoint " + short(sha)
	if len(out.Undeclared) > 0 {
		detail += "; undeclared files: " + strings.Join(out.Undeclared, ", ")
	}
	if _, err := jr.End(started, journal.Completed, journal.Event{IDs: append([]string{t.ID}, pt.Covers...), Detail: detail}); err != nil {
		return out, err
	}
	_ = f.RenderLog()
	out.Next = "call task_next"
	return out, s.save()
}

// Verified is the result of GroupVerify.
type Verified struct {
	Group    string
	Passed   bool
	Failed   bool // the group reached the fix-attempt limit
	Attempts int
	Commit   string
	Subject  string
	Results  []verify.Result
	Next     string
}

// GroupVerify runs build, lint and test on the group in progress once all its
// tasks are done. On success it squashes the group's work into one commit with
// SDD trailers and moves the group to awaiting-review; on failure it counts a
// fix attempt, and after limits.max_fix_attempts marks the group failed.
func GroupVerify(ctx context.Context, s *Session) (Verified, error) {
	f := s.F
	if f.Phase != workflow.Implementing {
		return Verified{}, s.phaseError()
	}
	if f.Technical == nil {
		return Verified{}, errors.New("technical.md is missing")
	}
	wt, err := s.worktree(ctx)
	if err != nil {
		return Verified{}, err
	}
	g, t, err := s.current()
	if err != nil {
		return Verified{}, err
	}
	if g == nil || g.Status != workflow.GroupInProgress {
		return Verified{}, errors.New("no group is in progress: call task_next")
	}
	if t != nil {
		return Verified{}, fmt.Errorf("%s is still in progress: finish it with task_done first", t.ID)
	}
	for _, task := range g.Tasks {
		if task.Status != workflow.TaskDone {
			return Verified{}, fmt.Errorf("%s is not done yet: call task_next", task.ID)
		}
	}
	_, pg := s.group(g.ID)
	out := Verified{Group: g.ID}

	jr, err := f.Journal()
	if err != nil {
		return out, err
	}
	started, err := jr.Start(journal.Event{Actor: s.Who.Actor(), RunID: f.State.RunID, Phase: string(workflow.Implementing),
		Action: "group.verify", IDs: []string{g.ID}})
	if err != nil {
		return out, err
	}
	results, ok := verify.Run(ctx, wt.Dir, s.verifyOptions(), s.steps("build", "lint", "test")...)
	out.Results, out.Passed = results, ok
	g.Verification = verify.Summary(results)
	if !ok {
		g.Attempts++
		out.Attempts = g.Attempts
		detail := fmt.Sprintf("attempt %d: %s", g.Attempts, lastLine(results))
		if s.Limits.MaxFixAttempts > 0 && g.Attempts >= s.Limits.MaxFixAttempts {
			g.Status, out.Failed = workflow.GroupFailed, true
			out.Next = fmt.Sprintf("stop: %s failed verification %d times; tell the user, who decides with `sdd revise %s -m \"<guidance>\"`",
				g.ID, g.Attempts, f.Name)
		} else {
			out.Next = fmt.Sprintf("fix the failures in the worktree, then call group_verify again (attempt %d of %d)", g.Attempts+1, s.Limits.MaxFixAttempts)
		}
		_, _ = jr.End(started, journal.Failed, journal.Event{Detail: detail})
		_ = f.RenderLog()
		return out, s.save()
	}

	var tasks, covers []string
	for _, task := range g.Tasks {
		tasks = append(tasks, task.ID)
		pt, _, _ := f.Plan.Task(task.ID)
		for _, id := range pt.Covers {
			if !slices.Contains(covers, id) {
				covers = append(covers, id)
			}
		}
	}
	spec.SortIDs(covers)
	out.Subject = fmt.Sprintf("feat(%s): %s %s", f.Name, g.ID, pg.Title)
	msg := workspace.CommitMessage(out.Subject, workspace.Trailers{
		Feature: f.Name, Group: g.ID, Tasks: tasks, Covers: covers, RunID: f.State.RunID,
	})
	sha, err := wt.Squash(ctx, g.BaseSHA, msg)
	if err != nil {
		_, _ = jr.End(started, journal.Failed, journal.Event{Detail: err.Error()})
		return out, err
	}
	out.Commit = sha
	g.Status, g.CommitSHA, g.Attempts, g.Feedback = workflow.GroupAwaitingReview, sha, 0, ""
	f.State.CheckpointSHA = sha
	f.State.Current = state.Cursor{Group: g.ID}
	if _, err := jr.End(started, journal.Completed, journal.Event{IDs: append([]string{g.ID}, covers...), Detail: "passed"}); err != nil {
		return out, err
	}
	_ = s.record(journal.Event{Action: "group.committed", Status: journal.Info, IDs: append([]string{g.ID}, tasks...),
		Detail: short(sha) + " " + out.Subject})
	out.Next = fmt.Sprintf("stop: %s is committed and awaits the user's review; ask the user to run `sdd review %s`", g.ID, f.Name)
	return out, s.save()
}

func lastLine(results []verify.Result) string {
	if len(results) == 0 {
		return ""
	}
	r := results[len(results)-1]
	if r.Passed {
		return "passed"
	}
	return fmt.Sprintf("%s failed (exit %d)", r.Name, r.ExitCode)
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}
