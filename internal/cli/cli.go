// Package cli implements the sdd command-line interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/templates"
	"github.com/darkcarard/sdd-framework/internal/version"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// App holds the I/O and working directory for a CLI invocation.
type App struct {
	Dir    string // project root (the current directory by default)
	Stdout io.Writer
	Stderr io.Writer
}

type command struct {
	name      string
	summary   string
	milestone string // non-empty for commands not implemented yet
	run       func(a *App, ctx context.Context, args []string) error
}

func (a *App) commands() []command {
	return []command{
		{name: "init", summary: "Install the SDD framework in the current project", run: (*App).runInit},
		{name: "version", summary: "Print the sdd version", run: (*App).runVersion},
		{name: "status", summary: "Show feature state and the next action", milestone: "M1"},
		{name: "spec", summary: "Create, draft (chat), lint and approve specs", milestone: "M1/M3"},
		{name: "plan", summary: "Generate and approve the implementation plan", milestone: "M4"},
		{name: "implement", summary: "Implement the next task group in an isolated worktree", milestone: "M5"},
		{name: "review", summary: "Review the group awaiting approval", milestone: "M5"},
		{name: "approve", summary: "Approve the reviewed group and continue", milestone: "M5"},
		{name: "revise", summary: "Send review feedback to the agent for the current group", milestone: "M5"},
		{name: "resume", summary: "Resume an interrupted run", milestone: "M5"},
		{name: "clean", summary: "Remove a feature worktree", milestone: "M5"},
		{name: "validate", summary: "Run integration tests and sync checks, write the report", milestone: "M6"},
		{name: "sync", summary: "Check consistency between specs, plan and code", milestone: "M1/M6"},
		{name: "trace", summary: "Trace an ID across specs, plan, commits and tests", milestone: "M1/M6"},
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
		default:
			fmt.Fprintf(a.Stderr, "sdd %s: %v\n", c.name, err)
			return ExitError
		}
	}
	fmt.Fprintf(a.Stderr, "sdd: unknown command %q\n\n", args[0])
	a.usage(a.Stderr)
	return ExitUsage
}

var errUsage = errors.New("usage error")

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

func (a *App) runVersion(_ context.Context, args []string) error {
	fs := a.newFlagSet("version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "sdd %s (layout v%d, %s, %s/%s)\n",
		version.Version, version.LayoutVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

func (a *App) runInit(_ context.Context, args []string) error {
	fs := a.newFlagSet("init")
	force := fs.Bool("force", false, "overwrite templates and profiles in .sdd/ with the built-in versions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.Stderr, "usage: sdd init [--force]")
		return errUsage
	}
	l := state.Layout{Root: a.Dir}

	if _, err := os.Stat(filepath.Join(a.Dir, ".git")); err != nil {
		fmt.Fprintln(a.Stderr, "warning: not a git repository; run `git init` before `sdd implement` (worktrees need git)")
	}
	if err := l.Init(); err != nil {
		return err
	}

	created, err := writeIfMissing(l.ConfigFile(), []byte(config.DefaultYAML()))
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(a.Stdout, "created  %s\n", rel(a.Dir, l.ConfigFile()))
	} else {
		fmt.Fprintf(a.Stdout, "kept     %s\n", rel(a.Dir, l.ConfigFile()))
	}

	written, err := templates.Install(l.TemplatesDir(), l.ProfilesDir(), *force)
	if err != nil {
		return fmt.Errorf("installing templates: %w", err)
	}
	for _, p := range written {
		fmt.Fprintf(a.Stdout, "created  %s\n", rel(a.Dir, p))
	}

	added, err := ensureGitignore(l.GitignoreFile(), state.GitignoreEntries)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		fmt.Fprintf(a.Stdout, "updated  .gitignore (%s)\n", strings.Join(added, ", "))
	}

	fmt.Fprintln(a.Stdout, "\nSDD initialized. Next: sdd spec new <feature>")
	return nil
}

func writeIfMissing(path string, data []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// ensureGitignore appends any missing entries under an sdd header and returns those added.
func ensureGitignore(path string, entries []string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, e := range entries {
		if !have[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	var sb strings.Builder
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		sb.WriteString("\n")
	}
	if len(b) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString("# sdd framework (secrets and ephemeral state)\n")
	for _, e := range missing {
		sb.WriteString(e + "\n")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(sb.String()); err != nil {
		return nil, err
	}
	return missing, nil
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
