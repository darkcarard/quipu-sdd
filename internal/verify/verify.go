// Package verify runs the verification commands declared by a technical spec
// (build, lint, test, integration) in a worktree, with a timeout, and keeps
// the tail of their output for the agent and the review.
package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DefaultTail is how many characters of a command's combined output are kept
// (its tail) unless Options.Tail says otherwise. The output goes back to the
// agent on failure, so it is bounded to keep its token cost low.
const DefaultTail = 6000

// Options control how steps run.
type Options struct {
	Timeout time.Duration // per step; 0 means none
	Tail    int           // characters of output kept; 0 means DefaultTail
}

// Step is one command to run.
type Step struct {
	Name    string // build, lint, test, integration
	Command string
}

// Result is the outcome of one step.
type Result struct {
	Name     string        `json:"name"`
	Command  string        `json:"command"`
	ExitCode int           `json:"exit_code"`
	Passed   bool          `json:"passed"`
	TimedOut bool          `json:"timed_out,omitempty"`
	Duration time.Duration `json:"duration"`
	Output   string        `json:"output,omitempty"` // tail of stdout+stderr
}

// Run executes steps in order in dir and stops at the first failure. A step
// with an empty command is skipped.
func Run(ctx context.Context, dir string, opts Options, steps ...Step) ([]Result, bool) {
	var out []Result
	for _, s := range steps {
		if strings.TrimSpace(s.Command) == "" {
			continue
		}
		r := runOne(ctx, dir, opts, s)
		out = append(out, r)
		if !r.Passed {
			return out, false
		}
	}
	return out, true
}

func runOne(ctx context.Context, dir string, opts Options, s Step) Result {
	timeout := opts.Timeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", s.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", s.Command)
	}
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.WaitDelay = 5 * time.Second // do not hang on children that keep the pipes open
	setProcessGroup(cmd)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	start := time.Now()
	err := cmd.Run()
	r := Result{Name: s.Name, Command: s.Command, Duration: time.Since(start).Round(time.Millisecond), Output: tail(buf.String(), opts.Tail)}
	var exit *exec.ExitError
	switch {
	case err == nil:
		r.Passed = true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.TimedOut, r.ExitCode = true, -1
		r.Output += fmt.Sprintf("\n[killed after %s]", timeout)
	case errors.As(err, &exit):
		r.ExitCode = exit.ExitCode()
	default:
		r.ExitCode = -1
		r.Output += "\n" + err.Error()
	}
	return r
}

func tail(s string, n int) string {
	if n <= 0 {
		n = DefaultTail
	}
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < 200 {
		cut = cut[i+1:]
	}
	return "[… output truncated …]\n" + cut
}

// Summary renders results for the agent and the review: one line per step,
// and the output of the failing one.
func Summary(results []Result) string {
	var sb strings.Builder
	for _, r := range results {
		status := "passed"
		switch {
		case r.TimedOut:
			status = "TIMED OUT"
		case !r.Passed:
			status = fmt.Sprintf("FAILED (exit %d)", r.ExitCode)
		}
		fmt.Fprintf(&sb, "%-11s %s  [%s, %s]\n", r.Name+":", status, r.Command, r.Duration)
	}
	for _, r := range results {
		if !r.Passed {
			fmt.Fprintf(&sb, "\n--- output of %s ---\n%s\n", r.Name, strings.TrimRight(r.Output, "\n"))
		}
	}
	return sb.String()
}
