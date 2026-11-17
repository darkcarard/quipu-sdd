package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/darkcarard/sdd-framework/internal/feature"
)

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
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if len(pos) != 1 {
		return a.usageErr(featureUsage)
	}
	l, cfg, err := a.project()
	if err != nil {
		return err
	}
	name, err := feature.Resolve(l, pos[0])
	if err != nil {
		return err
	}
	var tp, dp *string
	if set["title"] {
		tp = title
	}
	if set["description"] {
		dp = desc
	}
	if tp == nil && dp == nil { // no flags: show the current mapping
		f, err := feature.Load(l, cfg, name)
		if err != nil {
			return err
		}
		a.printDescription(f)
		return nil
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
		changed, err := f.Describe(tp, dp, who)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintln(a.Stdout, "nothing changed")
		}
		a.printDescription(f)
		return nil
	})
}

func (a *App) printDescription(f *feature.Feature) {
	fmt.Fprintf(a.Stdout, "feature:     %s\ntitle:       %s\ndescription: %s\n", f.Name, f.Title(), f.State.Description)
	if !f.State.CreatedAt.IsZero() {
		fmt.Fprintf(a.Stdout, "created:     %s by %s\n", f.State.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), f.State.CreatedBy)
	}
}
