package cli

import (
	"context"
	"fmt"

	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

const planUsage = `sdd plan new <feature>
       sdd plan approve <feature>
       sdd plan <feature>   (agent generation, M4)`

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
		if args[0] == "new" {
			l, cfg, err := a.project()
			if err != nil {
				return err
			}
			name, err := feature.Resolve(l, pos[0])
			if err != nil {
				return err
			}
			return a.create(ctx, l, cfg, name, workflow.Plan, feature.Info{})
		}
		return a.approve(ctx, pos[0], workflow.Plan)
	case "-h", "--help", "help":
		fmt.Fprintln(a.Stdout, "usage: "+planUsage)
		return nil
	}
	fmt.Fprintln(a.Stderr, "sdd plan: agent plan generation is not implemented yet (planned for M4, see docs/ROADMAP.md); write the plan by hand with `sdd plan new <feature>`")
	return errFailed
}
