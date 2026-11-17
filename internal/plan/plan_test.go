package plan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/sddtest"
	"github.com/darkcarard/sdd-framework/internal/spec"
)

func functional(t *testing.T) *spec.Functional {
	t.Helper()
	f, ds := spec.ParseFunctional([]byte(sddtest.Functional))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return f
}

func TestParseFixture(t *testing.T) {
	p, ds := Parse([]byte(sddtest.Plan))
	ds = append(ds, Validate(p, Options{Feature: sddtest.Feature, MaxFilesPerTask: 5})...)
	ds = append(ds, CheckCoverage(p, functional(t))...)
	if len(ds) != 0 {
		t.Fatalf("fixture should validate clean, got:\n%v", ds)
	}
	if len(p.Groups) != 3 || p.Groups[1].Layer != "service" || !reflect.DeepEqual(p.Groups[1].DependsOn, []string{"G1"}) {
		t.Fatalf("groups = %+v", p.Groups)
	}
	task, g, ok := p.Task("T1.1")
	if !ok || g.ID != "G1" || task.Description != "Create the entity." || task.Verify != "TestUser" ||
		!reflect.DeepEqual(task.Files, []string{"internal/signup/user.go", "internal/signup/user_test.go"}) {
		t.Errorf("T1.1 = %+v", task)
	}
	if got := p.Covered()["FR-001"]; !reflect.DeepEqual(got, []string{"T1.1", "T2.1"}) {
		t.Errorf("Covered[FR-001] = %v", got)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name, old, new string
		sev            diag.Severity
		text           string
	}{
		{"forward dependency", "- depends_on: [G1]", "- depends_on: [G3]", diag.Error, "G2 depends on G3, which comes later"},
		{"self dependency", "- depends_on: [G1]", "- depends_on: [G2]", diag.Error, "G2 depends on itself"},
		{"unknown dependency", "- depends_on: [G1]", "- depends_on: [G9]", diag.Error, "unknown group G9"},
		{"group out of sequence", "## G3: Confirmation mail", "## G4: Confirmation mail", diag.Error, "group G4 is out of sequence: expected G3"},
		{"task out of sequence", "### T2.1: Register", "### T2.2: Register", diag.Error, "task T2.2 is out of sequence: expected T2.1"},
		{"task in wrong group", "### T3.1: Confirm", "### T2.2: Confirm", diag.Error, "expected T3.1"},
		{"too many files", "- files: [internal/mail/mail.go, internal/mail/mail_test.go]",
			"- files: [a, b, c, d, e, f]", diag.Error, "T3.1 touches 6 files; the limit is 5"},
		{"missing verify", "- verify: TestConfirm\n", "", diag.Error, `T3.1 is missing "- verify:"`},
		{"missing layer", "- layer: integration\n", "", diag.Error, "G3 has no layer"},
		{"layer revisited", "- layer: integration", "- layer: domain", diag.Warning, `G3 returns to layer "domain"`},
		{"covers a decision", "- covers: [FR-002, AC-003]", "- covers: [FR-002, AC-003, ADR-001]", diag.Error, "T3.1 covers ADR-001"},
		{"group without tasks", "### T3.1: Confirm", "#### T3.1: Confirm", diag.Error, "G3 has no tasks"},
		{"placeholder", "Create the entity.", "<description>", diag.Error, "template placeholder <description>"},
		{"few groups", "## G3: Confirmation mail", "### T2.2: Confirmation mail", diag.Warning, "the plan has 2 groups"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(sddtest.Plan, tt.old, tt.new, 1)
			if src == sddtest.Plan {
				t.Fatalf("fixture does not contain %q", tt.old)
			}
			p, ds := Parse([]byte(src))
			ds = append(ds, Validate(p, Options{MaxFilesPerTask: 5})...)
			for _, d := range ds {
				if d.Severity == tt.sev && strings.Contains(d.Message, tt.text) {
					return
				}
			}
			t.Errorf("want %s %q, got:\n%v", tt.sev, tt.text, ds)
		})
	}
}

func TestCheckCoverage(t *testing.T) {
	src := strings.Replace(sddtest.Plan, "- covers: [FR-002, AC-003]", "- covers: [FR-002, AC-009]", 1)
	src = strings.Replace(src, "AC-002, NFR-001]", "AC-002]", 1)
	p, _ := Parse([]byte(src))
	ds := CheckCoverage(p, functional(t))
	for _, w := range []struct {
		sev  diag.Severity
		text string
	}{
		{diag.Error, "AC-003 is not covered by any task"},
		{diag.Error, "T3.1 covers AC-009, which is not defined"},
		{diag.Warning, "NFR-001 is not covered by any task"},
	} {
		found := false
		for _, d := range ds {
			found = found || (d.Severity == w.sev && strings.Contains(d.Message, w.text))
		}
		if !found {
			t.Errorf("want %s %q, got:\n%v", w.sev, w.text, ds)
		}
	}
}

func TestNoGroups(t *testing.T) {
	p, ds := Parse([]byte("---\nid: x\n---\n# Plan\n## Notes\n"))
	ds = append(ds, Validate(p, Options{})...)
	if !ds.HasErrors() {
		t.Fatalf("a plan without groups must fail, got:\n%v", ds)
	}
}
