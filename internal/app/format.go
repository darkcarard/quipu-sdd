package app

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkcarard/sdd-framework/internal/implement"
	"github.com/darkcarard/sdd-framework/internal/trace"
	"github.com/darkcarard/sdd-framework/internal/validate"
	"github.com/darkcarard/sdd-framework/internal/verify"
)

// Text renderings shared by the CLI and the MCP server.

// Plural formats a count with its noun ("1 error", "2 errors").
func Plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// ShortSHA abbreviates a git commit SHA for display.
func ShortSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// Short abbreviates a "sha256:<hex>" hash for display.
func Short(hash string) string {
	if len(hash) > len("sha256:")+12 {
		return hash[:len("sha256:")+12]
	}
	return hash
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// noFeatures is shown when a project has no features yet.
const noFeatures = "No features yet. Next: sdd spec new <feature>\n"

// FormatDiagnostics lists diagnostics compiler-style, one per line.
func FormatDiagnostics(r Report) string {
	var sb strings.Builder
	for _, d := range r.List() {
		sb.WriteString(d.String() + "\n")
	}
	return sb.String()
}

// FormatLint renders a lint report.
func FormatLint(r Report) string {
	if r.Features == 0 {
		return noFeatures
	}
	return FormatDiagnostics(r) + fmt.Sprintf("%s checked: %s, %s\n", Plural(r.Files, "file"), Plural(r.Errors, "error"), Plural(r.Warnings, "warning"))
}

// FormatSync renders a sync report.
func FormatSync(r Report) string {
	if r.Features == 0 {
		return noFeatures
	}
	return FormatDiagnostics(r) + fmt.Sprintf("sync check (spec↔spec, spec↔plan) on %s: %s, %s\n",
		Plural(r.Features, "feature"), Plural(r.Errors, "error"), Plural(r.Warnings, "warning"))
}

// FormatOverview renders the project's features as a table.
func FormatOverview(list []Summary) string {
	if len(list) == 0 {
		return noFeatures
	}
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FEATURE\tTITLE\tPHASE\tFUNCTIONAL\tTECHNICAL\tPLAN")
	for _, s := range list {
		if s.Error != "" {
			fmt.Fprintf(tw, "%s\t\t(error: %s)\t\t\t\n", s.Name, s.Error)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", s.Name, truncate(s.Title, 30), s.Phase, s.Functional, s.Technical, s.Plan)
	}
	_ = tw.Flush()
	sb.WriteString("\nDetails and the next command: sdd status <feature>\n")
	return sb.String()
}

// FormatDetail renders the full status of one feature.
func FormatDetail(d Detail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Feature: %s (%s)\n", d.Name, d.Title)
	if d.Description != "" {
		fmt.Fprintf(&sb, "         %s\n", d.Description)
	}
	fmt.Fprintf(&sb, "Phase:   %s", d.Phase)
	if d.StoredPhase != "" {
		fmt.Fprintf(&sb, " (state.json still says %s; updated on the next approval)", d.StoredPhase)
	}
	sb.WriteString("\n\nArtifacts:\n")
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	for _, a := range d.Artifacts {
		var detail string
		if a.Approval != nil {
			detail = fmt.Sprintf("approved %s by %s at %s", Short(a.Approval.Hash), a.Approval.By, a.Approval.At.Format(time.RFC3339))
		}
		if a.Linted {
			lint := fmt.Sprintf("lint: %s, %s", Plural(a.Errors, "error"), Plural(a.Warnings, "warning"))
			if detail == "" {
				detail = lint
			} else {
				detail = lint + "; last " + detail
			}
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", a.File, a.Status, detail)
	}
	_ = tw.Flush()
	if len(d.Interrupted) > 0 {
		sb.WriteString("\nInterrupted actions:\n")
		for _, ev := range d.Interrupted {
			fmt.Fprintf(&sb, "  #%d %s by %s at %s\n", ev.Seq, ev.Action, ev.Actor, ev.At.Format(time.RFC3339))
		}
	}
	if len(d.Groups) > 0 {
		fmt.Fprintf(&sb, "\nImplementation (worktree %s, branch %s):\n", d.Worktree, d.Branch)
		sb.WriteString(FormatGroups(d.Groups))
	}
	if d.Lock != "" {
		fmt.Fprintf(&sb, "\nLock: %s\n", d.Lock)
	}
	fmt.Fprintf(&sb, "\nNext: %s\n", d.Next)
	return sb.String()
}

// FormatCreated renders the result of creating an artifact.
func FormatCreated(c Created) string {
	return fmt.Sprintf("feature  %s: %s\ncreated  %s\n\nNext: %s\n", c.Feature, c.Title, c.File, c.Next)
}

// FormatTrace renders trace entries as trees.
func FormatTrace(entries []trace.Entry) string {
	var parts []string
	for _, e := range entries {
		parts = append(parts, e.Render())
	}
	return strings.Join(parts, "\n")
}

// FormatDescription renders a feature's title and description.
func FormatDescription(d Description) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "feature:     %s\ntitle:       %s\ndescription: %s\n", d.Feature, d.Title, d.Description)
	if !d.CreatedAt.IsZero() {
		fmt.Fprintf(&sb, "created:     %s by %s\n", d.CreatedAt.Format(time.RFC3339), d.CreatedBy)
	}
	return sb.String()
}

func taskLine(t implement.TaskView) string {
	s := fmt.Sprintf("    %-6s %-12s %s", t.ID, t.Status, t.Title)
	if len(t.Undeclared) > 0 {
		s += fmt.Sprintf("  (undeclared: %s)", strings.Join(t.Undeclared, ", "))
	}
	return s
}

// FormatGroups renders implementation progress.
func FormatGroups(groups []implement.GroupView) string {
	var sb strings.Builder
	for _, g := range groups {
		done := 0
		for _, t := range g.Tasks {
			if t.Status == "done" {
				done++
			}
		}
		line := fmt.Sprintf("  %-4s %-16s %d/%d tasks  %s", g.ID, g.Status, done, len(g.Tasks), g.Title)
		if g.Commit != "" {
			line += "  (" + ShortSHA(g.Commit) + ")"
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// FormatStarted renders the result of `sdd implement`.
func FormatStarted(s implement.Started, feature string) string {
	var sb strings.Builder
	if s.Worktree != "" {
		verb := "using"
		if s.Created {
			verb = "created"
		}
		fmt.Fprintf(&sb, "%s worktree %s on branch %s (run %s)\n", verb, s.Worktree, s.Branch, s.RunID)
	}
	if s.Group != nil {
		fmt.Fprintf(&sb, "\n%s: %s is in progress\n", s.Group.ID, s.Group.Title)
		for _, t := range s.Group.Tasks {
			sb.WriteString(taskLine(t) + "\n")
		}
		fmt.Fprintf(&sb, "\nNext: `/sdd-implement %s` in Claude Code (or have your agent call task_next)\n", feature)
	}
	if s.Message != "" {
		fmt.Fprintf(&sb, "\n%s\n", s.Message)
	}
	return sb.String()
}

func coveredLine(c implement.Covered) string {
	id := c.ID
	if c.Parent != "" {
		id += " (" + c.Parent + ")"
	}
	s := fmt.Sprintf("- %s: %s", id, c.Text)
	if len(c.Technical) > 0 {
		s += " — " + strings.Join(c.Technical, ", ")
	}
	return s
}

// FormatNext renders the agent's next step.
func FormatNext(n implement.Next) string {
	var sb strings.Builder
	if n.Group != nil {
		fmt.Fprintf(&sb, "Group %s: %s  [layer: %s]\nGoal: %s\n", n.Group.ID, n.Group.Title, n.Group.Layer, n.Group.Goal)
	}
	switch n.Kind {
	case implement.NextTask:
		t := n.Task
		resumed := ""
		if n.Resumed {
			resumed = "  (resumed)"
		}
		fmt.Fprintf(&sb, "\nTask %s: %s%s\n\n", t.ID, t.Title, resumed)
		fmt.Fprintf(&sb, "Worktree:  %s\n", n.Worktree)
		fmt.Fprintf(&sb, "Files:     %s\n", strings.Join(t.Files, ", "))
		fmt.Fprintf(&sb, "Done when: %s\n", t.DoneWhen)
		fmt.Fprintf(&sb, "Verify:    %s\n", t.Verify)
		if len(t.Covers) > 0 {
			sb.WriteString("\nCovers:\n")
			for _, c := range t.Covers {
				sb.WriteString(coveredLine(c) + "\n")
			}
		}
		if t.Description != "" {
			fmt.Fprintf(&sb, "\nDescription:\n%s\n", t.Description)
		}
		fmt.Fprintf(&sb, "\nCommands (task_done runs build; group_verify runs build, lint and test): build `%s`, lint `%s`, test `%s`\n",
			t.Commands.Build, t.Commands.Lint, t.Commands.Test)
		if len(n.Dirty) > 0 {
			fmt.Fprintf(&sb, "Uncommitted changes in the worktree: %s\n", strings.Join(n.Dirty, ", "))
		}
		sb.WriteString("Rules: edit only in the worktree, never commit, keep to the declared files, mark tests with `covers: AC-###` (see `guide implement`).\n")
		switch {
		case t.Profile != "":
			fmt.Fprintf(&sb, "\nLanguage profile (%s):\n\n%s\n", t.ProfilePath, strings.TrimSpace(stripFrontmatter(t.Profile)))
		case t.ProfilePath != "":
			fmt.Fprintf(&sb, "Language profile: %s (sent with the group's first task; read it if you no longer have it)\n", t.ProfilePath)
		}
	case implement.NextRevision:
		fmt.Fprintf(&sb, "\nThe user asked for changes:\n\n%s\n\nWorktree: %s\nChanges in this group so far:\n%s\n", n.Feedback, n.Worktree, n.DiffStat)
	}
	if n.Message != "" {
		fmt.Fprintf(&sb, "\nNext: %s\n", n.Message)
	}
	return sb.String()
}

// FormatDone renders the result of task_done.
func FormatDone(d implement.Done) string {
	var sb strings.Builder
	sb.WriteString(verify.Summary(d.Results))
	if d.Passed {
		fmt.Fprintf(&sb, "\n%s done (checkpoint %s).\n", d.Task, ShortSHA(d.Commit))
		if d.NoChanges {
			sb.WriteString("Warning: the task changed no files.\n")
		}
		if len(d.Undeclared) > 0 {
			fmt.Fprintf(&sb, "Changed files not declared for %s (reported to the user): %s\n", d.Task, strings.Join(d.Undeclared, ", "))
		}
	} else {
		fmt.Fprintf(&sb, "\n%s is not done: the build failed.\n", d.Task)
	}
	fmt.Fprintf(&sb, "Next: %s\n", d.Next)
	return sb.String()
}

// FormatVerified renders the result of group_verify.
func FormatVerified(v implement.Verified) string {
	var sb strings.Builder
	sb.WriteString(verify.Summary(v.Results))
	if v.Passed {
		fmt.Fprintf(&sb, "\n%s verified and committed: %s %s\n", v.Group, ShortSHA(v.Commit), v.Subject)
	}
	fmt.Fprintf(&sb, "Next: %s\n", v.Next)
	return sb.String()
}

// FormatReview renders a group review for the user.
func FormatReview(r implement.Review) string {
	var sb strings.Builder
	g := r.Group
	fmt.Fprintf(&sb, "Group %s: %s  [%s]\n", g.ID, g.Title, g.Status)
	if g.Commit != "" {
		fmt.Fprintf(&sb, "Commit: %s   (git -C %s show %s)\n", ShortSHA(g.Commit), r.Worktree, ShortSHA(g.Commit))
	}
	sb.WriteString("\nTasks:\n")
	for _, t := range g.Tasks {
		sb.WriteString(taskLine(t) + "\n")
		if len(t.Covers) > 0 {
			fmt.Fprintf(&sb, "           covers: %s\n", strings.Join(t.Covers, ", "))
		}
		if t.Summary != "" {
			fmt.Fprintf(&sb, "           agent: %s\n", t.Summary)
		}
	}
	if g.Verification != "" {
		fmt.Fprintf(&sb, "\nVerification:\n%s", g.Verification)
	}
	if g.Feedback != "" {
		fmt.Fprintf(&sb, "\nPending feedback: %s\n", g.Feedback)
	}
	fmt.Fprintf(&sb, "\nChanges:\n%s\n", r.DiffStat)
	if r.Diff != "" {
		fmt.Fprintf(&sb, "\n%s\n", r.Diff)
	}
	fmt.Fprintf(&sb, "\nNext: %s\n", r.Next)
	return sb.String()
}

// FormatResumed renders the result of `sdd resume`.
func FormatResumed(r implement.Resumed) string {
	var sb strings.Builder
	for _, s := range r.Reconciled {
		fmt.Fprintf(&sb, "closed interrupted action %s\n", s)
	}
	if r.Recreated {
		sb.WriteString("recreated the worktree from its branch\n")
	}
	if len(r.Discarded) > 0 {
		fmt.Fprintf(&sb, "discarded uncommitted changes: %s\n", strings.Join(r.Discarded, ", "))
	}
	if len(r.Kept) > 0 {
		fmt.Fprintf(&sb, "kept uncommitted changes for the agent: %s\n", strings.Join(r.Kept, ", "))
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&sb, "warning: %s\n", w)
	}
	if sb.Len() == 0 {
		sb.WriteString("nothing to repair\n")
	}
	fmt.Fprintf(&sb, "\nNext: %s\n", r.Next)
	return sb.String()
}

// stripFrontmatter drops a leading YAML frontmatter block.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if i := strings.Index(s[4:], "\n---\n"); i >= 0 {
		return s[4+i+5:]
	}
	return s
}

// FormatValidation renders the result of `sdd validate`.
func FormatValidation(r validate.Result) string {
	var sb strings.Builder
	if r.Skipped {
		sb.WriteString("integration: skipped (no command)\n")
	} else {
		sb.WriteString(verify.Summary(r.Integration))
	}
	for _, d := range r.Diagnostics {
		sb.WriteString(d.String() + "\n")
	}
	errs, warns := r.Diagnostics.Count()
	result := "PASSED"
	if !r.Passed {
		result = "FAILED"
	}
	fmt.Fprintf(&sb, "\nvalidation %s: %s, %s; report written to %s\n", result, Plural(errs, "error"), Plural(warns, "warning"), r.Report)
	fmt.Fprintf(&sb, "\nNext: %s\n", r.Next)
	return sb.String()
}
