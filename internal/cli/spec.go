package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

const specUsage = `sdd spec new <feature or "Free text title"> [--name <feature>] [--title "<title>"] [--description "<text>"]
       sdd spec new <feature> --technical
       sdd spec lint [<feature>]
       sdd spec approve <feature> --functional|--technical
       sdd spec chat <feature> --functional|--technical   (M3)`

func (a *App) runSpec(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.usageErr(specUsage)
	}
	switch args[0] {
	case "new":
		return a.runSpecNew(ctx, args[1:])
	case "lint":
		return a.runSpecLint(ctx, args[1:])
	case "approve":
		return a.runSpecApprove(ctx, args[1:])
	case "chat":
		fmt.Fprintln(a.Stderr, "sdd spec chat: not implemented yet (planned for M3, see docs/ROADMAP.md); edit the spec by hand and use `sdd spec lint`")
		return errFailed
	case "-h", "--help", "help":
		fmt.Fprintln(a.Stdout, "usage: "+specUsage)
		return nil
	}
	return a.usageErr(specUsage)
}

func (a *App) runSpecNew(ctx context.Context, args []string) error {
	fs := a.newFlagSet("spec new")
	technical := fs.Bool("technical", false, "create technical.md (requires an approved functional spec)")
	name := fs.String("name", "", "feature name to use instead of the one derived from the argument")
	title := fs.String("title", "", "human-readable title (default: the argument when it is free text)")
	desc := fs.String("description", "", "one-line description, stored in state.json and the Summary")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (len(pos) == 0 && *name == "") {
		return a.usageErr(`sdd spec new <feature or "Free text title"> [--name <feature>] [--title "<title>"] [--description "<text>"] [--technical]`)
	}
	arg := ""
	if len(pos) == 1 {
		arg = pos[0]
	}
	l, cfg, err := a.project()
	if err != nil {
		return err
	}
	if *technical {
		slug := *name
		if slug == "" {
			if slug, err = feature.Resolve(l, arg); err != nil {
				return err
			}
		} else if err := feature.ValidName(slug); err != nil {
			return err
		}
		return a.create(ctx, l, cfg, slug, workflow.Technical, feature.Info{})
	}
	slug, info, err := newFeatureName(arg, *name, *title, *desc)
	if err != nil {
		return err
	}
	return a.create(ctx, l, cfg, slug, workflow.Functional, info)
}

// newFeatureName decides the feature name and title of a new feature. A valid
// name is used as is; free text is slugified and kept as the title; --name
// overrides the derived name.
func newFeatureName(arg, name, title, desc string) (string, feature.Info, error) {
	info := feature.Info{Title: title, Description: desc}
	switch {
	case name != "":
		if err := feature.ValidName(name); err != nil {
			return "", info, err
		}
		if info.Title == "" && arg != name {
			info.Title = arg
		}
		return name, info, nil
	case feature.ValidName(arg) == nil:
		return arg, info, nil
	}
	slug, err := feature.Slugify(arg)
	if err != nil {
		return "", info, err
	}
	if info.Title == "" {
		info.Title = strings.Join(strings.Fields(arg), " ")
	}
	return slug, info, nil
}

// create writes a new artifact from its template under the feature lock.
func (a *App) create(ctx context.Context, l state.Layout, cfg config.Config, name string, art workflow.Artifact, info feature.Info) error {
	who, err := a.identity(ctx)
	if err != nil {
		return err
	}
	return a.withLock(l, name, func() error {
		f, err := feature.Open(l, cfg, name)
		if err != nil {
			return err
		}
		if _, err := f.Create(art, info, who, a.now()); err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "feature  %s: %s\n", f.Name, f.Title())
		fmt.Fprintf(a.Stdout, "created  %s\n", f.Files[art].Rel)
		fmt.Fprintf(a.Stdout, "\nNext: %s\n", f.Next())
		return nil
	})
}

func (a *App) runSpecLint(_ context.Context, args []string) error {
	fs := a.newFlagSet("spec lint")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return a.usageErr("sdd spec lint [<feature>]")
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
	files := 0
	for _, n := range names {
		if err := feature.ValidName(n); err != nil {
			all = append(all, diag.Diagnostic{Severity: diag.Error, File: "specs/" + n, Message: err.Error()})
			continue
		}
		f, err := feature.Load(l, cfg, n)
		if err != nil {
			return err
		}
		for _, art := range workflow.Artifacts() {
			if f.Files[art].Exists {
				files++
				all = append(all, f.Lint(art)...)
			}
		}
	}
	errs, warns := a.printDiags(all)
	fmt.Fprintf(a.Stdout, "%s checked: %s, %s\n", plural(files, "file"), plural(errs, "error"), plural(warns, "warning"))
	if errs > 0 {
		return errFailed
	}
	return nil
}

func (a *App) runSpecApprove(ctx context.Context, args []string) error {
	fs := a.newFlagSet("spec approve")
	functional := fs.Bool("functional", false, "approve functional.md")
	technical := fs.Bool("technical", false, "approve technical.md")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	art, ok := artifactFlag(*functional, *technical)
	if len(pos) != 1 || !ok {
		return a.usageErr("sdd spec approve <feature> --functional|--technical")
	}
	return a.approve(ctx, pos[0], art)
}

// approve freezes an artifact under the feature lock and reports the outcome.
func (a *App) approve(ctx context.Context, name string, art workflow.Artifact) error {
	l, cfg, err := a.project()
	if err != nil {
		return err
	}
	if name, err = feature.Resolve(l, name); err != nil {
		return err
	}
	who, err := a.identity(ctx)
	if err != nil {
		return err
	}
	return a.withLock(l, name, func() error {
		f, err := feature.Load(l, cfg, name)
		if err != nil {
			return err
		}
		ds, err := f.Approve(art, who, a.now())
		a.printDiags(ds)
		switch {
		case errors.Is(err, feature.ErrAlreadyApproved):
			fmt.Fprintf(a.Stdout, "%s is already approved (%s)\n", f.Files[art].Rel, short(f.State.Approvals[art].Hash))
			return nil
		case errors.Is(err, feature.ErrBlocked):
			n, _ := ds.Count()
			fmt.Fprintf(a.Stdout, "\nnot approved: fix %s in %s and try again\n", plural(n, "error"), f.Files[art].Rel)
			return errFailed
		case err != nil:
			return err
		}
		fmt.Fprintf(a.Stdout, "approved %s (%s) by %s\n", f.Files[art].Rel, short(f.State.Approvals[art].Hash), who)
		fmt.Fprintf(a.Stdout, "phase:   %s\n\nNext: %s\n", f.Phase, f.Next())
		return nil
	})
}

// short abbreviates a "sha256:<hex>" hash for display.
func short(hash string) string {
	if len(hash) > len("sha256:")+12 {
		return hash[:len("sha256:")+12]
	}
	return hash
}
