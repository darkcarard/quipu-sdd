//go:build unix

package implement

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/journal"
	"github.com/darkcarard/quipu-sdd/internal/sddtest"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/templates"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

var (
	user  = feature.Identity{Name: "Ada", Email: "ada@example.com"}
	agent = feature.Identity{Agent: "claude-code"}
	now   = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
)

// The technical spec's commands fail on demand: create BUILD_FAIL or
// TEST_FAIL in the worktree.
const technical = `  build: test ! -f BUILD_FAIL
  lint: "true"
  test: test ! -f TEST_FAIL
  integration: "true"
`

type project struct {
	t      *testing.T
	l      state.Layout
	limits config.Limits
}

func setup(t *testing.T) *project { return setupPlan(t, sddtest.Plan) }

func setupPlan(t *testing.T, planText string) *project {
	t.Helper()
	root := t.TempDir()
	l := state.Layout{Root: root}
	ctx := context.Background()
	g := workspace.Git{Dir: root}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Ada"},
		{"config", "user.email", "ada@example.com"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Init(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(l.ConfigFile(), []byte(config.DefaultYAML()), 0o644)
	os.WriteFile(l.GitignoreFile(), []byte(state.Gitignore), 0o644)
	if _, err := templates.Install(l.TemplatesDir(), l.ProfilesDir(), false); err != nil {
		t.Fatal(err)
	}
	dir := l.SpecDir(sddtest.Feature)
	os.MkdirAll(dir, 0o755)
	tech := sddtest.Technical
	i := strings.Index(tech, "  build:")
	j := strings.Index(tech, "---\n\n#")
	tech = tech[:i] + technical + tech[j:]
	os.WriteFile(filepath.Join(dir, "functional.md"), []byte(sddtest.Functional), 0o644)
	os.WriteFile(filepath.Join(dir, "technical.md"), []byte(tech), 0o644)
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte(planText), 0o644)
	p := &project{t: t, l: l, limits: config.Limits{MaxFixAttempts: 3, CommandTimeout: 30 * time.Second, MaxFilesPerTask: 5}}
	for _, a := range workflow.Artifacts() {
		if ds, err := p.load().Approve(a, user, now); err != nil {
			t.Fatalf("approve %s: %v\n%v", a, err, ds)
		}
	}
	if _, err := g.Run(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(ctx, "commit", "-q", "-m", "specs"); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *project) load() *feature.Feature {
	p.t.Helper()
	f, err := feature.Load(p.l, config.Config{Limits: p.limits}, sddtest.Feature)
	if err != nil {
		p.t.Fatal(err)
	}
	return f
}

// session loads the feature afresh, as a new sdd process would.
func (p *project) session(who feature.Identity) *Session {
	return &Session{F: p.load(), Who: who, Now: now, Limits: p.limits}
}

func (p *project) wt() string { return p.l.WorktreeDir(sddtest.Feature) }

func (p *project) write(rel, content string) {
	p.t.Helper()
	path := filepath.Join(p.wt(), rel)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) remove(rel string) { os.Remove(filepath.Join(p.wt(), rel)) }

func (p *project) next() Next {
	p.t.Helper()
	n, err := TaskNext(context.Background(), p.session(agent))
	if err != nil {
		p.t.Fatal(err)
	}
	return n
}

func (p *project) group(id string) state.GroupState {
	for _, g := range p.load().State.Groups {
		if g.ID == id {
			return g
		}
	}
	p.t.Fatalf("group %s not found", id)
	return state.GroupState{}
}

// finishTask implements the current task trivially and checks it in.
func (p *project) finishTask(files ...string) {
	p.t.Helper()
	for _, f := range files {
		p.write(f, "// "+f+"\n")
	}
	d, err := TaskDone(context.Background(), p.session(agent), "done")
	if err != nil || !d.Passed {
		p.t.Fatalf("TaskDone: %v %+v", err, d)
	}
}

func TestImplementationCycle(t *testing.T) {
	ctx := context.Background()
	p := setup(t)

	if _, err := TaskNext(ctx, p.session(agent)); !errors.Is(err, ErrNotImplementing) {
		t.Fatalf("task_next before sdd implement: %v", err)
	}
	st, err := Start(ctx, p.session(user))
	if err != nil || !st.Created || st.Group == nil || st.Group.ID != "G1" || st.Branch != "sdd/user-signup" {
		t.Fatalf("Start: %+v %v", st, err)
	}
	if f := p.load(); f.Phase != workflow.Implementing || f.State.RunID == "" || len(f.State.Groups) != 3 {
		t.Fatalf("state after start: phase %s, %+v", f.Phase, f.State)
	}

	// T1.1: the brief carries the spec text, the technical mapping, the profile and the commands.
	n := p.next()
	if n.Kind != NextTask || n.Task.ID != "T1.1" || n.Worktree != p.wt() {
		t.Fatalf("next = %+v", n)
	}
	if len(n.Task.Covers) != 2 || !strings.HasPrefix(n.Task.Covers[0].Text, "Sign up with email. The system must create") ||
		n.Task.Covers[1].Parent != "FR-001" || strings.Join(n.Task.Covers[1].Technical, ",") != "unit" {
		t.Errorf("covers = %+v", n.Task.Covers)
	}
	if !strings.Contains(n.Task.Profile, "Go Best-Practice Profile") || n.Task.Commands.Test != "test ! -f TEST_FAIL" {
		t.Errorf("profile/commands missing: %q %+v", n.Task.ProfilePath, n.Task.Commands)
	}

	// A failing build keeps the task in progress.
	p.write("BUILD_FAIL", "x")
	d, err := TaskDone(ctx, p.session(agent), "")
	if err != nil || d.Passed || d.Commit != "" {
		t.Fatalf("failing build: %+v %v", d, err)
	}
	p.remove("BUILD_FAIL")
	if g := p.group("G1"); g.Tasks[0].Status != workflow.TaskInProgress || g.Tasks[0].Attempts != 1 {
		t.Errorf("task after failure: %+v", g.Tasks[0])
	}

	// A passing build checkpoints the task and reports undeclared files.
	p.write("internal/signup/user.go", "package signup\n")
	p.write("internal/signup/user_test.go", "package signup\n// covers: AC-001\n")
	p.write("NOTES.md", "scratch\n")
	d, err = TaskDone(ctx, p.session(agent), "User entity with validation")
	if err != nil || !d.Passed || d.Commit == "" || strings.Join(d.Undeclared, ",") != "NOTES.md" {
		t.Fatalf("TaskDone: %+v %v", d, err)
	}
	if g := p.group("G1"); g.Tasks[0].Status != workflow.TaskDone || g.Tasks[0].Summary != "User entity with validation" {
		t.Errorf("task after done: %+v", g.Tasks[0])
	}

	// Every task done: verify the group; failures count attempts.
	if n := p.next(); n.Kind != NextVerify {
		t.Fatalf("next = %+v", n)
	}
	p.write("TEST_FAIL", "x")
	v, err := GroupVerify(ctx, p.session(agent))
	if err != nil || v.Passed || v.Attempts != 1 || v.Failed || !strings.Contains(p.group("G1").Verification, "test:       FAILED") {
		t.Fatalf("failing verify: %+v %v", v, err)
	}
	p.remove("TEST_FAIL")
	base := p.group("G1").BaseSHA
	v, err = GroupVerify(ctx, p.session(agent))
	if err != nil || !v.Passed || v.Commit == "" {
		t.Fatalf("GroupVerify: %+v %v", v, err)
	}
	log, _ := workspace.Open(p.wt(), "sdd/user-signup").Log(ctx, base)
	if len(log) != 1 || log[0].Subject != "feat(user-signup): G1 Domain model" || log[0].Trailers["SDD-Tasks"] != "T1.1" ||
		log[0].Trailers["SDD-Covers"] != "FR-001, AC-001" || log[0].Trailers["SDD-Run"] == "" {
		t.Fatalf("group commit = %+v", log)
	}
	if g := p.group("G1"); g.Status != workflow.GroupAwaitingReview || g.Attempts != 0 {
		t.Errorf("group after verify: %+v", g)
	}

	// The agent must stop for the review and cannot approve.
	if n := p.next(); n.Kind != NextStop || !strings.Contains(n.Message, "sdd review G1") {
		t.Errorf("next during review = %+v", n)
	}
	if _, err := ApproveGroup(ctx, p.session(agent)); !errors.Is(err, ErrHumanOnly) {
		t.Errorf("agent approval: %v", err)
	}
	if _, err := ReviseGroup(ctx, p.session(agent), "x"); !errors.Is(err, ErrHumanOnly) {
		t.Errorf("agent revise: %v", err)
	}
	if _, err := Merge(ctx, p.session(agent)); !errors.Is(err, ErrHumanOnly) {
		t.Errorf("agent merge: %v", err)
	}
	if !strings.Contains(p.load().Next(), "`sdd review G1`, then `sdd approve G1`") {
		t.Errorf("status next = %s", p.load().Next())
	}

	// A named group must be the one under review.
	wrong := p.session(user)
	wrong.Group = "G2"
	if _, err := ApproveGroup(ctx, wrong); err == nil || !strings.Contains(err.Error(), "G2 is pending; the current group is G1 (awaiting-review)") {
		t.Errorf("approve of the wrong group: %v", err)
	}
	wrong.Group = "G9"
	if _, err := ReviewGroup(ctx, wrong, false); err == nil || !strings.Contains(err.Error(), "has no group G9") {
		t.Errorf("review of an unknown group: %v", err)
	}

	r, err := ReviewGroup(ctx, p.session(user), true)
	if err != nil || !strings.Contains(r.DiffStat, "internal/signup/user.go") || !strings.Contains(r.Diff, "package signup") ||
		r.Group.Tasks[0].Undeclared[0] != "NOTES.md" {
		t.Fatalf("review: %+v %v", r, err)
	}

	// Revise: the feedback reaches the agent, and the group is squashed again into one commit.
	if _, err := ReviseGroup(ctx, p.session(user), "   "); err == nil {
		t.Error("empty feedback must be rejected")
	}
	if _, err := ReviseGroup(ctx, p.session(user), "Remove NOTES.md"); err != nil {
		t.Fatal(err)
	}
	n = p.next()
	if n.Kind != NextRevision || n.Feedback != "Remove NOTES.md" || !strings.Contains(n.DiffStat, "NOTES.md") {
		t.Fatalf("revision = %+v", n)
	}
	p.remove("NOTES.md")
	if v, err := GroupVerify(ctx, p.session(agent)); err != nil || !v.Passed {
		t.Fatalf("verify after revise: %+v %v", v, err)
	}
	log, _ = workspace.Open(p.wt(), "sdd/user-signup").Log(ctx, base)
	if len(log) != 1 {
		t.Errorf("a revised group is still one commit: %+v", log)
	}
	if g := p.group("G1"); g.Feedback != "" || g.Revisions != 1 {
		t.Errorf("group after revision: %+v", g)
	}
	a, err := ApproveGroup(ctx, p.session(user))
	if err != nil || a.Group != "G1" || a.Last || !strings.Contains(a.Next, "start G2") {
		t.Fatalf("approve: %+v %v", a, err)
	}

	// The next task_next starts G2 on top of G1's commit.
	n = p.next()
	if n.Kind != NextTask || n.Task.ID != "T2.1" || n.Group.ID != "G2" {
		t.Fatalf("next = %+v", n)
	}
	if g := p.group("G2"); g.BaseSHA != log[0].SHA {
		t.Errorf("G2 base = %s", g.BaseSHA)
	}

	// Interruption: uncommitted work survives a resume and is handed back.
	p.write("internal/signup/service.go", "package signup // half done\n")
	jr, _ := p.load().Journal()
	jr.Start(journal.Event{Actor: "agent:claude-code", Action: "task.verify", IDs: []string{"T2.1"}}) // a crash mid-verification
	res, err := Resume(ctx, p.session(user), false)
	if err != nil || len(res.Reconciled) != 1 || strings.Join(res.Kept, ",") != "internal/signup/service.go" {
		t.Fatalf("resume: %+v %v", res, err)
	}
	if n := p.next(); n.Kind != NextTask || !n.Resumed || n.Task.ID != "T2.1" || !slices.Contains(n.Dirty, "internal/signup/service.go") {
		t.Errorf("next after resume = %+v", n)
	}
	res, err = Resume(ctx, p.session(user), true)
	if err != nil || len(res.Discarded) != 1 {
		t.Fatalf("resume --discard: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(p.wt(), "internal/signup/service.go")); err == nil {
		t.Error("--discard must reset the worktree")
	}

	// A removed worktree is recreated from the branch.
	if _, err := Clean(ctx, p.session(user), false); err != nil {
		t.Fatal(err)
	}
	n = p.next()
	if _, err := os.Stat(filepath.Join(p.wt(), "internal/signup/user.go")); err != nil || n.Task.ID != "T2.1" {
		t.Fatalf("recreated worktree: %v %+v", err, n)
	}

	// The fix-attempt limit fails the group; a revision reopens it.
	p.finishTask("internal/signup/service.go", "internal/signup/service_test.go")
	p.limits.MaxFixAttempts = 2
	p.write("TEST_FAIL", "x")
	GroupVerify(ctx, p.session(agent))
	v, _ = GroupVerify(ctx, p.session(agent))
	if !v.Failed || p.group("G2").Status != workflow.GroupFailed {
		t.Fatalf("limit: %+v", v)
	}
	if n := p.next(); n.Kind != NextStop || !strings.Contains(n.Message, "failed verification 2 times") {
		t.Errorf("next after failure = %+v", n)
	}
	if !strings.Contains(p.load().Next(), "G2 failed verification") {
		t.Errorf("status next = %s", p.load().Next())
	}
	if _, err := ReviseGroup(ctx, p.session(user), "The test fails because of TEST_FAIL; delete it"); err != nil {
		t.Fatal(err)
	}
	p.remove("TEST_FAIL")
	if v, _ := GroupVerify(ctx, p.session(agent)); !v.Passed {
		t.Fatalf("verify after revision: %+v", v)
	}
	ApproveGroup(ctx, p.session(user))

	// Last group.
	p.next()
	p.finishTask("internal/mail/mail.go", "internal/mail/mail_test.go")
	if v, _ := GroupVerify(ctx, p.session(agent)); !v.Passed || v.Subject != "feat(user-signup): G3 Confirmation mail" {
		t.Fatalf("G3: %+v", v)
	}
	a, err = ApproveGroup(ctx, p.session(user))
	if err != nil || !a.Last || p.load().Phase != workflow.Implemented {
		t.Fatalf("last approval: %+v %v (phase %s)", a, err, p.load().Phase)
	}
	if _, err := TaskNext(ctx, p.session(agent)); !errors.Is(err, ErrNotImplementing) {
		t.Errorf("task_next after the last group: %v", err)
	}

	// History: three group commits on the private branch, main untouched, every action closed.
	log, _ = workspace.Open(p.wt(), "sdd/user-signup").Log(ctx, "main")
	if len(log) != 3 {
		t.Errorf("branch log = %+v", log)
	}
	events, _ := journal.Read(p.l.JournalFile(sddtest.Feature))
	if open := journal.Interrupted(events); len(open) != 0 {
		t.Errorf("open actions: %+v", open)
	}
	if out, _ := (workspace.Git{Dir: p.l.Root}).Run(ctx, "status", "--porcelain"); strings.Contains(out, "worktrees") {
		t.Errorf("the worktree must be ignored by the main repository:\n%s", out)
	}
}

// TestProfileOncePerGroup keeps task_next small: the language profile goes
// with the first task of a group and with a resumed task, not with every task.
func TestProfileOncePerGroup(t *testing.T) {
	ctx := context.Background()
	plan := strings.Replace(sddtest.Plan, "Create the entity.\n", `Create the entity.

### T1.2: Entity validation
- covers: [AC-001]
- files: [internal/signup/validate.go]
- done_when: invalid emails are rejected
- verify: TestValidate
`, 1)
	p := setupPlan(t, plan)
	if _, err := Start(ctx, p.session(user)); err != nil {
		t.Fatal(err)
	}
	n := p.next()
	if n.Task.ID != "T1.1" || n.Task.Profile == "" {
		t.Fatalf("the first task of a group carries the profile: %+v", n.Task)
	}
	p.finishTask("internal/signup/user.go")
	n = p.next()
	if n.Task.ID != "T1.2" || n.Task.Profile != "" || n.Task.ProfilePath != ".sdd/profiles/go.md" {
		t.Errorf("later tasks get only the profile path: %+v", n.Task)
	}
	if n := p.next(); !n.Resumed || n.Task.Profile == "" {
		t.Errorf("a resumed task carries the profile again (new session): %+v", n.Task)
	}
}

func TestStaleSpecsStopImplementation(t *testing.T) {
	ctx := context.Background()
	p := setup(t)
	if _, err := Start(ctx, p.session(user)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.l.SpecDir(sddtest.Feature), "functional.md")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "p95 < 300 ms", "p95 < 250 ms", 1)), 0o644)
	if _, err := TaskNext(ctx, p.session(agent)); !errors.Is(err, ErrNotImplementing) || !strings.Contains(err.Error(), "functional-drafting") {
		t.Errorf("an edited spec must stop the implementation: %v", err)
	}
}
