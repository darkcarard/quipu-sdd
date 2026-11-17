package spec

import (
	"reflect"
	"strings"
	"testing"

	"github.com/darkcarard/quipu-sdd/internal/diag"
	"github.com/darkcarard/quipu-sdd/internal/sddtest"
)

func lintFunctional(t *testing.T, src string, opts LintOptions) (*Functional, diag.List) {
	t.Helper()
	f, ds := ParseFunctional([]byte(src))
	return f, append(ds, LintFunctional(f, opts)...)
}

func TestParseFunctionalFixture(t *testing.T) {
	f, ds := lintFunctional(t, sddtest.Functional, LintOptions{Feature: sddtest.Feature})
	if len(ds) != 0 {
		t.Fatalf("fixture should lint clean, got:\n%v", ds)
	}
	if f.Meta.ID != "user-signup" || f.Meta.Title != "User Signup" {
		t.Errorf("meta = %+v", f.Meta)
	}
	if got := f.IDs(); !reflect.DeepEqual(got, []string{"FR-001", "AC-001", "AC-002", "FR-002", "AC-003", "NFR-001"}) {
		t.Errorf("IDs = %v", got)
	}
	ac, ok := f.Criterion("AC-002")
	if !ok || ac.RequirementID != "FR-001" || !strings.HasSuffix(ac.Text, "rejected with a clear message.") {
		t.Errorf("AC-002 continuation line not joined: %+v", ac)
	}
	if r, _ := f.Requirement("FR-001"); r.Title != "Sign up with email" || !strings.Contains(r.Text, "must create") {
		t.Errorf("FR-001 = %+v", r)
	}
	if len(f.OpenQuestions) != 0 {
		t.Errorf("open questions = %v", f.OpenQuestions)
	}
}

// want describes an expected diagnostic.
type want struct {
	sev  diag.Severity
	text string
}

func hasDiag(ds diag.List, w want) bool {
	for _, d := range ds {
		if d.Severity == w.sev && strings.Contains(d.Message, w.text) {
			return true
		}
	}
	return false
}

