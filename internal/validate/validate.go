// Package validate runs Phase 4 (docs/WORKFLOW.md): the plan↔code checks
// (one commit per group with matching trailers, every acceptance criterion
// referenced by a test), the integration tests, and the validation report
// .sdd/features/<feature>/report.md.
package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/spec"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/trace"
	"github.com/darkcarard/sdd-framework/internal/verify"
	"github.com/darkcarard/sdd-framework/internal/workflow"
	"github.com/darkcarard/sdd-framework/internal/workspace"
)

// TestRef is a `covers:` reference found in a tracked file.
type TestRef struct {
	File string
	Line int
	IDs  []string
}

var (
	coversRe = regexp.MustCompile(`(?i)covers:\s*(.*)`)
	idRe     = regexp.MustCompile(`\b(?:FR|NFR|AC)-\d{3}\b`)
)

// ScanTests finds `covers: AC-###` references in the files of dir that git
// sees (tracked or untracked, not ignored), outside specs/ and .sdd/.
func ScanTests(ctx context.Context, dir string) ([]TestRef, error) {
	out, err := workspace.Git{Dir: dir}.Run(ctx, "grep", "--untracked", "-n", "-I", "-i", "-E", "covers:",
		"--", ".", ":(exclude)specs", ":(exclude).sdd")
	if err != nil {
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil // no matches
		}
		return nil, err
	}
	var refs []TestRef
	for _, line := range strings.Split(out, "\n") {
		file, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		num, text, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		m := coversRe.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		if ids := idRe.FindAllString(m[1], -1); len(ids) > 0 {
			refs = append(refs, TestRef{File: file, Line: n, IDs: ids})
		}
	}
	return refs, nil
}

// codeDir is where the feature's code is: its worktree, or (after the branch
// was merged and the worktree removed) the project itself.
func codeDir(f *feature.Feature) string {
	wt := f.Layout.WorktreeDir(f.Name)
	if _, err := os.Stat(wt); err == nil {
		return wt
	}
	return f.Layout.Root
}

// started reports whether any group has started.
func started(f *feature.Feature) bool {
	for _, g := range f.State.Groups {
		if g.BaseSHA != "" {
			return true
		}
	}
	return false
}

