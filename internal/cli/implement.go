package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/darkcarard/quipu-sdd/internal/agents"
	"github.com/darkcarard/quipu-sdd/internal/app"
	"github.com/darkcarard/quipu-sdd/internal/feature"
)

// oneFeature parses exactly one positional argument (the feature) and the flags defined on fs.
func (a *App) oneFeature(fs *flag.FlagSet, args []string, usage string) (string, error) {
	pos, err := parseArgs(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", a.usageErr(usage)
	}
	return pos[0], nil
}

// optionalTarget parses at most one positional argument (a feature or a group
// ID; empty when omitted) and the flags defined on fs.
func (a *App) optionalTarget(fs *flag.FlagSet, args []string, usage string) (string, error) {
	pos, err := parseArgs(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) > 1 {
		return "", a.usageErr(usage)
	}
	if len(pos) == 0 {
		return "", nil
	}
	return pos[0], nil
}

func (a *App) runImplement(ctx context.Context, args []string) error {
	fs := a.newFlagSet("implement")
	arg, err := a.oneFeature(fs, args, "sdd implement <feature>")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	name, err := p.Resolve(arg)
	if err != nil {
		return err
	}
	s, err := p.StartImplement(ctx, name)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatStarted(s, name))
	return nil
}

func (a *App) runTask(ctx context.Context, args []string) error {
	const usage = `sdd task next <feature>
       sdd task done <feature> [--summary "<what was done>"]`
	if len(args) == 0 || (args[0] != "next" && args[0] != "done") {
		return a.usageErr(usage)
	}
	fs := a.newFlagSet("task " + args[0])
	summary := fs.String("summary", "", "one-line summary of the finished task")
	arg, err := a.oneFeature(fs, args[1:], usage)
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	if args[0] == "next" {
		n, err := p.TaskNext(ctx, arg)
		if err != nil {
			return err
		}
		fmt.Fprint(a.Stdout, app.FormatNext(n))
		return nil
	}
	d, err := p.TaskDone(ctx, arg, *summary)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatDone(d))
	if !d.Passed {
		return errFailed
	}
	return nil
}

func (a *App) runGroup(ctx context.Context, args []string) error {
	const usage = "sdd group verify <feature>"
	if len(args) == 0 || args[0] != "verify" {
		return a.usageErr(usage)
	}
	fs := a.newFlagSet("group verify")
	arg, err := a.oneFeature(fs, args[1:], usage)
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	v, err := p.GroupVerify(ctx, arg)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatVerified(v))
	if !v.Passed {
		return errFailed
	}
	return nil
}

func (a *App) runReview(ctx context.Context, args []string) error {
	fs := a.newFlagSet("review")
	diff := fs.Bool("diff", false, "also print the full diff")
	brief := fs.Bool("context", false, "print the brief for a deep review of the implemented feature (what review_context returns)")
	arg, err := a.optionalTarget(fs, args, "sdd review [<group> | <feature>] [--diff | --context]")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	if *brief {
		if arg == "" {
			return a.usageErr("sdd review <feature> --context")
		}
		text, err := p.ReviewContext(ctx, arg)
		if err != nil {
			return err
		}
		fmt.Fprint(a.Stdout, text)
		return nil
	}
	r, err := p.Review(ctx, arg, *diff)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatReview(r))
	return nil
}

func (a *App) runApproveGroup(ctx context.Context, args []string) error {
	fs := a.newFlagSet("approve")
	arg, err := a.optionalTarget(fs, args, "sdd approve [<group> | <feature>]")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.ApproveGroup(ctx, arg)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "approved %s\n\nNext: %s\n", r.Group, r.Next)
	return nil
}

func (a *App) runRevise(ctx context.Context, args []string) error {
	fs := a.newFlagSet("revise")
	msg := fs.String("m", "", "what the agent should change (required)")
	arg, err := a.optionalTarget(fs, args, `sdd revise [<group> | <feature>] -m "<feedback>"`)
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	g, err := p.Revise(ctx, arg, *msg)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "%s is back in progress with your feedback (revision %d).\n\nNext: %s; the agent gets the feedback from task_next.\n", g.ID, g.Revisions, feature.Skill("sdd-implement", g.Feature))
	return nil
}

func (a *App) runMerge(ctx context.Context, args []string) error {
	fs := a.newFlagSet("merge")
	arg, err := a.optionalTarget(fs, args, "sdd merge [<feature>]")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.Merge(ctx, arg)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatMerged(r))
	return nil
}

func (a *App) runResume(ctx context.Context, args []string) error {
	fs := a.newFlagSet("resume")
	discard := fs.Bool("discard", false, "discard the interrupted task's uncommitted changes (default: keep them for the agent)")
	force := fs.Bool("force-unlock", false, "remove the feature lock even if another process holds it")
	arg, err := a.oneFeature(fs, args, "sdd resume <feature> [--discard] [--force-unlock]")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.Resume(ctx, arg, *discard, *force)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatResumed(r))
	return nil
}

func (a *App) runClean(ctx context.Context, args []string) error {
	fs := a.newFlagSet("clean")
	force := fs.Bool("force", false, "remove the worktree even with uncommitted changes")
	arg, err := a.oneFeature(fs, args, "sdd clean <feature> [--force]")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	msg, err := p.Clean(ctx, arg, *force)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, msg)
	return nil
}

func (a *App) runValidate(ctx context.Context, args []string) error {
	fs := a.newFlagSet("validate")
	arg, err := a.oneFeature(fs, args, "sdd validate <feature>")
	if err != nil {
		return err
	}
	p, err := a.open()
	if err != nil {
		return err
	}
	r, err := p.Validate(ctx, arg)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Stdout, app.FormatValidation(r))
	if !r.Passed {
		return errFailed
	}
	return nil
}

// runHook answers a PreToolUse hook (Claude Code, Codex, Antigravity): a Bash call that runs a
// human-only command, even inside a wrapper, is denied. It needs no project.
func (a *App) runHook(_ context.Context, args []string) error {
	if len(args) != 1 || args[0] != "pre-tool-use" {
		return a.usageErr("sdd hook pre-tool-use   (reads the hook's JSON on stdin)")
	}
	if a.Stdin == nil {
		return nil
	}
	in, err := io.ReadAll(io.LimitReader(a.Stdin, 1<<20))
	if err != nil {
		return err
	}
	if reply := agents.HookReply(in); reply != nil {
		_, err = a.Stdout.Write(reply)
	}
	return err
}
