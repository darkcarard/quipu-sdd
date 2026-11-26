package feature

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/journal"
	"github.com/darkcarard/quipu-sdd/internal/sddtest"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
)

var (
	who = Identity{Name: "Ada", Email: "ada@example.com"}
	now = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
)

func setup(t *testing.T) state.Layout {
	t.Helper()
	l := state.Layout{Root: t.TempDir()}
	if err := l.Init(); err != nil {
		t.Fatal(err)
	}
	return l
}

func write(t *testing.T, l state.Layout, a workflow.Artifact, content string) {
	t.Helper()
	p := filepath.Join(l.SpecDir(sddtest.Feature), FileName(a))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T, l state.Layout) *Feature {
	t.Helper()
	f, err := Load(l, config.Default(), sddtest.Feature)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func approve(t *testing.T, l state.Layout, a workflow.Artifact) *Feature {
	t.Helper()
	f := load(t, l)
	if ds, err := f.Approve(a, who, now); err != nil {
		t.Fatalf("approve %s: %v\n%v", a, err, ds)
	}
	return f
}

func TestLoadUnknownFeature(t *testing.T) {
	l := setup(t)
	if _, err := Load(l, config.Default(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateFromTemplates(t *testing.T) {
	l := setup(t)
	f, err := Open(l, config.Default(), sddtest.Feature)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Create(workflow.Technical, Info{}, who, now); err == nil || !strings.Contains(err.Error(), "functional.md does not exist") {
		t.Fatalf("technical before functional: %v", err)
	}
	path, err := f.Create(workflow.Functional, Info{}, who, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "id: user-signup") || !strings.Contains(string(b), "# User Signup: Functional Specification") {
		t.Errorf("template not rendered:\n%s", b)
	}
	if _, err := f.Create(workflow.Functional, Info{}, who, now); err == nil {
		t.Error("creating an existing artifact must fail")
	}
	st, err := l.Load(sddtest.Feature)
	if err != nil || st.Phase != workflow.FunctionalDrafting {
		t.Fatalf("state = %+v, %v", st, err)
	}

	// A customized project template wins over the built-in one.
	if err := os.WriteFile(filepath.Join(l.TemplatesDir(), "functional.md"), []byte("---\nid: {{feature}}\n---\n# Custom {{title}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, _ := Open(l, config.Default(), "other")
	path, err = g.Create(workflow.Functional, Info{Title: "Other Thing"}, who, now)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "---\nid: other\n---\n# Custom Other Thing\n" {
		t.Errorf("custom template not used:\n%s", b)
	}
}

func TestApproveChainAndStaleness(t *testing.T) {
	l := setup(t)
	write(t, l, workflow.Functional, sddtest.Functional)
	write(t, l, workflow.Technical, sddtest.Technical)

	f := load(t, l)
	if _, err := f.Approve(workflow.Technical, who, now); err == nil || !strings.Contains(err.Error(), "approve it first") {
		t.Fatalf("technical before functional approval: %v", err)
	}

	f = approve(t, l, workflow.Functional)
	if f.Phase != workflow.TechnicalDrafting {
		t.Errorf("phase = %s", f.Phase)
	}
	appr := f.State.Approvals[workflow.Functional]
	if appr.By != "Ada <ada@example.com>" || !appr.At.Equal(now) || len(appr.IDs) != 6 {
		t.Errorf("approval = %+v", appr)
	}
	if _, err := load(t, l).Approve(workflow.Functional, who, now); !errors.Is(err, ErrAlreadyApproved) {
		t.Errorf("second approval: %v", err)
	}

	f = approve(t, l, workflow.Technical)
	if f.Technical.FunctionalHash != f.Files[workflow.Functional].Hash {
		t.Errorf("functional_hash not stamped: %q", f.Technical.FunctionalHash)
	}
	f, err := Open(l, config.Default(), sddtest.Feature)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Create(workflow.Plan, Info{}, who, now); err != nil {
		t.Fatal(err)
	}
	write(t, l, workflow.Plan, sddtest.Plan)
	f = approve(t, l, workflow.Plan)
	if f.Phase != workflow.PlanApproved {
		t.Errorf("phase = %s", f.Phase)
	}
	if f.Plan.FunctionalHash == "" || f.Plan.TechnicalHash != f.Files[workflow.Technical].Hash {
		t.Errorf("plan hashes not stamped: %+v", f.Plan)
	}
	if ds := f.Sync(); len(ds) != 0 {
		t.Errorf("approved feature should be in sync:\n%v", ds)
	}

	// Editing the approved functional spec makes everything downstream stale.
	write(t, l, workflow.Functional, strings.Replace(sddtest.Functional, "p95 < 300 ms", "p95 < 250 ms", 1))
	f = load(t, l)
	want := map[workflow.Artifact]workflow.ArtifactStatus{
		workflow.Functional: workflow.StatusModified, workflow.Technical: workflow.StatusStale, workflow.Plan: workflow.StatusStale,
	}
	for a, st := range want {
		if f.Statuses[a] != st {
			t.Errorf("%s = %s, want %s", a, f.Statuses[a], st)
		}
	}
	if f.Phase != workflow.FunctionalDrafting {
		t.Errorf("phase = %s", f.Phase)
	}
	if !f.Sync().HasErrors() {
		t.Error("sync must fail on modified and stale artifacts")
	}
	if !strings.Contains(f.Next(), "sdd spec approve user-signup --functional") {
		t.Errorf("next = %q", f.Next())
	}

	// Re-approving the functional spec leaves the technical spec stale until it is re-approved.
	f = approve(t, l, workflow.Functional)
	if f.Phase != workflow.TechnicalDrafting || f.Statuses[workflow.Technical] != workflow.StatusStale {
		t.Errorf("phase = %s, technical = %s", f.Phase, f.Statuses[workflow.Technical])
	}
	f = approve(t, l, workflow.Technical)
	if f.Statuses[workflow.Plan] != workflow.StatusStale || f.Phase != workflow.PlanDrafting {
		t.Errorf("plan = %s, phase = %s", f.Statuses[workflow.Plan], f.Phase)
	}

	events, err := journal.Read(l.JournalFile(sddtest.Feature))
	if err != nil {
		t.Fatal(err)
	}
	if open := journal.Interrupted(events); len(open) != 0 {
		t.Errorf("every started action must be closed: %+v", open)
	}
	last := events[len(events)-1]
	if last.Action != "spec.approve" || last.Status != journal.Completed || last.Actor != "user:ada@example.com" || last.Hashes["functional"] == "" {
		t.Errorf("last event = %+v", last)
	}
	if b, err := os.ReadFile(l.LogFile(sddtest.Feature)); err != nil || !strings.Contains(string(b), "spec.approve") {
		t.Errorf("log.md not rendered: %v", err)
	}
}

func TestApproveBlockedByLint(t *testing.T) {
	l := setup(t)
	write(t, l, workflow.Functional, strings.Replace(sddtest.Functional, "<!-- Must be empty before approval. -->", "- Captcha?", 1))
	f := load(t, l)
	ds, err := f.Approve(workflow.Functional, who, now)
	if !errors.Is(err, ErrBlocked) || !ds.HasErrors() {
		t.Fatalf("err = %v, diags = %v", err, ds)
	}
	if f.Statuses[workflow.Functional] != workflow.StatusDraft {
		t.Errorf("status = %s", f.Statuses[workflow.Functional])
	}
	events, _ := journal.Read(l.JournalFile(sddtest.Feature))
	if n := len(events); n != 2 || events[1].Status != journal.Failed || events[1].Ref != events[0].Seq {
		t.Errorf("events = %+v", events)
	}
}

func TestDeletedIDBlocksReapproval(t *testing.T) {
	l := setup(t)
	write(t, l, workflow.Functional, sddtest.Functional)
	approve(t, l, workflow.Functional)
	write(t, l, workflow.Functional, strings.Replace(sddtest.Functional,
		"- **AC-003:** Given a new account, when it is created, then a confirmation email is sent.",
		"- **AC-004:** Given a new account, when it is created, then a confirmation email is sent.", 1))
	ds, err := load(t, l).Approve(workflow.Functional, who, now)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
	found := false
	for _, d := range ds {
		found = found || strings.Contains(d.Message, "AC-003 was approved before and is now gone")
	}
	if !found {
		t.Errorf("renumbering must be reported:\n%v", ds)
	}
}

func TestList(t *testing.T) {
	l := setup(t)
	for _, d := range []string{l.SpecDir("b"), l.FeatureDir("a"), l.SpecDir("a"), filepath.Join(l.SpecsDir(), ".hidden")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	names, err := List(l)
	if err != nil || strings.Join(names, ",") != "a,b" {
		t.Errorf("List = %v, %v", names, err)
	}
}

func TestAgentsCannotApprove(t *testing.T) {
	l := setup(t)
	write(t, l, workflow.Functional, sddtest.Functional)
	if _, err := load(t, l).Approve(workflow.Functional, Identity{Agent: "claude-code"}, now); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("err = %v", err)
	}
	if load(t, l).Statuses[workflow.Functional] != workflow.StatusDraft {
		t.Error("an agent's approval attempt must change nothing")
	}
	if got := (Identity{Name: "Ada", Email: "a@x", Agent: "claude-code"}).Actor(); got != "agent:claude-code" {
		t.Errorf("Actor = %s", got)
	}
}

func TestBaselineFeature(t *testing.T) {
	l := setup(t)
	f, _ := Open(l, config.Default(), sddtest.Feature)
	path, err := f.Create(workflow.Functional, Info{Baseline: true}, who, now)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `origin: "existing"`) {
		t.Errorf("the functional spec must be marked as existing:\n%s", b)
	}
	if f := load(t, l); !f.Baseline() || f.State.Origin != OriginExisting || !strings.Contains(f.Next(), "/sdd-adopt") {
		t.Errorf("baseline = %v, origin %q, next %q", f.Baseline(), f.State.Origin, f.Next())
	}

	baseline := strings.Replace(sddtest.Functional, "depends_on: []", "depends_on: []\norigin: existing", 1)
	write(t, l, workflow.Functional, baseline)
	approve(t, l, workflow.Functional)
	write(t, l, workflow.Technical, sddtest.Technical)
	f = approve(t, l, workflow.Technical)
	if f.Phase != workflow.Baselined || !strings.Contains(f.Next(), "depends_on: [user-signup]") {
		t.Fatalf("phase %s, next %q", f.Phase, f.Next())
	}
	if _, err := f.Create(workflow.Plan, Info{}, who, now); !errors.Is(err, ErrBaselinePlan) {
		t.Errorf("a baseline has no plan: %v", err)
	}
	write(t, l, workflow.Plan, sddtest.Plan)
	if _, err := load(t, l).Approve(workflow.Plan, who, now); !errors.Is(err, ErrBaselinePlan) {
		t.Errorf("approving a plan for a baseline: %v", err)
	}
	if ds := load(t, l).Sync(); !ds.HasErrors() {
		t.Error("a plan.md in a baseline must be reported")
	}
	os.Remove(filepath.Join(l.SpecDir(sddtest.Feature), "plan.md"))

	// Revising the baseline reopens it until both specs are re-approved.
	write(t, l, workflow.Functional, strings.Replace(baseline, "p95 < 300 ms", "p95 < 200 ms", 1))
	if f := load(t, l); f.Phase != workflow.FunctionalDrafting {
		t.Errorf("revised baseline phase = %s", f.Phase)
	}
	approve(t, l, workflow.Functional)
	if f := approve(t, l, workflow.Technical); f.Phase != workflow.Baselined {
		t.Errorf("re-approved baseline phase = %s", f.Phase)
	}

	// A feature that depends on it is told when the baseline is not approved.
	g, _ := Open(l, config.Default(), "checkout")
	g.Create(workflow.Functional, Info{}, who, now)
	dep := strings.Replace(strings.Replace(sddtest.Functional, "depends_on: []", "depends_on: [user-signup]", 1), "id: user-signup", "id: checkout", 1)
	os.WriteFile(filepath.Join(l.SpecDir("checkout"), "functional.md"), []byte(dep), 0o644)
	ck, _ := Load(l, config.Default(), "checkout")
	for _, d := range ck.Sync() {
		if strings.Contains(d.Message, "depends_on") {
			t.Errorf("an approved baseline dependency is fine: %v", d)
		}
	}
	write(t, l, workflow.Functional, baseline+"\n") // edit the baseline again
	ck, _ = Load(l, config.Default(), "checkout")
	found := false
	for _, d := range ck.Sync() {
		found = found || strings.Contains(d.Message, `baseline "user-signup" is not approved yet`)
	}
	if !found {
		t.Errorf("an unapproved baseline dependency must be reported: %v", ck.Sync())
	}
}

// References to other features' IDs: <feature>/<ID> is checked against that
// feature by sync; "<feature> <ID>" is reported by lint as one to qualify.
func TestCrossFeatureReferences(t *testing.T) {
	l := setup(t)
	other := filepath.Join(l.SpecDir("mock-domain"), "functional.md")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte(sddtest.Functional), 0o644)
	write(t, l, workflow.Functional, strings.Replace(sddtest.Functional, "(see AC-001)",
		"(see AC-001, mock-domain/FR-002, mock-domain/FR-012, ghost/AC-001 and mock-domain AC-007)", 1))

	f := load(t, l)
	lint, sync := fmt.Sprint(f.Lint(workflow.Functional)), fmt.Sprint(f.Sync())
	for _, w := range []string{"mock-domain/FR-012: FR-012 is not defined in specs/mock-domain", `ghost/AC-001: feature "ghost" does not exist`} {
		if !strings.Contains(sync, w) {
			t.Errorf("sync: want %q, got:\n%s", w, sync)
		}
	}
	if strings.Contains(sync, "FR-002") {
		t.Errorf("a defined cross reference was reported:\n%s", sync)
	}
	if !strings.Contains(lint, "mock-domain AC-007 reads as a reference to feature mock-domain") || strings.Contains(lint, "AC-007 is referenced but not defined") {
		t.Errorf("lint:\n%s", lint)
	}
}
