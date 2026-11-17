package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/lock"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

// errFailed makes a command exit with ExitError after it has already reported why.
var errFailed = errors.New("failed")

// project returns the layout and configuration of an initialized project.
func (a *App) project() (state.Layout, config.Config, error) {
	l := state.Layout{Root: a.Dir}
	cfg, err := config.Load(a.Dir)
	return l, cfg, err
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

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) identity(ctx context.Context) (feature.Identity, error) {
	if a.Identity != nil {
		return a.Identity(ctx)
	}
	return gitIdentity(ctx, a.Dir)
}

// gitIdentity reads git config user.name and user.email.
func gitIdentity(ctx context.Context, dir string) (feature.Identity, error) {
	get := func(key string) string {
		cmd := exec.CommandContext(ctx, "git", "config", "--get", key)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	id := feature.Identity{Name: get("user.name"), Email: get("user.email")}
	if id.Name == "" && id.Email == "" {
		return id, errors.New("git config user.name and user.email are not set: approvals record who approved (run `git config user.email you@example.com`)")
	}
	return id, nil
}

// withLock runs fn while holding the feature lock.
func (a *App) withLock(l state.Layout, name string, fn func() error) error {
	lk, err := lock.Acquire(l.LockFile(name))
	if err != nil {
		return err
	}
	if lk.TookOver != nil {
		fmt.Fprintf(a.Stderr, "warning: took over an abandoned lock (%s)\n", lk.TookOver)
	}
	err = fn()
	return errors.Join(err, lk.Release())
}

// printDiags prints diagnostics compiler-style and returns the error and warning counts.
func (a *App) printDiags(ds diag.List) (int, int) {
	for _, d := range ds.Sorted() {
		fmt.Fprintln(a.Stdout, d)
	}
	return ds.Count()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// featureArgs resolves an optional feature argument to the list of features to act on.
func featureArgs(l state.Layout, args []string) ([]string, error) {
	if len(args) > 0 {
		if err := feature.ValidName(args[0]); err != nil {
			return nil, err
		}
		return args[:1], nil
	}
	return feature.List(l)
}

// artifactFlag returns the artifact selected by --functional / --technical.
func artifactFlag(functional, technical bool) (workflow.Artifact, bool) {
	switch {
	case functional && !technical:
		return workflow.Functional, true
	case technical && !functional:
		return workflow.Technical, true
	}
	return "", false
}