func TestLintFunctional(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		opts     LintOptions
		want     want
	}{
		{"missing section", "## Glossary", "## Vocabulary", LintOptions{},
			want{diag.Error, `missing required section "## Glossary"`}},
		{"open question", "<!-- Must be empty before approval. -->", "- Captcha?", LintOptions{},
			want{diag.Error, "Open Questions must be empty"}},
		{"placeholder", "Social login.", "<non-goal>", LintOptions{},
			want{diag.Error, "template placeholder <non-goal>"}},
		{"FR without AC", "- **AC-003:** Given a new account, when it is created, then a confirmation email is sent.", "", LintOptions{},
			want{diag.Error, "FR-002 has no acceptance criteria"}},
		{"AC not GWT", "Given a new account, when it is created, then a confirmation email is sent.", "A mail is sent.", LintOptions{},
			want{diag.Error, "AC-003 must be written as Given"}},
		{"no must/should", "The system must send", "The system sends", LintOptions{},
			want{diag.Warning, "FR-002 should be a single testable statement"}},
		{"malformed ID", "**NFR-001:**", "**NFR-1:**", LintOptions{},
			want{diag.Error, "malformed ID NFR-1"}},
		{"duplicate ID", "**AC-003:**", "**AC-001:**", LintOptions{},
			want{diag.Error, "AC-001 is defined twice"}},
		{"numbering gap", "**AC-003:**", "**AC-004:**", LintOptions{},
			want{diag.Error, "AC numbering has gaps (AC-003 missing)"}},
		{"undefined reference", "(see AC-001)", "(see AC-009)", LintOptions{},
			want{diag.Error, "AC-009 is referenced but not defined"}},
		{"deleted since approval", "## Glossary", "## Glossary", LintOptions{PreviousIDs: []string{"FR-001", "FR-003"}},
			want{diag.Error, "FR-003 was approved before and is now gone"}},
		{"frontmatter id", "id: user-signup", "id: other", LintOptions{Feature: "user-signup"},
			want{diag.Error, `id "other" does not match`}},
		{"not measurable", "Signup responds in p95 < 300 ms.", "Signup is fast.", LintOptions{},
			want{diag.Warning, "NFR-001 should be measurable"}},
		{"implementation detail", "Social login.", "Store users in PostgreSQL.", LintOptions{},
			want{diag.Warning, `"PostgreSQL" looks like an implementation detail`}},
		{"AC outside FR", "- **NFR-001:**", "- **AC-004:** Given x, when y, then z.\n- **NFR-001:**", LintOptions{},
			want{diag.Error, "AC-004 must be defined under an FR"}},
		{"missing non-goals", "## Non-Goals\n\n- Social login.\n", "", LintOptions{},
			want{diag.Error, `missing required section "## Non-Goals"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(sddtest.Functional, tt.old, tt.new, 1)
			if src == sddtest.Functional && tt.old != tt.new {
				t.Fatalf("fixture does not contain %q", tt.old)
			}
			_, ds := lintFunctional(t, src, tt.opts)
			if !hasDiag(ds, tt.want) {
				t.Errorf("want %s %q, got:\n%v", tt.want.sev, tt.want.text, ds)
			}
		})
	}
}

func TestLintFunctionalSectionOrder(t *testing.T) {
	src := strings.Replace(sddtest.Functional, "## Summary\n", "", 1)
	src = strings.Replace(src, "## Glossary\n", "## Glossary\n\n## Summary\n", 1)
	_, ds := lintFunctional(t, src, LintOptions{})
	if !hasDiag(ds, want{diag.Warning, "out of order"}) || ds.HasErrors() {
		t.Errorf("want only an out-of-order warning, got:\n%v", ds)
	}
}

func TestRetiredIDs(t *testing.T) {
	src := strings.Replace(sddtest.Functional, "### FR-002: Confirm email", "### ~~FR-002~~ (removed)", 1)
	src = strings.Replace(src, "- **AC-003:** Given", "- ~~AC-003~~ Given", 1)
	f, ds := lintFunctional(t, src, LintOptions{PreviousIDs: []string{"FR-002", "AC-003"}})
	if ds.HasErrors() {
		t.Fatalf("retiring IDs must not be an error:\n%v", ds)
	}
	if got := f.Active(KindFR, KindAC); !reflect.DeepEqual(got, []string{"FR-001", "AC-001", "AC-002"}) {
		t.Errorf("active = %v", got)
	}
	if d, _ := f.Lookup("FR-002"); !d.Retired {
		t.Error("FR-002 should be retired")
	}
}

func TestLintTechnical(t *testing.T) {
	opts := LintOptions{Feature: sddtest.Feature, ProfileExists: func(string) bool { return true }}
	tech, ds := ParseTechnical([]byte(sddtest.Technical))
	if ds = append(ds, LintTechnical(tech, opts)...); len(ds) != 0 {
		t.Fatalf("fixture should lint clean, got:\n%v", ds)
	}
	if tech.Language != "go" || tech.Commands.Test != "go test ./..." {
		t.Errorf("frontmatter = %+v", tech)
	}
	if got := tech.Components("FR-001"); !reflect.DeepEqual(got, []string{"SignupService", "HTTP handler"}) {
		t.Errorf("components = %v", got)
	}
	if got := tech.TestLevels("AC-002"); !reflect.DeepEqual(got, []string{"unit", "integration"}) {
		t.Errorf("levels = %v", got)
	}

	tests := []struct {
		name, old, new string
		profile        bool
		want           want
	}{
		{"no language", "language: go", "language: \"\"", true, want{diag.Error, "language is required"}},
		{"no profile", "language: go", "language: rust", false, want{diag.Warning, "no profile .sdd/profiles/rust.md"}},
		{"no test command", "  test: go test ./...", "", true, want{diag.Error, "commands.test is required"}},
		{"no integration", "  integration: go test -tags=integration ./...", "", true, want{diag.Warning, "commands.integration is empty"}},
		{"bad level", "| AC-003 | integration |", "| AC-003 | smoke |", true, want{diag.Error, `unknown test level "smoke"`}},
		{"AC in traceability", "| NFR-001 | HTTP handler |", "| AC-001 | HTTP handler |", true, want{diag.Error, "AC-001 does not belong in Requirement Traceability"}},
		{"no component", "| FR-002 | Mailer |", "| FR-002 | |", true, want{diag.Error, "FR-002 has no component"}},
		{"ADR without consequences", "- **Consequences:** slower logins.", "", true, want{diag.Warning, "ADR-001 should record its Consequences"}},
		{"undefined ADR", "## Risks", "See ADR-002.\n\n## Risks", true, want{diag.Error, "ADR-002 is referenced but not defined"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(sddtest.Technical, tt.old, tt.new, 1)
			if src == sddtest.Technical {
				t.Fatalf("fixture does not contain %q", tt.old)
			}
			tech, ds := ParseTechnical([]byte(src))
			o := opts
			o.ProfileExists = func(string) bool { return tt.profile }
			ds = append(ds, LintTechnical(tech, o)...)
			if !hasDiag(ds, tt.want) {
				t.Errorf("want %s %q, got:\n%v", tt.want.sev, tt.want.text, ds)
			}
		})
	}
}

func TestCheckTechnical(t *testing.T) {
	f, _ := ParseFunctional([]byte(sddtest.Functional))
	tech, _ := ParseTechnical([]byte(sddtest.Technical))
	if ds := CheckTechnical(f, tech); len(ds) != 0 {
		t.Fatalf("fixtures should be in sync, got:\n%v", ds)
	}

	src := strings.Replace(sddtest.Technical, "| FR-002 | Mailer | |\n", "", 1)
	src = strings.Replace(src, "| AC-003 | integration |", "| AC-004 | integration |", 1)
	tech, _ = ParseTechnical([]byte(src))
	ds := CheckTechnical(f, tech)
	for _, w := range []want{
		{diag.Error, "FR-002 is not mapped to any component"},
		{diag.Error, "AC-003 is not mapped to a test level"},
		{diag.Error, "AC-004 is not defined in functional.md"},
	} {
		if !hasDiag(ds, w) {
			t.Errorf("want %q, got:\n%v", w.text, ds)
		}
	}
}

func TestIDHelpers(t *testing.T) {
	for id, valid := range map[string]bool{"FR-001": true, "NFR-012": true, "AC-1": false, "ADR-0001": false, "XR-001": false} {
		if ValidID(id) != valid {
			t.Errorf("ValidID(%s) = %v", id, !valid)
		}
	}
	ids := []string{"AC-002", "FR-010", "NFR-001", "FR-002", "ADR-001"}
	SortIDs(ids)
	if !reflect.DeepEqual(ids, []string{"FR-002", "FR-010", "NFR-001", "AC-002", "ADR-001"}) {
		t.Errorf("SortIDs = %v", ids)
	}
}
