package plan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/spec"
)

// Options configure Validate.
type Options struct {
	// Feature is the expected frontmatter id; empty skips the check.
	Feature string
	// MaxFilesPerTask bounds task size (limits.max_files_per_task); 0 skips the check.
	MaxFilesPerTask int
}

// Recommended number of groups per feature (a warning outside this range).
const (
	MinGroups = 3
	MaxGroups = 8
)

// Validate runs the deterministic plan checks of docs/WORKFLOW.md (Phase 2).
func Validate(p *Plan, opts Options) diag.List {
	var ds diag.List
	if opts.Feature != "" && p.ID != "" && p.ID != opts.Feature {
		ds.Errorf(1, "frontmatter: id %q does not match the feature directory %q", p.ID, opts.Feature)
	}
	if len(p.Groups) == 0 {
		ds.Errorf(0, "the plan has no groups (\"## G1: <title>\")")
		return append(ds, spec.Placeholders(p.lines)...)
	}
	if n := len(p.Groups); n < MinGroups || n > MaxGroups {
		ds.Warnf(0, "the plan has %d groups; keep features to roughly %d–%d groups and split larger work", n, MinGroups, MaxGroups)
	}

	seenGroups := map[string]int{}
	seenTasks := map[string]int{}
	var layers []string
	for gi, g := range p.Groups {
		want := fmt.Sprintf("G%d", gi+1)
		if prev, dup := seenGroups[g.ID]; dup {
			ds.Errorf(g.Line, "%s is defined twice (first at line %d)", g.ID, prev)
		} else if g.ID != want {
			ds.Errorf(g.Line, "group %s is out of sequence: expected %s (groups are numbered G1, G2, … in order)", g.ID, want)
		}
		seenGroups[g.ID] = g.Line

		if g.Title == "" {
			ds.Errorf(g.Line, "%s has no title", g.ID)
		}
		if strings.TrimSpace(g.Layer) == "" {
			ds.Errorf(g.Line, "%s has no layer (\"- layer: domain\")", g.ID)
		} else {
			if i := slices.Index(layers, g.Layer); i >= 0 && layers[len(layers)-1] != g.Layer {
				ds.Warnf(g.fields["layer"], "%s returns to layer %q after %q: order groups by layer", g.ID, g.Layer, layers[len(layers)-1])
			}
			layers = append(layers, g.Layer)
		}
		if _, ok := g.fields["depends_on"]; !ok {
			ds.Warnf(g.Line, "%s does not declare depends_on (use \"- depends_on: []\")", g.ID)
		}
		if strings.TrimSpace(g.Goal) == "" {
			ds.Warnf(g.Line, "%s has no goal", g.ID)
		}
		for _, dep := range g.DependsOn {
			line := g.fields["depends_on"]
			switch _, earlier := seenGroups[dep]; {
			case dep == g.ID:
				ds.Errorf(line, "%s depends on itself", g.ID)
			case earlier:
			case groupIndex(p, dep) >= 0:
				ds.Errorf(line, "%s depends on %s, which comes later: depends_on may only reference earlier groups", g.ID, dep)
			default:
				ds.Errorf(line, "%s depends on unknown group %s", g.ID, dep)
			}
		}

		if len(g.Tasks) == 0 {
			ds.Errorf(g.Line, "%s has no tasks (\"### T%d.1: <title>\")", g.ID, gi+1)
		}
		for ti, t := range g.Tasks {
			if prev, dup := seenTasks[t.ID]; dup {
				ds.Errorf(t.Line, "%s is defined twice (first at line %d)", t.ID, prev)
			} else if wantT := fmt.Sprintf("T%s.%d", strings.TrimPrefix(g.ID, "G"), ti+1); t.ID != wantT {
				ds.Errorf(t.Line, "task %s is out of sequence: expected %s (tasks are numbered T<group>.<n>)", t.ID, wantT)
			}
			seenTasks[t.ID] = t.Line
			validateTask(t, opts, &ds)
		}
	}

	return append(ds, spec.Placeholders(p.lines)...)
}

// CheckCoverage runs the spec↔plan checks: every task covers only IDs defined
// and active in the functional spec, and every active FR and AC is covered by
// at least one task (an uncovered NFR is a warning).
func CheckCoverage(p *Plan, f *spec.Functional) diag.List {
	var ds diag.List
	for _, g := range p.Groups {
		for _, t := range g.Tasks {
			for _, id := range t.Covers {
				if !spec.ValidID(id) || spec.KindOf(id) == spec.KindADR {
					continue // reported by Validate
				}
				switch d, ok := f.Lookup(id); {
				case !ok:
					ds.Errorf(t.fields["covers"], "%s covers %s, which is not defined in functional.md", t.ID, id)
				case d.Retired:
					ds.Errorf(t.fields["covers"], "%s covers %s, which is retired", t.ID, id)
				}
			}
		}
	}
	covered := p.Covered()
	for _, id := range f.Active(spec.KindFR, spec.KindAC) {
		if len(covered[id]) == 0 {
			ds.Errorf(0, "%s is not covered by any task", id)
		}
	}
	for _, id := range f.Active(spec.KindNFR) {
		if len(covered[id]) == 0 {
			ds.Warnf(0, "%s is not covered by any task", id)
		}
	}
	return ds
}

func validateTask(t Task, opts Options, ds *diag.List) {
	if t.Title == "" {
		ds.Errorf(t.Line, "%s has no title", t.ID)
	}
	for _, f := range []string{"covers", "files", "done_when", "verify"} {
		if _, ok := t.fields[f]; !ok {
			ds.Errorf(t.Line, "%s is missing \"- %s:\"", t.ID, f)
		}
	}
	if _, ok := t.fields["covers"]; ok && len(t.Covers) == 0 {
		ds.Warnf(t.fields["covers"], "%s covers no requirement or criterion", t.ID)
	}
	if _, ok := t.fields["files"]; ok && len(t.Files) == 0 {
		ds.Errorf(t.fields["files"], "%s lists no files", t.ID)
	}
	if _, ok := t.fields["done_when"]; ok && t.DoneWhen == "" {
		ds.Errorf(t.fields["done_when"], "%s has an empty done_when", t.ID)
	}
	if _, ok := t.fields["verify"]; ok && t.Verify == "" {
		ds.Errorf(t.fields["verify"], "%s has an empty verify", t.ID)
	}
	if opts.MaxFilesPerTask > 0 && len(t.Files) > opts.MaxFilesPerTask {
		ds.Errorf(t.fields["files"], "%s touches %d files; the limit is %d (limits.max_files_per_task): split the task",
			t.ID, len(t.Files), opts.MaxFilesPerTask)
	}
	seen := map[string]bool{}
	for _, file := range t.Files {
		if seen[file] {
			ds.Warnf(t.fields["files"], "%s lists %s twice", t.ID, file)
		}
		seen[file] = true
	}
	for _, id := range t.Covers {
		line := t.fields["covers"]
		k := spec.KindOf(id)
		switch {
		case !spec.ValidID(id):
			ds.Errorf(line, "%s covers %q, which is not a spec ID (FR-###, NFR-###, AC-###)", t.ID, id)
			continue
		case k == spec.KindADR:
			ds.Errorf(line, "%s covers %s: tasks cover requirements and criteria, not decisions", t.ID, id)
		}
	}
}

func groupIndex(p *Plan, id string) int {
	for i, g := range p.Groups {
		if g.ID == id {
			return i
		}
	}
	return -1
}
