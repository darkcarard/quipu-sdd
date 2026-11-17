package trace

import (
	"reflect"
	"strings"
	"testing"

	"github.com/darkcarard/sdd-framework/internal/plan"
	"github.com/darkcarard/sdd-framework/internal/sddtest"
	"github.com/darkcarard/sdd-framework/internal/spec"
)

func index(t *testing.T, technical string) *Index {
	t.Helper()
	f, _ := spec.ParseFunctional([]byte(sddtest.Functional))
	tech, _ := spec.ParseTechnical([]byte(technical))
	p, _ := plan.Parse([]byte(sddtest.Plan))
	return Build(sddtest.Feature, Files{Functional: "f.md", Technical: "t.md", Plan: "p.md"}, f, tech, p)
}

func TestBuildAndLookup(t *testing.T) {
	ix := index(t, sddtest.Technical)
	tests := []struct {
		id        string
		kind      Kind
		parent    string
		technical []string
		tasks     []string
		children  []string
	}{
		{"FR-001", KindRequirement, "", []string{"SignupService", "HTTP handler"}, []string{"T1.1", "T2.1"}, []string{"AC-001", "AC-002"}},
		{"NFR-001", KindRequirement, "", []string{"HTTP handler"}, []string{"T2.1"}, nil},
		{"AC-002", KindCriterion, "FR-001", []string{"unit", "integration"}, []string{"T2.1"}, nil},
		{"G2", KindGroup, "", nil, nil, []string{"T2.1"}},
		{"T3.1", KindTask, "G3", nil, nil, nil},
		{"ADR-001", KindDecision, "", nil, nil, nil},
	}
	for _, tt := range tests {
		e, ok := ix.Lookup(tt.id)
		if !ok {
			t.Errorf("%s not found", tt.id)
			continue
		}
		if e.Kind != tt.kind || e.Parent != tt.parent || !reflect.DeepEqual(e.Technical, tt.technical) ||
			!reflect.DeepEqual(e.Tasks, tt.tasks) || !reflect.DeepEqual(e.Children, tt.children) {
			t.Errorf("%s = %+v", tt.id, e)
		}
	}
	if _, ok := ix.Lookup("FR-999"); ok {
		t.Error("IDs in HTML comments must not be indexed")
	}
	out := mustLookup(t, ix, "AC-002").Render()
	for _, s := range []string{"AC-002  (user-signup · f.md:", "· FR-001)", "├─ technical: unit, integration", "plan:      T2.1", "└─ tests:"} {
		if !strings.Contains(out, s) {
			t.Errorf("Render missing %q:\n%s", s, out)
		}
	}
}

func mustLookup(t *testing.T, ix *Index, id string) Entry {
	t.Helper()
	e, ok := ix.Lookup(id)
	if !ok {
		t.Fatalf("%s not found", id)
	}
	return e
}

func TestMatrixMarksGaps(t *testing.T) {
	ix := index(t, strings.Replace(sddtest.Technical, "| FR-002 | Mailer | |\n", "", 1))
	m := ix.Matrix()
	if !strings.Contains(m, "| FR-002 | — | Confirm email | **missing** | T3.1 |") {
		t.Errorf("FR-002 gap not marked:\n%s", m)
	}
	if !strings.Contains(m, "| AC-001 | FR-001 |") || strings.Contains(m, "| G1 ") {
		t.Errorf("matrix should list requirements and criteria only:\n%s", m)
	}
}
