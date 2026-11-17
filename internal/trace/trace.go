// Package trace builds the requirement → technical → plan → commit → test chain
// used by `sdd trace` and `sdd sync check` (docs/TRACEABILITY.md §1).
// Spec and plan links are built here; commit and test links arrive in M6.
package trace

import (
	"fmt"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/plan"
	"github.com/darkcarard/sdd-framework/internal/spec"
)

// Kind is the kind of artifact an entry or link points to.
type Kind string

const (
	KindRequirement Kind = "requirement" // FR/NFR in functional.md
	KindCriterion   Kind = "criterion"   // AC in functional.md
	KindDecision    Kind = "decision"    // ADR in technical.md
	KindComponent   Kind = "component"   // technical.md traceability
	KindGroup       Kind = "group"       // G# in plan.md
	KindTask        Kind = "task"        // T#.# in plan.md
	KindCommit      Kind = "commit"      // git commit with SDD trailers (M6)
	KindTest        Kind = "test"        // test referencing `covers: AC-###` (M6)
)

// Entry is one ID of a feature and everything linked to it.
type Entry struct {
	ID       string
	Kind     Kind
	Feature  string
	Title    string
	Location string // file:line
	Retired  bool
	// Parent is the FR of an AC, or the group of a task.
	Parent string
	// Children are the ACs of an FR, or the tasks of a group.
	Children []string
	// Components (FR/NFR) or test levels (AC) from technical.md.
	Technical []string
	// Tasks covering an FR, NFR or AC.
	Tasks []string
	// Covers and Files of a task; DependsOn and Layer of a group.
	Covers    []string
	Files     []string
	DependsOn []string
	Layer     string
}

// Index holds every traced ID of one feature, in document order.
type Index struct {
	Feature string
	entries []Entry
	byID    map[string]int
}

// Files locates the feature artifacts, for Location fields.
type Files struct {
	Functional, Technical, Plan string
}

// Build indexes a feature. Any of f, t and p may be nil.
func Build(feature string, files Files, f *spec.Functional, t *spec.Technical, p *plan.Plan) *Index {
	ix := &Index{Feature: feature, byID: map[string]int{}}
	var covered map[string][]string
	if p != nil {
		covered = p.Covered()
	}
	tech := func(id string, levels bool) []string {
		if t == nil {
			return nil
		}
		if levels {
			return t.TestLevels(id)
		}
		return t.Components(id)
	}
	if f != nil {
		for _, r := range f.Requirements {
			e := Entry{ID: r.ID, Kind: KindRequirement, Title: r.Title, Location: loc(files.Functional, r.Line),
				Retired: r.Retired, Technical: tech(r.ID, false), Tasks: covered[r.ID]}
			for _, c := range r.Criteria {
				e.Children = append(e.Children, c.ID)
			}
			ix.add(e)
		}
		for _, r := range f.NFRs {
			title := r.Title
			if title == "" {
				title = r.Text
			}
			ix.add(Entry{ID: r.ID, Kind: KindRequirement, Title: title, Location: loc(files.Functional, r.Line),
				Retired: r.Retired, Technical: tech(r.ID, false), Tasks: covered[r.ID]})
		}
		for _, r := range f.Requirements {
			for _, c := range r.Criteria {
				ix.add(Entry{ID: c.ID, Kind: KindCriterion, Title: c.Text, Location: loc(files.Functional, c.Line),
					Retired: c.Retired, Parent: r.ID, Technical: tech(c.ID, true), Tasks: covered[c.ID]})
			}
		}
	}
	if t != nil {
		for _, d := range t.Decisions {
			ix.add(Entry{ID: d.ID, Kind: KindDecision, Title: d.Title, Location: loc(files.Technical, d.Line), Retired: d.Retired})
		}
	}
	if p != nil {
		for _, g := range p.Groups {
			e := Entry{ID: g.ID, Kind: KindGroup, Title: g.Title, Location: loc(files.Plan, g.Line),
				DependsOn: g.DependsOn, Layer: g.Layer}
			for _, tk := range g.Tasks {
				e.Children = append(e.Children, tk.ID)
			}
			ix.add(e)
			for _, tk := range g.Tasks {
				ix.add(Entry{ID: tk.ID, Kind: KindTask, Title: tk.Title, Location: loc(files.Plan, tk.Line),
					Parent: g.ID, Covers: tk.Covers, Files: tk.Files})
			}
		}
	}
	return ix
}

