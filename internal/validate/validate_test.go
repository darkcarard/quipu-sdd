//go:build unix

package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/implement"
	"github.com/darkcarard/quipu-sdd/internal/sddtest"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/templates"
	"github.com/darkcarard/quipu-sdd/internal/verify"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

var (
	user  = feature.Identity{Name: "Ada", Email: "ada@example.com"}
	agent = feature.Identity{Agent: "claude-code"}
	now   = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
)

type project struct {
	t      *testing.T
	l      state.Layout
	limits config.Limits
}

func (p *project) load() *feature.Feature {
	p.t.Helper()
	f, err := feature.Load(p.l, config.Config{Limits: p.limits}, sddtest.Feature)
	if err != nil {
		p.t.Fatal(err)
	}
	return f
}

func (p *project) session(who feature.Identity) *implement.Session {
	return &implement.Session{F: p.load(), Who: who, Now: now, Limits: p.limits}
}

func (p *project) put(rel, content string) {
	path := filepath.Join(p.l.WorktreeDir(sddtest.Feature), rel)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(content), 0o644)
}

// implemented returns a project whose fixture feature is fully implemented and
// approved; its tests reference every criterion. Integration fails while
// INTEG_FAIL exists in the worktree.
func implemented(t *testing.T) *project {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	l := state.Layout{Root: root}
	g := workspace.Git{Dir: root}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Ada"},
		{"config", "user.email", "ada@example.com"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	l.Init()
	os.WriteFile(l.ConfigFile(), []byte(config.DefaultYAML()), 0o644)
	os.WriteFile(l.GitignoreFile(), []byte(state.Gitignore), 0o644)
	templates.Install(l.TemplatesDir(), l.ProfilesDir(), false)
	dir := l.SpecDir(sddtest.Feature)
	os.MkdirAll(dir, 0o755)
	tech := sddtest.Technical
	i, j := strings.Index(tech, "  build:"), strings.Index(tech, "---\n\n#")
	tech = tech[:i] + "  build: \"true\"\n  lint: \"true\"\n  test: \"true\"\n  integration: test ! -f INTEG_FAIL\n" + tech[j:]
	os.WriteFile(filepath.Join(dir, "functional.md"), []byte(sddtest.Functional), 0o644)
	os.WriteFile(filepath.Join(dir, "technical.md"), []byte(tech), 0o644)
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte(sddtest.Plan), 0o644)
	p := &project{t: t, l: l, limits: config.Limits{MaxFixAttempts: 3, CommandTimeout: 30 * time.Second, MaxFilesPerTask: 5}}
	for _, a := range workflow.Artifacts() {
		if _, err := p.load().Approve(a, user, now); err != nil {
			t.Fatal(err)
		}
	}
	g.Run(ctx, "add", ".")
	g.Run(ctx, "commit", "-q", "-m", "specs")
	if _, err := implement.Start(ctx, p.session(user)); err != nil {
		t.Fatal(err)
	}
	files := map[string]map[string]string{
		"T1.1": {"internal/signup/user.go": "package signup\n", "internal/signup/user_test.go": "package signup\n// covers: AC-001\nfunc TestUser() {}\n"},
		"T2.1": {"internal/signup/service.go": "package signup\n", "internal/signup/service_test.go": "package signup\n// Covers: AC-002, NFR-001\nfunc TestRegister() {}\n"},
		"T3.1": {"internal/mail/mail.go": "package mail\n", "internal/mail/mail_test.go": "package mail\n// covers: AC-003\nfunc TestConfirm() {}\n"},
	}
	for range 3 {
		n, err := implement.TaskNext(ctx, p.session(agent))
		if err != nil || n.Task == nil {
			t.Fatalf("task_next: %+v %v", n, err)
		}
		for rel, content := range files[n.Task.ID] {
			p.put(rel, content)
		}
		if d, err := implement.TaskDone(ctx, p.session(agent), "done"); err != nil || !d.Passed {
			t.Fatalf("task_done: %+v %v", d, err)
		}
		if v, err := implement.GroupVerify(ctx, p.session(agent)); err != nil || !v.Passed {
			t.Fatalf("group_verify: %+v %v", v, err)
		}
		if _, err := implement.ApproveGroup(ctx, p.session(user)); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestValidate(t *testing.T) {
	ctx := context.Background()
	p := implemented(t)
	if ph := p.load().Phase; ph != workflow.Implemented {
		t.Fatalf("phase = %s", ph)
	}

	// Plan ↔ code: one commit per group, every AC referenced by a test.
	ds, links, err := CheckCode(ctx, p.load())
	if err != nil || len(ds) != 0 {
		t.Fatalf("CheckCode: %v %v", ds, err)
	}
	if len(links.Commits["AC-002"]) != 1 || !strings.Contains(links.Commits["AC-002"][0], "G2 Signup service") ||
		strings.Join(links.Tests["AC-002"], ",") != "internal/signup/service_test.go:2" || len(links.Tests["NFR-001"]) != 1 {
		t.Errorf("links = %+v", links)
	}

	// A failing integration test fails the validation and the report says why.
	p.put("INTEG_FAIL", "x")
	r, err := Validate(ctx, p.load(), user, now, verify.Options{Timeout: time.Minute})
	if err != nil || r.Passed {
		t.Fatalf("failing integration: %+v %v", r, err)
	}
	report, _ := os.ReadFile(p.l.ReportFile(sddtest.Feature))
	if !strings.Contains(string(report), "- Result: **FAILED**") || !strings.Contains(string(report), "integration: FAILED") {
		t.Errorf("report:\n%s", report)
	}
	if p.load().Phase != workflow.Implemented {
		t.Error("a failed validation must not change the phase")
	}
	os.Remove(filepath.Join(p.l.WorktreeDir(sddtest.Feature), "INTEG_FAIL"))

	r, err = Validate(ctx, p.load(), user, now, verify.Options{Timeout: time.Minute})
	if err != nil || !r.Passed {
		t.Fatalf("Validate: %+v %v", r, err)
	}
	report, _ = os.ReadFile(p.l.ReportFile(sddtest.Feature))
	for _, want := range []string{
		"# Validation report: user-signup", "- Result: **PASSED**", "integration: passed", "No problems found.",
		"| G2 Signup service | approved |", "| ID | Parent | Title | Technical | Plan | Commits | Tests |",
		"internal/mail/mail_test.go:2 |",
	} {
		if !strings.Contains(string(report), want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	f := p.load()
	if f.Phase != workflow.Validated || !strings.Contains(f.Next(), "`sdd merge user-signup`") || f.State.ValidatedSHA != r.Head {
		t.Errorf("phase %s, next %q", f.Phase, f.Next())
	}

	// Trace now reaches commits and tests.
	ix, _, _ := Index(ctx, f)
	e, _ := ix.Lookup("AC-003")
	if out := e.Render(); !strings.Contains(out, "commits:   ") || !strings.Contains(out, "G3 Confirmation mail") ||
		!strings.Contains(out, "tests:     internal/mail/mail_test.go:2") {
		t.Errorf("trace:\n%s", out)
	}

	brief, err := ReviewContext(ctx, f)
	if err != nil || !strings.Contains(brief, "### AC-002 (FR-001)") || !strings.Contains(brief, "tests: internal/signup/service_test.go:2") ||
		!strings.Contains(brief, ".sdd/features/user-signup/deep-review.md") {
		t.Errorf("review context:\n%s (%v)", brief, err)
	}
}

func TestCodeChecksFindProblems(t *testing.T) {
	ctx := context.Background()
	p := implemented(t)
	wt := workspace.Git{Dir: p.l.WorktreeDir(sddtest.Feature)}

	// A test loses its reference, another references an unknown criterion, and
	// a commit is made outside sdd.
	p.put("internal/mail/mail_test.go", "package mail\nfunc TestConfirm() {}\n")
	p.put("internal/extra_test.go", "package x\n// covers: AC-042\n")
	wt.Run(ctx, "add", ".")
	wt.Run(ctx, "commit", "-q", "-m", "hand-made change")

	ds, _, err := CheckCode(ctx, p.load())
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, string(d.Severity)+": "+d.Message)
	}
	got := strings.Join(msgs, "\n")
	for _, want := range []string{
		"error: AC-003 is not referenced by any test",
		"warning: covers AC-042, which is not defined",
		`warning: commit`, `"hand-made change" has no SDD trailers`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	r, err := Validate(ctx, p.load(), user, now, verify.Options{Timeout: time.Minute})
	if err != nil || r.Passed {
		t.Errorf("validation must fail: %+v %v", r, err)
	}

	// Before implementation there is nothing to check.
	f := p.load()
	f.State.Groups = nil
	if ds, _, err := CheckCode(ctx, f); ds != nil || err != nil {
		t.Errorf("no implementation: %v %v", ds, err)
	}
}

func TestValidateNeedsImplementation(t *testing.T) {
	p := implemented(t)
	f := p.load()
	f.SetPhase(workflow.Implementing)
	if _, err := Validate(context.Background(), f, user, now, verify.Options{Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "every group approved") {
		t.Errorf("err = %v", err)
	}
}

// Re-approving the specs and the plan of a feature whose groups are all
// approved returns it to implemented, so it can be validated again without
// another `sdd implement`.
func TestReapprovedPlanKeepsImplementation(t *testing.T) {
	ctx := context.Background()
	p := implemented(t)
	path := filepath.Join(p.l.SpecDir(sddtest.Feature), "technical.md")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, "\nA clarification.\n"...), 0o644)
	if ph := p.load().Phase; ph != workflow.TechnicalDrafting {
		t.Fatalf("edited technical: phase = %s", ph)
	}
	if _, err := p.load().Approve(workflow.Technical, user, now); err != nil {
		t.Fatal(err)
	}
	if ph := p.load().Phase; ph != workflow.PlanDrafting {
		t.Fatalf("stale plan: phase = %s", ph)
	}
	if _, err := p.load().Approve(workflow.Plan, user, now); err != nil {
		t.Fatal(err)
	}
	f := p.load()
	if f.Phase != workflow.Implemented || f.State.Phase != workflow.Implemented {
		t.Fatalf("re-approved plan: phase = %s (stored %s)", f.Phase, f.State.Phase)
	}
	if r, err := Validate(ctx, f, user, now, verify.Options{Timeout: time.Minute}); err != nil || !r.Passed {
		t.Fatalf("validate: %+v %v", r, err)
	}
}

// A `covers:` reference counts for a feature only in files the feature
// changed, or when it is qualified with the feature's name.
func TestCoversScopedToFeature(t *testing.T) {
	ctx := context.Background()
	p := implemented(t)
	main := workspace.Git{Dir: p.l.Root}
	wt := workspace.Git{Dir: p.l.WorktreeDir(sddtest.Feature)}
	// Another feature's test, merged into the branch from main.
	other := func(content string) {
		t.Helper()
		os.MkdirAll(filepath.Join(p.l.Root, "other"), 0o755)
		os.WriteFile(filepath.Join(p.l.Root, "other", "other_test.go"), []byte(content), 0o644)
		main.Run(ctx, "add", "other")
		main.Run(ctx, "commit", "-q", "-m", "another feature")
		if _, err := wt.Run(ctx, "merge", "-q", "--no-edit", "main"); err != nil {
			t.Fatal(err)
		}
	}
	// AC-003 is referenced only by the other feature's test.
	wt.Run(ctx, "rm", "-q", "internal/mail/mail_test.go")
	wt.Run(ctx, "commit", "-q", "-m", "drop the test")
	other("package other\n// covers: AC-003, other-feature/AC-001\n")
	ds, links, err := CheckCode(ctx, p.load())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(ds), "AC-003 is not referenced by any test") || len(links.Tests["AC-001"]) != 1 || len(links.Tests["AC-003"]) != 0 {
		t.Fatalf("another feature's reference counted: %v %+v", ds, links.Tests)
	}
	// Qualified with this feature, a reference counts in any file.
	other("package other\n// covers: " + sddtest.Feature + "/AC-003\n")
	ds, links, err = CheckCode(ctx, p.load())
	if err != nil || strings.Contains(fmt.Sprint(ds), "AC-003 is not referenced") || len(links.Tests["AC-003"]) != 1 {
		t.Fatalf("qualified reference: %v %+v %v", ds, links.Tests, err)
	}
}

// An unqualified `covers:` in a file that another feature's plan also declares,
// for an ID both features define, belongs to the feature whose commit wrote the
// line (its SDD-Feature trailer); without one it is ambiguous and reported.
func TestAmbiguousCoversInSharedFile(t *testing.T) {
	ctx := context.Background()
	p := implemented(t)
	dir := p.l.SpecDir("other-feature")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "functional.md"), []byte(sddtest.Functional), 0o644)
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte(sddtest.Plan), 0o644)

	// Written by this feature's group commit.
	ds, links, err := CheckCode(ctx, p.load())
	if err != nil || len(ds) != 0 || len(links.Tests["AC-001"]) != 1 {
		t.Fatalf("own line: %v %+v %v", ds, links.Tests, err)
	}

	// Written by a commit of the other feature: its reference, not this one's.
	wt := workspace.Git{Dir: p.l.WorktreeDir(sddtest.Feature)}
	test := "package signup\n// covers: AC-001\nfunc TestUser() {}\n// covers: AC-002\nfunc TestOther() {}\n"
	p.put("internal/signup/user_test.go", test)
	wt.Run(ctx, "commit", "-q", "-am", workspace.CommitMessage("other", workspace.Trailers{Feature: "other-feature"}))
	ds, links, err = CheckCode(ctx, p.load())
	if err != nil || strings.Contains(fmt.Sprint(ds), "ambiguous") || slices.Contains(links.Tests["AC-002"], "internal/signup/user_test.go:4") {
		t.Fatalf("other feature's line: %v %+v %v", ds, links.Tests, err)
	}

	// Not committed yet: nobody wrote it, so it is ambiguous.
	p.put("internal/signup/user_test.go", test+"// covers: AC-003\n")
	ds, _, err = CheckCode(ctx, p.load())
	if err != nil {
		t.Fatal(err)
	}
	want := "ambiguous covers AC-003: this file is also in the plan of other-feature, which defines AC-003; qualify it as " + sddtest.Feature + "/AC-003"
	if !strings.Contains(fmt.Sprint(ds), want) || ds.HasErrors() {
		t.Fatalf("diagnostics = %v", ds)
	}

	// Qualified, the reference is no longer ambiguous.
	p.put("internal/signup/user_test.go", test+"// covers: "+sddtest.Feature+"/AC-003\n")
	ds, links, err = CheckCode(ctx, p.load())
	if err != nil || strings.Contains(fmt.Sprint(ds), "ambiguous") || len(links.Tests["AC-003"]) != 2 {
		t.Fatalf("qualified: %v %+v %v", ds, links.Tests, err)
	}
}

