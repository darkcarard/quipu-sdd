// Package feature implements the use cases on one feature: loading its
// artifacts and state, linting, sync checks, creating artifacts from templates
// and approving them. The CLI is a thin layer over this package, and the
// MCP server reaches it through internal/app.
package feature

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/plan"
	"github.com/darkcarard/sdd-framework/internal/spec"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

// ErrNotFound is returned when a feature has neither a spec directory nor state.
var ErrNotFound = errors.New("feature not found")

// FileName returns the file name of an artifact inside specs/<feature>/.
func FileName(a workflow.Artifact) string { return string(a) + ".md" }

// File is the on-disk content of one artifact.
type File struct {
	Path    string // absolute
	Rel     string // relative to the project root, used in diagnostics
	Exists  bool
	Content []byte
	Hash    string // "" when missing
}

// Feature is a loaded feature: state, artifacts and derived statuses.
type Feature struct {
	Name   string
	Layout state.Layout
	Config config.Config

	State state.FeatureState
	// Saved reports whether state.json exists.
	Saved bool

	Files      map[workflow.Artifact]*File
	Functional *spec.Functional // nil when functional.md is missing
	Technical  *spec.Technical  // nil when technical.md is missing
	Plan       *plan.Plan       // nil when plan.md is missing
	parseDiags map[workflow.Artifact]diag.List

	Statuses map[workflow.Artifact]workflow.ArtifactStatus
	// Phase is the effective phase (see workflow.Effective); State.Phase is what was last saved.
	Phase workflow.Phase
}

// List returns the names of every feature in the project (from specs/ and .sdd/features/), sorted.
func List(l state.Layout) ([]string, error) {
	seen := map[string]bool{}
	for _, dir := range []string{l.SpecsDir(), l.FeaturesDir()} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				seen[e.Name()] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// Load reads the state and artifacts of a feature and derives their statuses.
func Load(l state.Layout, cfg config.Config, name string) (*Feature, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	f := &Feature{Name: name, Layout: l, Config: cfg}
	st, err := l.Load(name)
	switch {
	case err == nil:
		f.State, f.Saved = st, true
	case errors.Is(err, state.ErrNoFeature):
		if _, serr := os.Stat(l.SpecDir(name)); serr != nil {
			return nil, fmt.Errorf("%w: %s (create it with `sdd spec new %s`)", ErrNotFound, name, name)
		}
		f.State = state.NewFeatureState(name)
	default:
		return nil, err
	}
	if err := f.reload(); err != nil {
		return nil, err
	}
	return f, nil
}

// reload re-reads the artifacts and recomputes statuses and the effective phase.
func (f *Feature) reload() error {
	f.Files = map[workflow.Artifact]*File{}
	f.parseDiags = map[workflow.Artifact]diag.List{}
	f.Functional, f.Technical, f.Plan = nil, nil, nil
	current := map[workflow.Artifact]string{}
	for _, a := range workflow.Artifacts() {
		path := filepath.Join(f.Layout.SpecDir(f.Name), FileName(a))
		file := &File{Path: path, Rel: filepath.ToSlash(filepath.Join("specs", f.Name, FileName(a)))}
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			file.Exists, file.Content, file.Hash = true, b, workflow.Hash(b)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("reading %s: %w", file.Rel, err)
		}
		f.Files[a] = file
		current[a] = file.Hash
		if !file.Exists {
			continue
		}
		var ds diag.List
		switch a {
		case workflow.Functional:
			f.Functional, ds = spec.ParseFunctional(b)
		case workflow.Technical:
			f.Technical, ds = spec.ParseTechnical(b)
		case workflow.Plan:
			f.Plan, ds = plan.Parse(b)
		}
		f.parseDiags[a] = ds.InFile(file.Rel)
	}
	f.Statuses = workflow.Evaluate(f.State.Approvals, current)
	f.Phase = workflow.Effective(f.State.Phase, f.Statuses)
	return nil
}

