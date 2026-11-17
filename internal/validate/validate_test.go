//go:build unix

package validate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/implement"
	"github.com/darkcarard/sdd-framework/internal/sddtest"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/templates"
	"github.com/darkcarard/sdd-framework/internal/workflow"
	"github.com/darkcarard/sdd-framework/internal/workspace"
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
	r, err := Validate(ctx, p.load(), user, now, time.Minute)
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

	r, err = Validate(ctx, p.load(), user, now, time.Minute)
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
	if f.Phase != workflow.Validated || !strings.Contains(f.Next(), "merge branch sdd/user-signup") {
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
	r, err := Validate(ctx, p.load(), user, now, time.Minute)
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
	if _, err := Validate(context.Background(), f, user, now, time.Minute); err == nil || !strings.Contains(err.Error(), "every group approved") {
		t.Errorf("err = %v", err)
	}
}