// An unqualified `covers:` in a file that another feature's plan also declares,
// for an ID only that feature defines, is the other feature's: no warning.
func TestSharedFileCoversOfAnotherFeature(t *testing.T) {
	ctx := context.Background()
	p := implemented(t)
	dir := p.l.SpecDir("other-feature")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "functional.md"), []byte(strings.Replace(sddtest.Functional, "AC-003", "AC-032", -1)), 0o644)
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte(sddtest.Plan), 0o644)
	p.put("internal/signup/user_test.go", "package signup\n// covers: AC-001\n// covers: AC-032\n// covers: AC-099\nfunc TestUser() {}\n")

	ds, links, err := CheckCode(ctx, p.load())
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(ds); strings.Contains(got, "AC-032") || !strings.Contains(got, "covers AC-099, which is not defined") {
		t.Fatalf("diagnostics = %v", ds)
	}
	if len(links.Tests["AC-032"]) != 0 {
		t.Fatalf("another feature's ID linked: %+v", links.Tests)
	}
}

// merged returns a validated feature merged into main with sdd merge.
func merged(t *testing.T) *project {
	t.Helper()
	ctx := context.Background()
	p := implemented(t)
	if r, err := Validate(ctx, p.load(), user, now, verify.Options{Timeout: time.Minute}); err != nil || !r.Passed {
		t.Fatalf("validate: %+v %v", r, err)
	}
	if _, err := implement.Merge(ctx, p.session(user)); err != nil {
		t.Fatal(err)
	}
	g := workspace.Git{Dir: p.l.Root}
	g.Run(ctx, "add", ".")
	g.Run(ctx, "commit", "-q", "-m", "records")
	return p
}