// Lint returns the deterministic diagnostics of one artifact (parse errors plus
// guardrails). The plan is also checked for coverage when functional.md exists.
func (f *Feature) Lint(a workflow.Artifact) diag.List {
	file := f.Files[a]
	if !file.Exists {
		return nil
	}
	ds := append(diag.List(nil), f.parseDiags[a]...)
	opts := spec.LintOptions{Feature: f.Name, PreviousIDs: f.State.Approvals[a].IDs}
	var more diag.List
	switch a {
	case workflow.Functional:
		more = spec.LintFunctional(f.Functional, opts)
	case workflow.Technical:
		opts.ProfileExists = func(lang string) bool {
			_, err := os.Stat(filepath.Join(f.Layout.ProfilesDir(), lang+".md"))
			return err == nil
		}
		more = spec.LintTechnical(f.Technical, opts)
	case workflow.Plan:
		more = plan.Validate(f.Plan, plan.Options{Feature: f.Name, MaxFilesPerTask: f.Config.Limits.MaxFilesPerTask})
		if f.Functional != nil {
			more = append(more, plan.CheckCoverage(f.Plan, f.Functional)...)
		}
		if f.Technical != nil {
			more = append(more, plan.CheckLayers(f.Plan, f.Technical.Architecture)...)
		}
	}
	return append(ds, more.InFile(file.Rel)...)
}

// Sync runs the spec↔spec and spec↔plan checks (docs/WORKFLOW.md, Phase 4):
// recorded source hashes, ID mappings and coverage, approval staleness and
// cross-feature dependencies.
func (f *Feature) Sync() diag.List {
	var ds diag.List
	fn, tech, pl := f.Files[workflow.Functional], f.Files[workflow.Technical], f.Files[workflow.Plan]

	if f.Functional != nil {
		for _, dep := range f.Functional.Meta.DependsOn {
			if _, err := os.Stat(f.Layout.SpecDir(dep)); err != nil {
				ds = append(ds, diag.Diagnostic{Severity: diag.Error, File: fn.Rel, Line: 1,
					Message: fmt.Sprintf("depends_on: feature %q does not exist in specs/", dep)})
			}
		}
	}
	if f.Technical != nil {
		if f.Functional == nil {
			ds = append(ds, errAt(tech.Rel, "technical.md exists but functional.md is missing"))
		} else {
			if f.Statuses[workflow.Technical] != workflow.StatusStale { // stale already explains a mismatch
				ds = append(ds, sourceHash(tech.Rel, "functional_hash", f.Technical.FunctionalHash, fn.Hash)...)
			}
			ds = append(ds, spec.CheckTechnical(f.Functional, f.Technical).InFile(tech.Rel)...)
		}
	}
	if f.Plan != nil {
		if f.Functional == nil || f.Technical == nil {
			ds = append(ds, errAt(pl.Rel, "plan.md exists but functional.md or technical.md is missing"))
		} else {
			if f.Statuses[workflow.Plan] != workflow.StatusStale {
				ds = append(ds, sourceHash(pl.Rel, "functional_hash", f.Plan.FunctionalHash, fn.Hash)...)
				ds = append(ds, sourceHash(pl.Rel, "technical_hash", f.Plan.TechnicalHash, tech.Hash)...)
			}
		}
		if f.Functional != nil {
			ds = append(ds, plan.CheckCoverage(f.Plan, f.Functional).InFile(pl.Rel)...)
		}
	}
	for _, a := range workflow.Artifacts() {
		file := f.Files[a]
		switch f.Statuses[a] {
		case workflow.StatusModified:
			ds = append(ds, errAt(file.Rel, fmt.Sprintf("%s was edited after it was approved: review it and run `%s`", FileName(a), ApproveCommand(f.Name, a))))
		case workflow.StatusStale:
			ds = append(ds, errAt(file.Rel, fmt.Sprintf("%s is stale: %s changed after it was approved; reconcile it and run `%s`",
				FileName(a), f.changedSources(a), ApproveCommand(f.Name, a))))
		}
	}
	return ds
}

func (f *Feature) changedSources(a workflow.Artifact) string {
	var names []string
	for _, src := range workflow.Sources(a) {
		if f.State.Approvals[a].SourceHashes[src] != f.Files[src].Hash {
			names = append(names, FileName(src))
		}
	}
	return strings.Join(names, " and ")
}

func errAt(file, msg string) diag.Diagnostic {
	return diag.Diagnostic{Severity: diag.Error, File: file, Line: 1, Message: msg}
}

func sourceHash(file, key, recorded, current string) diag.List {
	switch {
	case recorded == "":
		return diag.List{{Severity: diag.Warning, File: file, Line: 1,
			Message: fmt.Sprintf("%s is not set yet; it is filled in on approval", key)}}
	case recorded != current:
		return diag.List{{Severity: diag.Error, File: file, Line: 1,
			Message: fmt.Sprintf("%s does not match the current %s: it was written against an older version; reconcile and re-approve",
				key, strings.TrimSuffix(key, "_hash")+".md")}}
	}
	return nil
}