// CheckCode runs the plan↔code checks and returns the implementation links of
// every ID (for trace and the report). It does nothing before implementation starts.
func CheckCode(ctx context.Context, f *feature.Feature) (diag.List, trace.Links, error) {
	links := trace.Links{Commits: map[string][]string{}, Tests: map[string][]string{}}
	if f.Plan == nil || !started(f) {
		return nil, links, nil
	}
	var ds diag.List
	root := workspace.Git{Dir: f.Layout.Root}
	branch := workspace.Branch(f.Name)
	ref := branch
	if _, err := root.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		ref = "HEAD" // the branch was merged and deleted
	}
	where := "branch " + branch

	base := f.State.Groups[0].BaseSHA
	commits, err := root.Log(ctx, base+".."+ref)
	if err != nil {
		return nil, links, err
	}
	for _, c := range commits {
		if c.Trailers["SDD-Feature"] == "" && ref == branch {
			ds = append(ds, diag.Diagnostic{Severity: diag.Warning, File: where,
				Message: fmt.Sprintf("commit %s %q has no SDD trailers: it was made outside sdd", short(c.SHA), c.Subject)})
		}
	}

	for _, g := range f.State.Groups {
		if g.CommitSHA == "" {
			continue
		}
		pg, _ := f.Plan.Group(g.ID)
		var tasks, covers []string
		for _, t := range pg.Tasks {
			tasks = append(tasks, t.ID)
			for _, id := range t.Covers {
				if !slices.Contains(covers, id) {
					covers = append(covers, id)
				}
			}
		}
		spec.SortIDs(covers)
		var matches []workspace.Commit
		for _, c := range commits {
			if c.Trailers["SDD-Feature"] == f.Name && c.Trailers["SDD-Group"] == g.ID {
				matches = append(matches, c)
			}
		}
		switch len(matches) {
		case 0:
			if g.Status == workflow.GroupApproved {
				ds = append(ds, diag.Diagnostic{Severity: diag.Error, File: where,
					Message: fmt.Sprintf("%s has no commit with SDD-Group: %s trailers (expected %s)", g.ID, g.ID, short(g.CommitSHA))})
			}
			continue
		case 1:
		default:
			ds = append(ds, diag.Diagnostic{Severity: diag.Error, File: where,
				Message: fmt.Sprintf("%s has %d commits; a group is squashed into exactly one", g.ID, len(matches))})
		}
		c := matches[0]
		if got := c.Trailers["SDD-Tasks"]; got != strings.Join(tasks, ", ") {
			ds = append(ds, diag.Diagnostic{Severity: diag.Error, File: where,
				Message: fmt.Sprintf("%s commit %s lists tasks %q, the plan has %q", g.ID, short(c.SHA), got, strings.Join(tasks, ", "))})
		}
		if got := c.Trailers["SDD-Covers"]; got != strings.Join(covers, ", ") {
			ds = append(ds, diag.Diagnostic{Severity: diag.Warning, File: where,
				Message: fmt.Sprintf("%s commit %s covers %q, the plan's tasks cover %q", g.ID, short(c.SHA), got, strings.Join(covers, ", "))})
		}
		label := short(c.SHA) + " " + c.Subject
		for _, id := range append(append([]string{g.ID}, tasks...), covers...) {
			links.Commits[id] = append(links.Commits[id], label)
		}
	}

	refs, err := ScanTests(ctx, codeDir(f))
	if err != nil {
		return ds, links, err
	}
	for _, r := range refs {
		for _, id := range r.IDs {
			loc := fmt.Sprintf("%s:%d", r.File, r.Line)
			if !slices.Contains(links.Tests[id], loc) {
				links.Tests[id] = append(links.Tests[id], loc)
			}
			if f.Functional == nil {
				continue
			}
			if d, ok := f.Functional.Lookup(id); !ok {
				ds = append(ds, diag.Diagnostic{Severity: diag.Warning, File: r.File, Line: r.Line,
					Message: fmt.Sprintf("covers %s, which is not defined in specs/%s/functional.md", id, f.Name)})
			} else if d.Retired {
				ds = append(ds, diag.Diagnostic{Severity: diag.Warning, File: r.File, Line: r.Line, Message: fmt.Sprintf("covers %s, which is retired", id)})
			}
		}
	}

	// Every criterion of an approved group must be referenced by a test.
	approved := map[string]bool{}
	for _, g := range f.State.Groups {
		if g.Status != workflow.GroupApproved {
			continue
		}
		pg, _ := f.Plan.Group(g.ID)
		for _, t := range pg.Tasks {
			for _, id := range t.Covers {
				approved[id] = true
			}
		}
	}
	if f.Functional != nil {
		for _, id := range f.Functional.Active(spec.KindAC) {
			if approved[id] && len(links.Tests[id]) == 0 {
				ds = append(ds, diag.Diagnostic{Severity: diag.Error, File: where,
					Message: fmt.Sprintf("%s is not referenced by any test (add `covers: %s` to the test that verifies it)", id, id)})
			}
		}
	}
	return ds, links, nil
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// Index builds the feature's trace index with its implementation links.
func Index(ctx context.Context, f *feature.Feature) (*trace.Index, diag.List, error) {
	ix := trace.Build(f.Name, trace.Files{
		Functional: f.Files[workflow.Functional].Rel,
		Technical:  f.Files[workflow.Technical].Rel,
		Plan:       f.Files[workflow.Plan].Rel,
	}, f.Functional, f.Technical, f.Plan)
	if !started(f) {
		return ix, nil, nil
	}
	ds, links, err := CheckCode(ctx, f)
	if err != nil {
		return ix, ds, err
	}
	ix.Link(links)
	return ix, ds, nil
}

// Result is the outcome of Validate.
type Result struct {
	Passed      bool
	Integration []verify.Result
	Skipped     bool // no integration command
	Diagnostics diag.List
	Report      string // path relative to the project root
	Head        string
	Next        string
}