// A spec edited after the merge is an erratum: the feature stays validated
// and approving it restamps the plan instead of asking for its re-approval.
func TestErratumAfterMerge(t *testing.T) {
	p := merged(t)
	path := filepath.Join(p.l.SpecDir(sddtest.Feature), "technical.md")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, "\nA clarification.\n"...), 0o644)

	f := p.load()
	if f.Phase != workflow.Validated || len(f.Errata()) != 1 || !strings.Contains(f.Next(), "edited after the merge") {
		t.Fatalf("edited after merge: phase %s, errata %v, next %q", f.Phase, f.Errata(), f.Next())
	}
	if _, err := implement.Start(context.Background(), p.session(user)); err == nil {
		t.Fatal("implement started on a merged feature")
	}
	if _, err := f.Approve(workflow.Technical, user, now); err != nil {
		t.Fatal(err)
	}
	if len(f.Restamped) != 1 || f.Restamped[0] != workflow.Plan {
		t.Fatalf("restamped = %v", f.Restamped)
	}
	f = p.load()
	for a, st := range f.Statuses {
		if st != workflow.StatusApproved {
			t.Errorf("%s is %s", a, st)
		}
	}
	if f.Phase != workflow.Validated || f.State.Phase != workflow.Validated || f.Plan.TechnicalHash != f.Files[workflow.Technical].Hash {
		t.Fatalf("after the erratum: phase %s (stored %s), plan technical_hash %s", f.Phase, f.State.Phase, f.Plan.TechnicalHash)
	}
	if ds := f.Sync(); ds.HasErrors() {
		t.Fatalf("sync: %v", ds)
	}

	// An edit that changes the IDs is new behavior, not an erratum.
	path = filepath.Join(p.l.SpecDir(sddtest.Feature), "functional.md")
	b, _ = os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "**AC-003:**", "**AC-004:**", 1)), 0o644)
	_, err := p.load().Approve(workflow.Functional, user, now)
	if !errors.Is(err, feature.ErrErratumIDs) || !strings.Contains(err.Error(), "adds AC-004 and removes AC-003") {
		t.Fatalf("ID change: %v", err)
	}

	// A state left in plan-approved by an older sdd is shown as validated.
	f = p.load()
	f.State.Phase = workflow.PlanApproved
	f.SaveState()
	if ph := p.load().Phase; ph != workflow.Validated {
		t.Fatalf("legacy state: phase %s", ph)
	}
}

// Validating a merged feature checks the project checkout and removes a
// leftover worktree instead of recreating it from the old branch.
func TestValidateMergedFeature(t *testing.T) {
	ctx := context.Background()
	p := merged(t)
	wtDir := p.l.WorktreeDir(sddtest.Feature)
	if _, err := (workspace.Git{Dir: p.l.Root}).Run(ctx, "worktree", "add", "-q", wtDir, workspace.Branch(sddtest.Feature)); err != nil {
		t.Fatal(err)
	}
	f := p.load()
	if !strings.Contains(f.Next(), "sdd clean") {
		t.Fatalf("next with a leftover worktree: %q", f.Next())
	}
	r, err := Validate(ctx, f, user, now, verify.Options{Timeout: time.Minute})
	if err != nil || !r.Passed || !r.Merged || !r.Removed {
		t.Fatalf("validate: %+v %v", r, err)
	}
	if _, err := os.Stat(wtDir); err == nil {
		t.Fatal("the leftover worktree is still there")
	}
	if f := p.load(); f.Phase != workflow.Validated || strings.Contains(f.Next(), "sdd clean") {
		t.Fatalf("after validate: phase %s, next %q", f.Phase, f.Next())
	}
}
