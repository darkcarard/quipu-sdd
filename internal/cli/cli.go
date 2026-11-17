// Package cli implements the sdd command-line interface: a thin layer over the
// use cases in internal/app, which the MCP server shares.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/agents"
	"github.com/darkcarard/quipu-sdd/internal/app"
	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/state"
	"github.com/darkcarard/quipu-sdd/internal/templates"
	"github.com/darkcarard/quipu-sdd/internal/version"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// App holds the I/O and working directory for a CLI invocation.
type App struct {
	Dir    string // working directory; commands find the project root at or above it
	Stdout io.Writer
	Stderr io.Writer

	// Identity returns who performs an action; nil reads git config user.name/email.
	Identity func(ctx context.Context) (feature.Identity, error)
	// Now returns the current time; nil uses time.Now.
	Now func() time.Time
}

type command struct {
	name      string
	summary   string
	milestone string // non-empty for commands not implemented yet
	run       func(a *App, ctx context.Context, args []string) error
}

func (a *App) commands() []command {
	return []command{
		{name: "init", summary: "Set up sdd in the current project (and an agent with --agent)", run: (*App).runInit},
		{name: "version", summary: "Print the sdd version", run: (*App).runVersion},
		{name: "status", summary: "Show feature state and the next action", run: (*App).runStatus},
		{name: "guide", summary: "Print the drafting instructions and template for a topic", run: (*App).runGuide},
		{name: "mcp", summary: "Serve sdd to a coding agent over MCP (stdio)", run: (*App).runMCP},
		{name: "adopt", summary: "Inventory an existing project to document it as baseline specs", run: (*App).runAdopt},
		{name: "feature", summary: "Show or change a feature's title and description", run: (*App).runFeature},
		{name: "spec", summary: "Create, lint and approve specs", run: (*App).runSpec},
		{name: "plan", summary: "Create and approve the implementation plan", run: (*App).runPlan},
		{name: "implement", summary: "Start implementing a feature in its isolated worktree", run: (*App).runImplement},
		{name: "task", summary: "Get the next task or finish the current one (used by the agent)", run: (*App).runTask},
		{name: "group", summary: "Verify and commit the current group (used by the agent)", run: (*App).runGroup},
		{name: "review", summary: "Review the group awaiting approval", run: (*App).runReview},
		{name: "approve", summary: "Approve the reviewed group and continue", run: (*App).runApproveGroup},
		{name: "revise", summary: "Send review feedback to the agent for the current group", run: (*App).runRevise},
		{name: "resume", summary: "Repair state after an interruption", run: (*App).runResume},
		{name: "clean", summary: "Remove a feature worktree (the branch is kept)", run: (*App).runClean},
		{name: "validate", summary: "Run integration tests and every sync check, write the report", run: (*App).runValidate},
		{name: "sync", summary: "Check consistency between specs, plan and code", run: (*App).runSync},
		{name: "trace", summary: "Trace an ID across specs, plan, commits and tests", run: (*App).runTrace},
		{name: "upgrade", summary: "Migrate the project layout to this binary's version", milestone: "later"},
	}
}

// Run executes the CLI with args (excluding the program name) and returns an exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		a.usage(a.Stdout)
		if len(args) == 0 {
			return ExitUsage
		}
		return ExitOK
	}
	for _, c := range a.commands() {
		if c.name != args[0] {
			continue
		}
		if c.run == nil {
			fmt.Fprintf(a.Stderr, "sdd %s: not implemented yet (planned for %s, see docs/ROADMAP.md)\n", c.name, c.milestone)
			return ExitError
		}
		err := c.run(a, ctx, args[1:])
		switch {
		case err == nil:
			return ExitOK
		case errors.Is(err, flag.ErrHelp):
			return ExitOK
		case errors.Is(err, errUsage):
			return ExitUsage
		case errors.Is(err, errFailed):
			return ExitError
		default:
			fmt.Fprintf(a.Stderr, "sdd %s: %v\n", c.name, err)
			return ExitError
		}
	}
	fmt.Fprintf(a.Stderr, "sdd: unknown command %q\n\n", args[0])
	a.usage(a.Stderr)
	return ExitUsage
}

var (
	errUsage = errors.New("usage error")
	// errFailed makes a command exit with ExitError after it has already reported why.
	errFailed = errors.New("failed")
)

func (a *App) usage(w io.Writer) {
	fmt.Fprintln(w, "sdd: Spec-Driven Development framework")
	fmt.Fprintln(w, "\nUsage: sdd <command> [arguments]\n\nCommands:")
	for _, c := range a.commands() {
		note := ""
		if c.run == nil {
			note = fmt.Sprintf(" (%s)", c.milestone)
		}
		fmt.Fprintf(w, "  %-10s %s%s\n", c.name, c.summary, note)
	}
	fmt.Fprintln(w, "\nWorkflow: specify → plan → implement → validate. See docs/WORKFLOW.md.")
}

func (a *App) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("sdd "+name, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	return fs
}