func loc(file string, line int) string {
	if file == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", file, line)
}

func (ix *Index) add(e Entry) {
	e.Feature = ix.Feature
	if _, dup := ix.byID[e.ID]; dup {
		return // duplicates are reported by lint; keep the first definition
	}
	ix.byID[e.ID] = len(ix.entries)
	ix.entries = append(ix.entries, e)
}

// Lookup returns the entry for id.
func (ix *Index) Lookup(id string) (Entry, bool) {
	i, ok := ix.byID[id]
	if !ok {
		return Entry{}, false
	}
	return ix.entries[i], true
}

// Entries returns every entry in document order.
func (ix *Index) Entries() []Entry { return append([]Entry(nil), ix.entries...) }

// Render prints an entry as a tree:
//
//	AC-003  (specs/user-signup/functional.md:42 · FR-001)
//	  ├─ technical: unit, integration
//	  └─ plan:      T2.1, T4.2
func (e Entry) Render() string {
	var sb strings.Builder
	head := e.ID
	if e.Retired {
		head += " (retired)"
	}
	where := e.Feature + " · " + e.Location
	if e.Parent != "" {
		where += " · " + e.Parent
	}
	if e.Title != "" && e.Kind != KindCriterion {
		fmt.Fprintf(&sb, "%s  %s  (%s)\n", head, e.Title, where)
	} else {
		fmt.Fprintf(&sb, "%s  (%s)\n", head, where)
	}
	type row struct{ label, value string }
	var rows []row
	list := func(xs []string) string {
		if len(xs) == 0 {
			return "(none)"
		}
		return strings.Join(xs, ", ")
	}
	switch e.Kind {
	case KindRequirement:
		if strings.HasPrefix(e.ID, "FR-") {
			rows = append(rows, row{"criteria", list(e.Children)})
		}
		rows = append(rows, row{"technical", list(e.Technical)}, row{"plan", list(e.Tasks)})
	case KindCriterion:
		if e.Title != "" {
			rows = append(rows, row{"text", e.Title})
		}
		rows = append(rows, row{"technical", list(e.Technical)}, row{"plan", list(e.Tasks)})
	case KindGroup:
		rows = append(rows, row{"layer", e.Layer}, row{"depends_on", list(e.DependsOn)}, row{"tasks", list(e.Children)})
	case KindTask:
		rows = append(rows, row{"covers", list(e.Covers)}, row{"files", list(e.Files)})
	}
	if e.Kind == KindRequirement || e.Kind == KindCriterion {
		rows = append(rows, row{"commits", "(linked in M6)"}, row{"tests", "(linked in M6)"})
	}
	for i, r := range rows {
		branch := "├─"
		if i == len(rows)-1 {
			branch = "└─"
		}
		fmt.Fprintf(&sb, "  %s %-11s%s\n", branch, r.label+":", r.value)
	}
	return sb.String()
}

// Matrix renders the requirement × artifact matrix of the feature in markdown.
// Gaps in active rows are marked **missing**.
func (ix *Index) Matrix() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Traceability matrix: %s\n\n", ix.Feature)
	sb.WriteString("| ID | Parent | Title | Technical | Plan |\n|---|---|---|---|---|\n")
	gap := func(e Entry, xs []string) string {
		switch {
		case len(xs) > 0:
			return strings.Join(xs, ", ")
		case e.Retired:
			return "—"
		}
		return "**missing**"
	}
	for _, e := range ix.entries {
		if e.Kind != KindRequirement && e.Kind != KindCriterion {
			continue
		}
		id := e.ID
		if e.Retired {
			id = "~~" + id + "~~"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n", id, cell(e.Parent), cell(e.Title), gap(e, e.Technical), gap(e, e.Tasks))
	}
	sb.WriteString("\nCommits and tests are linked from Phase 4 (M6).\n")
	return sb.String()
}

func cell(s string) string {
	if s == "" {
		return "—"
	}
	s = strings.ReplaceAll(s, "|", `\|`)
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

// Gap is a missing link reported by sync checks (e.g. an AC with no test).
type Gap struct {
	ID      string
	Missing Kind
	Message string
}