// ApproveCommand returns the command that approves an artifact.
func ApproveCommand(feature string, a workflow.Artifact) string {
	if a == workflow.Plan {
		return "sdd plan approve " + feature
	}
	return fmt.Sprintf("sdd spec approve %s --%s", feature, a)
}

// Journal opens the feature journal.
func (f *Feature) Journal() (*journal.Journal, error) {
	if err := os.MkdirAll(f.Layout.FeatureDir(f.Name), 0o755); err != nil {
		return nil, err
	}
	return journal.Open(f.Layout.JournalFile(f.Name))
}

// RenderLog regenerates log.md from journal.jsonl.
func (f *Feature) RenderLog() error {
	events, err := journal.Read(f.Layout.JournalFile(f.Name))
	if err != nil {
		return err
	}
	return state.WriteFileAtomic(f.Layout.LogFile(f.Name), []byte(journal.RenderLog(f.Name, events)), 0o644)
}

// Next returns the next command (or action) for the feature, as shown by `sdd status`.
func (f *Feature) Next() string {
	n := f.Name
	edit := func(a workflow.Artifact) string {
		switch f.Statuses[a] {
		case workflow.StatusStale:
			return fmt.Sprintf("reconcile specs/%s/%s with %s, then the user re-approves it with `%s`", n, FileName(a), f.changedSources(a), ApproveCommand(n, a))
		case workflow.StatusModified:
			return fmt.Sprintf("review the edits to specs/%s/%s, then the user re-approves it with `%s`", n, FileName(a), ApproveCommand(n, a))
		}
		return fmt.Sprintf("edit specs/%s/%s until `sdd spec lint %s` passes, then the user approves it with `%s`", n, FileName(a), n, ApproveCommand(n, a))
	}
	switch f.Phase {
	case workflow.FunctionalDrafting:
		if !f.Files[workflow.Functional].Exists {
			return fmt.Sprintf("draft the functional spec with your agent (`/sdd-spec %s` in Claude Code) or create it with `sdd spec new %s`", n, n)
		}
		return edit(workflow.Functional)
	case workflow.FunctionalApproved:
		return fmt.Sprintf("draft the technical spec with your agent (`/sdd-tech %s` in Claude Code) or create it with `sdd spec new %s --technical`", n, n)
	case workflow.TechnicalDrafting:
		return edit(workflow.Technical)
	case workflow.TechnicalApproved:
		return fmt.Sprintf("draft the plan with your agent (`/sdd-plan %s` in Claude Code) or create it with `sdd plan new %s`", n, n)
	case workflow.PlanDrafting:
		return edit(workflow.Plan)
	case workflow.PlanApproved:
		return fmt.Sprintf("start implementing with `sdd implement %s` (creates the worktree and branch sdd/%s), then `/sdd-implement %s` in Claude Code", n, n, n)
	case workflow.Implementing:
		for _, g := range f.State.Groups {
			switch g.Status {
			case workflow.GroupApproved:
				continue
			case workflow.GroupAwaitingReview:
				return fmt.Sprintf("review %s with `sdd review %s`, then `sdd approve %s` or `sdd revise %s -m \"<what to change>\"`", g.ID, n, n, n)
			case workflow.GroupFailed:
				return fmt.Sprintf("%s failed verification: inspect .sdd/worktrees/%s, then give the agent guidance with `sdd revise %s -m \"<guidance>\"`", g.ID, n, n)
			case workflow.GroupInProgress:
				return fmt.Sprintf("continue %s with your agent (`/sdd-implement %s` in Claude Code)", g.ID, n)
			}
			return fmt.Sprintf("start %s with your agent (`/sdd-implement %s` in Claude Code)", g.ID, n)
		}
		return fmt.Sprintf("continue with your agent (`/sdd-implement %s` in Claude Code)", n)
	case workflow.Implemented:
		return fmt.Sprintf("run `sdd validate %s` (integration tests and every consistency check; writes report.md)", n)
	case workflow.Validated:
		return fmt.Sprintf("merge branch sdd/%s into your branch (merge, PR or rebase), then remove the worktree with `sdd clean %s`", n, n)
	}
	return ""
}