// parseArgs parses flags that may appear before, between or after positional
// arguments (Go's flag package stops at the first positional one).
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (a *App) usageErr(usage string) error {
	fmt.Fprintln(a.Stderr, "usage: "+usage)
	return errUsage
}

// open loads the project that contains the working directory.
func (a *App) open() (*app.App, error) {
	p, err := app.Open(a.Dir)
	if err != nil {
		return nil, err
	}
	p.Who, p.Now = a.Identity, a.Now
	p.Warn = func(msg string) { fmt.Fprintln(a.Stderr, "warning: "+msg) }
	return p, nil
}

func (a *App) runVersion(_ context.Context, args []string) error {
	fs := a.newFlagSet("version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "sdd %s (layout v%d, %s, %s/%s)\n",
		version.Version, version.LayoutVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

const initUsage = "sdd init [--agent claude|generic] [--check] [--force]"

func (a *App) runInit(_ context.Context, args []string) error {
	fs := a.newFlagSet("init")
	force := fs.Bool("force", false, "overwrite templates and profiles in .sdd/ with the built-in versions")
	agent := fs.String("agent", "", "also set up a coding agent: "+strings.Join(agents.Names, ", "))
	check := fs.Bool("check", false, "report what is missing without writing anything")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || (*agent != "" && !slices.Contains(agents.Names, *agent)) || (*check && *force) {
		return a.usageErr(initUsage)
	}
	l := state.Layout{Root: a.Dir}
	if _, err := os.Stat(filepath.Join(a.Dir, ".git")); err != nil {
		fmt.Fprintln(a.Stderr, "warning: not a git repository; run `git init` before `sdd implement` (worktrees need git)")
	}

	// sdd's own files: sdd.yaml, .sdd/.gitignore, templates and profiles.
	own := []agents.File{
		{Path: rel(a.Dir, l.ConfigFile()), Content: []byte(config.DefaultYAML())},
		{Path: rel(a.Dir, l.GitignoreFile()), Content: []byte(state.Gitignore)},
	}
	missingTemplates, err := templates.Missing(l.TemplatesDir(), l.ProfilesDir())
	if err != nil {
		return err
	}
	var files []agents.File
	if *agent != "" {
		if files, err = agents.Files(*agent, app.WorkflowGuide()); err != nil {
			return err
		}
	}

	if *check {
		results, err := agents.Install(a.Dir, append(own, files...), true)
		if err != nil {
			return err
		}
		incomplete := a.report(results)
		for _, p := range missingTemplates {
			fmt.Fprintf(a.Stdout, "missing  %s\n", rel(a.Dir, p))
		}
		if incomplete || len(missingTemplates) > 0 {
			fmt.Fprintln(a.Stdout, "\nRun `sdd init` (with the same --agent) to create what is missing.")
			return errFailed
		}
		fmt.Fprintln(a.Stdout, "Everything is in place.")
		return nil
	}

	if err := l.Init(); err != nil {
		return err
	}
	results, err := agents.Install(a.Dir, own, false)
	if err != nil {
		return err
	}
	written, err := templates.Install(l.TemplatesDir(), l.ProfilesDir(), *force)
	if err != nil {
		return fmt.Errorf("installing templates: %w", err)
	}
	a.report(results)
	for _, p := range written {
		fmt.Fprintf(a.Stdout, "created  %s\n", rel(a.Dir, p))
	}
	if len(files) > 0 {
		results, err := agents.Install(a.Dir, files, false)
		if err != nil {
			return err
		}
		a.report(results)
	}

	switch *agent {
	case "claude":
		fmt.Fprintln(a.Stdout, "\nClaude Code is set up. Restart it in this project and approve the `sdd` MCP server when asked;\nthen start with /sdd-spec <feature or description>.")
	case "generic":
		fmt.Fprintln(a.Stdout, "\nAGENTS.md explains the workflow to your agent. Register `sdd mcp` (stdio) as an MCP server if your agent supports it.")
	default:
		fmt.Fprintln(a.Stdout, "\nSDD initialized. Next: sdd init --agent claude (or generic), or sdd spec new <feature>")
	}
	return nil
}

// report prints install results and returns whether anything needs attention.
func (a *App) report(results []agents.Result) bool {
	attention := false
	for _, r := range results {
		switch r.Status {
		case agents.Created:
			fmt.Fprintf(a.Stdout, "created  %s\n", r.File.Path)
		case agents.Present:
			fmt.Fprintf(a.Stdout, "ok       %s\n", r.File.Path)
		case agents.Missing:
			attention = true
			fmt.Fprintf(a.Stdout, "missing  %s\n", r.File.Path)
		case agents.Kept:
			attention = true
			fmt.Fprintf(a.Stdout, "kept     %s (exists; not modified)\n", r.File.Path)
			if r.File.Snippet != "" {
				for _, line := range strings.Split(r.File.Snippet, "\n") {
					fmt.Fprintf(a.Stdout, "         %s\n", line)
				}
			}
		}
	}
	return attention
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}
