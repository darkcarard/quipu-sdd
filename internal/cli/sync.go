package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/plan"
	"github.com/darkcarard/sdd-framework/internal/spec"
	"github.com/darkcarard/sdd-framework/internal/trace"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

func (a *App) runSync(_ context.Context, args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return a.usageErr("sdd sync check [<feature>] [--deep]")
	}
	fs := a.newFlagSet("sync check")
	deep := fs.Bool("deep", false, "LLM-assisted semantic review (M6)")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return a.usageErr("sdd sync check [<feature>] [--deep]")
	}
	if *deep {
		fmt.Fprintln(a.Stderr, "sdd sync check --deep: not implemented yet (planned for M6, see docs/ROADMAP.md)")
		return errFailed
	}
	l, cfg, err := a.project()
	if err != nil {
		return err
	}
	names, err := featureArgs(l, pos)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(a.Stdout, "No features yet. Next: sdd spec new <feature>")
		return nil
	}
	var all diag.List
	for _, n := range names {
		if err := feature.ValidName(n); err != nil {
			all = append(all, diag.Diagnostic{Severity: diag.Error, File: "specs/" + n, Message: err.Error()})
			continue
		}
		f, err := feature.Load(l, cfg, n)
		if err != nil {
			return err
		}
		all = append(all, f.Sync()...)
	}
	errs, warns := a.printDiags(all)
	fmt.Fprintf(a.Stdout, "sync check (spec↔spec, spec↔plan) on %s: %s, %s\n",
		plural(len(names), "feature"), plural(errs, "error"), plural(warns, "warning"))
	if errs > 0 {
		return errFailed
	}
	return nil
}

const traceUsage = `sdd trace [<feature>/]<ID>
       sdd trace --matrix <feature>`

func (a *App) runTrace(_ context.Context, args []string) error {
	fs := a.newFlagSet("trace")
	matrix := fs.Bool("matrix", false, "print the requirement × artifact matrix of a feature in markdown")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return a.usageErr(traceUsage)
	}
	l, cfg, err := a.project()
	if err != nil {
		return err
	}
	index := func(name string) (*trace.Index, error) {
		f, err := feature.Load(l, cfg, name)
		if err != nil {
			return nil, err
		}
		return trace.Build(name, trace.Files{
			Functional: f.Files[workflow.Functional].Rel,
			Technical:  f.Files[workflow.Technical].Rel,
			Plan:       f.Files[workflow.Plan].Rel,
		}, f.Functional, f.Technical, f.Plan), nil
	}

	if *matrix {
		name, err := feature.Resolve(l, pos[0])
		if err != nil {
			return err
		}
		ix, err := index(name)
		if err != nil {
			return err
		}
		fmt.Fprint(a.Stdout, ix.Matrix())
		return nil
	}

	id := pos[0]
	var names []string
	if feat, rest, ok := strings.Cut(id, "/"); ok {
		name, err := feature.Resolve(l, feat)
		if err != nil {
			return err
		}
		names, id = []string{name}, rest
	} else if names, err = feature.List(l); err != nil {
		return err
	}
	if !traceableID(id) {
		return fmt.Errorf("%q is not a traceable ID (FR-###, NFR-###, AC-###, ADR-###, G#, T#.#)", id)
	}
	found := 0
	for _, n := range names {
		ix, err := index(n)
		if err != nil {
			return err
		}
		if e, ok := ix.Lookup(id); ok {
			if found > 0 {
				fmt.Fprintln(a.Stdout)
			}
			fmt.Fprint(a.Stdout, e.Render())
			found++
		}
	}
	if found == 0 {
		return fmt.Errorf("%s is not defined in any feature spec or plan", pos[0])
	}
	return nil
}

func traceableID(id string) bool {
	return spec.ValidID(id) || plan.GroupIDRe.MatchString(id) || plan.TaskIDRe.MatchString(id)
}
