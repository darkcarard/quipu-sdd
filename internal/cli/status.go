package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/lock"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

func (a *App) runStatus(_ context.Context, args []string) error {
	fs := a.newFlagSet("status")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return a.usageErr("sdd status [<feature>]")
	}
	l, cfg, err := a.project()
	if err != nil {
		return err
	}
	if len(pos) == 1 {
		name, err := feature.Resolve(l, pos[0])
		if err != nil {
			return err
		}
		f, err := feature.Load(l, cfg, name)
		if err != nil {
			return err
		}
		return a.featureStatus(f)
	}

	names, err := feature.List(l)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(a.Stdout, "No features yet. Next: sdd spec new <feature>")
		return nil
	}
	tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FEATURE\tTITLE\tPHASE\tFUNCTIONAL\tTECHNICAL\tPLAN")
	for _, n := range names {
		f, err := feature.Load(l, cfg, n)
		if err != nil {
			fmt.Fprintf(tw, "%s\t\t(error: %v)\t\t\t\n", n, err)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", n, truncate(f.Title(), 30), f.Phase,
			f.Statuses[workflow.Functional], f.Statuses[workflow.Technical], f.Statuses[workflow.Plan])
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "\nDetails and the next command: sdd status <feature>")
	return nil
}

func (a *App) featureStatus(f *feature.Feature) error {
	w := a.Stdout
	fmt.Fprintf(w, "Feature: %s (%s)\n", f.Name, f.Title())
	if f.State.Description != "" {
		fmt.Fprintf(w, "         %s\n", f.State.Description)
	}
	fmt.Fprintf(w, "Phase:   %s", f.Phase)
	if f.Saved && f.State.Phase != f.Phase {
		fmt.Fprintf(w, " (state.json still says %s; updated on the next approval)", f.State.Phase)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "\nArtifacts:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, art := range workflow.Artifacts() {
		st := f.Statuses[art]
		detail := ""
		if appr, ok := f.State.Approvals[art]; ok {
			detail = fmt.Sprintf("approved %s by %s at %s", short(appr.Hash), appr.By, appr.At.Format(time.RFC3339))
		}
		if f.Files[art].Exists && st != workflow.StatusApproved {
			e, wn := f.Lint(art).Count()
			lint := fmt.Sprintf("lint: %s, %s", plural(e, "error"), plural(wn, "warning"))
			if detail == "" {
				detail = lint
			} else {
				detail = lint + "; last " + detail
			}
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", feature.FileName(art), st, detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	events, err := journal.Read(f.Layout.JournalFile(f.Name))
	if err != nil {
		return err
	}
	if open := journal.Interrupted(events); len(open) > 0 {
		fmt.Fprintln(w, "\nInterrupted actions:")
		for _, ev := range open {
			fmt.Fprintf(w, "  #%d %s by %s at %s\n", ev.Seq, ev.Action, ev.Actor, ev.TS.Format(time.RFC3339))
		}
	}
	holder, err := lock.Holder(f.Layout.LockFile(f.Name))
	switch {
	case err == nil:
		host, _ := os.Hostname()
		note := ""
		if lock.Abandoned(holder, host) {
			note = " (abandoned: the process is gone)"
		}
		fmt.Fprintf(w, "\nLock: held by %s%s\n", holder, note)
	case !errors.Is(err, fs.ErrNotExist):
		fmt.Fprintf(w, "\nLock: unreadable (%v)\n", err)
	}

	fmt.Fprintf(w, "\nNext: %s\n", f.Next())
	return nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
