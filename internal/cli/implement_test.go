//go:build unix

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkcarard/sdd-framework/internal/sddtest"
	"github.com/darkcarard/sdd-framework/internal/workspace"
)

// gitProject initializes sdd in a git repository with the fixture feature's
// specs and plan approved and committed. Its verification commands fail on
// demand (BUILD_FAIL / TEST_FAIL files in the worktree).
func gitProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	g := workspace.Git{Dir: dir}
	ctx := context.Background()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Ada"},
		{"config", "user.email", "ada@example.com"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, dir, ExitOK, "init", "--agent", "claude")
	mustRun(t, dir, ExitOK, "spec", "new", sddtest.Feature)
	writeSpec(t, dir, "functional.md", sddtest.Functional)
	mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--functional")
	tech := sddtest.Technical
	i, j := strings.Index(tech, "  build:"), strings.Index(tech, "---\n\n#")
	tech = tech[:i] + "  build: test ! -f BUILD_FAIL\n  lint: \"true\"\n  test: test ! -f TEST_FAIL\n  integration: \"true\"\n" + tech[j:]
	writeSpec(t, dir, "technical.md", tech)
	mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--technical")
	writeSpec(t, dir, "plan.md", sddtest.Plan)
	mustRun(t, dir, ExitOK, "plan", "approve", sddtest.Feature)
	if _, err := g.Run(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(ctx, "commit", "-q", "-m", "specs"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestImplementWorkflowCLI(t *testing.T) {
	dir := gitProject(t)
	wt := filepath.Join(dir, ".sdd", "worktrees", sddtest.Feature)
	put := func(rel string) {
		os.MkdirAll(filepath.Dir(filepath.Join(wt, rel)), 0o755)
		os.WriteFile(filepath.Join(wt, rel), []byte("x\n"), 0o644)
	}

	contains(t, mustRun(t, dir, ExitOK, "status", sddtest.Feature), "start implementing with `sdd implement user-signup`")
	contains(t, mustRun(t, dir, ExitError, "task", "next", sddtest.Feature), "not being implemented")
	contains(t, mustRun(t, dir, ExitOK, "implement", sddtest.Feature),
		"created worktree .sdd/worktrees/user-signup on branch sdd/user-signup", "G1: Domain model is in progress", "/sdd-implement user-signup")

	contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature),
		"Task T1.1: User entity", "Worktree:  "+wt, "- AC-001 (FR-001): Given a new email", "Language profile (.sdd/profiles/go.md)")
	put("BUILD_FAIL")
	contains(t, mustRun(t, dir, ExitError, "task", "done", sddtest.Feature), "build:      FAILED", "T1.1 is not done")
	os.Remove(filepath.Join(wt, "BUILD_FAIL"))
	put("internal/signup/user.go")
	contains(t, mustRun(t, dir, ExitOK, "task", "done", sddtest.Feature, "--summary", "User entity"), "T1.1 done (checkpoint")
	contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature), "every task of this group is done: call group_verify")
	contains(t, mustRun(t, dir, ExitOK, "group", "verify", sddtest.Feature),
		"G1 verified and committed", "feat(user-signup): G1 Domain model", "sdd review user-signup")

	out := mustRun(t, dir, ExitOK, "status", sddtest.Feature)
	contains(t, out, "Implementation (worktree .sdd/worktrees/user-signup, branch sdd/user-signup):",
		"G1   awaiting-review  1/1 tasks  Domain model", "Next: review G1 with `sdd review user-signup`")
	contains(t, mustRun(t, dir, ExitOK, "review", sddtest.Feature, "--diff"),
		"Group G1: Domain model  [awaiting-review]", "agent: User entity", "internal/signup/user.go", "test:       passed")
	contains(t, mustRun(t, dir, ExitError, "revise", sddtest.Feature), "feedback is required")
	contains(t, mustRun(t, dir, ExitOK, "revise", sddtest.Feature, "-m", "Add a doc comment"), "G1 is back in progress")
	contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature), "The user asked for changes:", "Add a doc comment")
	mustRun(t, dir, ExitOK, "group", "verify", sddtest.Feature)
	contains(t, mustRun(t, dir, ExitOK, "approve", sddtest.Feature), "approved G1", "start G2")

	// Interruption and recovery.
	contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature), "Task T2.1: Register")
	put("internal/signup/service.go")
	contains(t, mustRun(t, dir, ExitOK, "resume", sddtest.Feature), "kept uncommitted changes for the agent: internal/signup/service.go")
	contains(t, mustRun(t, dir, ExitError, "clean", sddtest.Feature), "uncommitted changes")
	contains(t, mustRun(t, dir, ExitOK, "resume", sddtest.Feature, "--discard"), "discarded uncommitted changes")
	contains(t, mustRun(t, dir, ExitOK, "clean", sddtest.Feature), "branch sdd/user-signup is kept")
	contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature), "Task T2.1: Register  (resumed)")
	if _, err := os.Stat(filepath.Join(wt, "internal/signup/user.go")); err != nil {
		t.Errorf("the recreated worktree must contain G1's commit: %v", err)
	}

	// Commands also work from inside the worktree.
	contains(t, mustRun(t, filepath.Join(wt, "internal"), ExitOK, "status"), "user-signup  User Signup  implementing")
}
