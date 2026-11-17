package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/darkcarard/sdd-framework/internal/app"
	"github.com/darkcarard/sdd-framework/internal/mcpserver"
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
	p, err := a.open()
	if err != nil {
		return err
	}
	if len(pos) == 1 {
		d, err := p.Status(pos[0])
		if err != nil {
			return err
		}
		fmt.Fprint(a.Stdout, app.FormatDetail(d))
		return nil
	}
	list, err := p.Overview()
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatOverview(list))
	return nil
}

func (a *App) runGuide(_ context.Context, args []string) error {
	fs := a.newFlagSet("guide")
	feat := fs.String("feature", "", "include the current status of this feature")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return a.usageErr("sdd guide <workflow|functional|technical> [--feature <feature>]")
	}
	if pos[0] == "workflow" && *feat == "" { // needs no project
		fmt.Fprint(a.Stdout, app.WorkflowGuide())
		return nil
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	text, err := p.Guide(pos[0], *feat)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, text)
	return nil
}

func (a *App) runMCP(ctx context.Context, args []string) error {
	fs := a.newFlagSet("mcp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return a.usageErr("sdd mcp")
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	err = mcpserver.Run(ctx, p)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

const specUsage = `sdd spec new <feature or "Free text title"> [--name <feature>] [--title "<title>"] [--description "<text>"]
       sdd spec new <feature> --technical
       sdd spec lint [<feature>]
       sdd spec approve <feature> --functional|--technical`

func (a *App) runSpec(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.usageErr(specUsage)
	}
	switch args[0] {
	case "new":
		return a.runSpecNew(ctx, args[1:])
	case "lint":
		return a.runLint(args[1:])
	case "approve":
		return a.runSpecApprove(ctx, args[1:])
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
	in := app.NewSpecInput{Name: *name, Title: *title, Description: *desc, Technical: *technical}
	if len(pos) == 1 {
		in.Text = pos[0]
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	c, err := p.NewSpec(ctx, in)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatCreated(c))
	return nil
}

func (a *App) runLint(args []string) error {
	fs := a.newFlagSet("spec lint")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return a.usageErr("sdd spec lint [<feature>]")
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.Lint(first(pos))
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatLint(r))
	if r.Errors > 0 {
		return errFailed
	}
	return nil
}

func first(pos []string) string {
	if len(pos) > 0 {
		return pos[0]
	}
	return ""
}

func (a *App) runSpecApprove(ctx context.Context, args []string) error {
	fs := a.newFlagSet("spec approve")
	functional := fs.Bool("functional", false, "approve functional.md")
	technical := fs.Bool("technical", false, "approve technical.md")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	var art workflow.Artifact
	switch {
	case *functional && !*technical:
		art = workflow.Functional
	case *technical && !*functional:
		art = workflow.Technical
	}
	if len(pos) != 1 || art == "" {
		return a.usageErr("sdd spec approve <feature> --functional|--technical")
	}
	return a.approve(ctx, pos[0], art)
}

// approve freezes an artifact and reports the outcome. Approvals are human-only:
// they exist only in the CLI, never as MCP tools.
func (a *App) approve(ctx context.Context, arg string, art workflow.Artifact) error {
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.Approve(ctx, arg, art)
	fmt.Fprint(a.Stdout, app.FormatDiagnostics(r.Diagnostics))
	switch {
	case errors.Is(err, app.ErrBlocked):
		fmt.Fprintf(a.Stdout, "\nnot approved: fix %s in %s and try again\n", app.Plural(r.Diagnostics.Errors, "error"), r.File)
		return errFailed
	case err != nil:
		return err
	case r.AlreadyApproved:
		fmt.Fprintf(a.Stdout, "%s is already approved (%s)\n", r.File, app.Short(r.Hash))
		return nil
	}
	fmt.Fprintf(a.Stdout, "approved %s (%s) by %s\n", r.File, app.Short(r.Hash), r.By)
	fmt.Fprintf(a.Stdout, "phase:   %s\n\nNext: %s\n", r.Phase, r.Next)
	return nil
}

const planUsage = `sdd plan new <feature>
       sdd plan approve <feature>`

func (a *App) runPlan(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.usageErr(planUsage)
	}
	switch args[0] {
	case "new", "approve":
		fs := a.newFlagSet("plan " + args[0])
		pos, err := parseArgs(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return a.usageErr("sdd plan " + args[0] + " <feature>")
		}
		if args[0] == "approve" {
			return a.approve(ctx, pos[0], workflow.Plan)
		}
		p, err := a.open()
		if err != nil {
			return err
		}
		c, err := p.NewPlan(ctx, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprint(a.Stdout, app.FormatCreated(c))
		return nil
	case "-h", "--help", "help":
		fmt.Fprintln(a.Stdout, "usage: "+planUsage)
		return nil
	}
	return a.usageErr(planUsage)
}

const featureUsage = `sdd feature describe <feature> [--title "<title>"] [--description "<text>"]`

func (a *App) runFeature(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "describe" {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
			fmt.Fprintln(a.Stdout, "usage: "+featureUsage)
			return nil
		}
		return a.usageErr(featureUsage)
	}
	fs := a.newFlagSet("feature describe")
	title := fs.String("title", "", "new human-readable title")
	desc := fs.String("description", "", "new one-line description (\"\" clears it)")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return a.usageErr(featureUsage)
	}
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	var tp, dp *string
	if set["title"] {
		tp = title
	}
	if set["description"] {
		dp = desc
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	d, err := p.Describe(ctx, pos[0], tp, dp)
	if err != nil {
		return err
	}
	if (tp != nil || dp != nil) && !d.Changed {
		fmt.Fprintln(a.Stdout, "nothing changed")
	}
	fmt.Fprint(a.Stdout, app.FormatDescription(d))
	return nil
}

func (a *App) runSync(_ context.Context, args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return a.usageErr("sdd sync check [<feature>]")
	}
	fs := a.newFlagSet("sync check")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return a.usageErr("sdd sync check [<feature>]")
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.Sync(first(pos))
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatSync(r))
	if r.Errors > 0 {
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
	p, err := a.open()
	if err != nil {
		return err
	}
	if *matrix {
		m, err := p.Matrix(pos[0])
		if err != nil {
			return err
		}
		fmt.Fprint(a.Stdout, m)
		return nil
	}
	entries, err := p.Trace(pos[0])
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatTrace(entries))
	return nil
}
