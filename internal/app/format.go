package app

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkcarard/sdd-framework/internal/trace"
)

// Text renderings shared by the CLI and the MCP server.

// Plural formats a count with its noun ("1 error", "2 errors").
func Plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
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