// Validate runs the integration tests and every sync check of an implemented
// feature, writes report.md and, when everything passes, moves the feature to
// validated.
func Validate(ctx context.Context, f *feature.Feature, who feature.Identity, now time.Time, timeout time.Duration) (Result, error) {
	var out Result
	switch f.Phase {
	case workflow.Implemented, workflow.Validated:
	default:
		return out, fmt.Errorf("%s is in phase %s: validation needs every group approved (next: %s)", f.Name, f.Phase, f.Next())
	}
	if f.Technical == nil {
		return out, errors.New("technical.md is missing")
	}
	wt, _, err := workspace.Ensure(ctx, f.Layout.Root, f.Layout.WorktreeDir(f.Name), f.Name)
	if err != nil {
		return out, err
	}
	out.Head, _ = wt.Head(ctx)

	jr, err := f.Journal()
	if err != nil {
		return out, err
	}
	startedEv, err := jr.Start(journal.Event{Actor: who.Actor(), RunID: f.State.RunID, Phase: string(f.Phase), Action: "feature.validate"})
	if err != nil {
		return out, err
	}
	results, ok := verify.Run(ctx, wt.Dir, timeout, verify.Step{Name: "integration", Command: f.Technical.Commands.Integration})
	out.Integration, out.Skipped = results, len(results) == 0

	ix, codeDs, err := Index(ctx, f)
	if err != nil {
		_, _ = jr.End(startedEv, journal.Failed, journal.Event{Detail: err.Error()})
		return out, err
	}
	out.Diagnostics = append(f.Sync(), codeDs...).Sorted()
	if out.Skipped {
		out.Diagnostics = append(out.Diagnostics, diag.Diagnostic{Severity: diag.Warning, File: f.Files[workflow.Technical].Rel,
			Message: "commands.integration is empty: no integration tests were run"})
	}
	out.Passed = ok && !out.Diagnostics.HasErrors()

	out.Report = reportRel(f)
	report := Render(f, ix, out, who, now)
	if err := state.WriteFileAtomic(f.Layout.ReportFile(f.Name), []byte(report), 0o644); err != nil {
		_, _ = jr.End(startedEv, journal.Failed, journal.Event{Detail: err.Error()})
		return out, err
	}
	errs, warns := out.Diagnostics.Count()
	detail := fmt.Sprintf("%s: integration %s, %d errors, %d warnings", verdict(out.Passed), integrationStatus(out), errs, warns)
	status := journal.Completed
	if !out.Passed {
		status = journal.Failed
	}
	if _, err := jr.End(startedEv, status, journal.Event{Hashes: map[string]string{"report": workflow.Hash([]byte(report))}, Detail: detail}); err != nil {
		return out, err
	}
	_ = f.RenderLog()
	if out.Passed && f.Phase == workflow.Implemented {
		f.SetPhase(workflow.Validated)
		if err := f.SaveState(); err != nil {
			return out, err
		}
	}
	out.Next = f.Next()
	if !out.Passed {
		out.Next = fmt.Sprintf("fix the problems in %s (or revise the specs), then run `sdd validate %s` again", out.Report, f.Name)
	}
	return out, nil
}

func reportRel(f *feature.Feature) string {
	return ".sdd/features/" + f.Name + "/report.md"
}

func verdict(passed bool) string {
	if passed {
		return "PASSED"
	}
	return "FAILED"
}

func integrationStatus(r Result) string {
	switch {
	case r.Skipped:
		return "skipped"
	case len(r.Integration) > 0 && r.Integration[0].Passed:
		return "passed"
	}
	return "failed"
}

// Render writes the validation report in markdown.
func Render(f *feature.Feature, ix *trace.Index, r Result, who feature.Identity, now time.Time) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Validation report: %s\n\n", f.Name)
	fmt.Fprintf(&sb, "- Result: **%s**\n", verdict(r.Passed))
	fmt.Fprintf(&sb, "- Feature: %s\n", f.Title())
	fmt.Fprintf(&sb, "- Validated by %s at %s\n", who, now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "- Branch `%s` at `%s`\n", workspace.Branch(f.Name), short(r.Head))
	for _, a := range workflow.Artifacts() {
		if appr, ok := f.State.Approvals[a]; ok {
			fmt.Fprintf(&sb, "- %s approved at `%s` by %s\n", feature.FileName(a), shortHash(appr.Hash), appr.By)
		}
	}

	sb.WriteString("\n## Integration tests\n\n")
	if r.Skipped {
		sb.WriteString("No `integration` command is declared in the technical spec.\n")
	} else {
		sb.WriteString("```text\n" + verify.Summary(r.Integration) + "```\n")
	}

	sb.WriteString("\n## Consistency checks\n\nspec ↔ spec, spec ↔ plan, approvals and plan ↔ code.\n\n")
	if len(r.Diagnostics) == 0 {
		sb.WriteString("No problems found.\n")
	} else {
		for _, d := range r.Diagnostics {
			fmt.Fprintf(&sb, "- %s\n", d)
		}
	}

	sb.WriteString("\n## Groups\n\n| Group | Status | Commit | Tasks |\n|---|---|---|---|\n")
	for _, g := range f.State.Groups {
		pg, _ := f.Plan.Group(g.ID)
		var tasks []string
		for _, t := range g.Tasks {
			tasks = append(tasks, t.ID)
		}
		fmt.Fprintf(&sb, "| %s %s | %s | `%s` | %s |\n", g.ID, pg.Title, g.Status, short(g.CommitSHA), strings.Join(tasks, ", "))
	}

	sb.WriteString("\n## Traceability\n\n")
	matrix := ix.Matrix()
	if i := strings.Index(matrix, "| ID"); i >= 0 {
		matrix = matrix[i:]
	}
	sb.WriteString(matrix)

	if _, err := os.Stat(f.Layout.FeatureDir(f.Name) + "/deep-review.md"); err == nil {
		sb.WriteString("\n## Deep review\n\nSee [deep-review.md](deep-review.md) (agent-assisted; informational, it does not affect the result).\n")
	}
	return sb.String()
}

func shortHash(h string) string {
	if len(h) > len("sha256:")+12 {
		return h[:len("sha256:")+12]
	}
	return h
}

// SortedIDs returns the keys of m in spec order (helper for reports and context).
func SortedIDs(m map[string][]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	spec.SortIDs(ids)
	return ids
}
