//go:build unix

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkcarard/quipu-sdd/internal/sddtest"
	"github.com/darkcarard/quipu-sdd/internal/workspace"
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
		"G1 verified and committed", "feat(user-signup): G1 Domain model", "sdd review G1")

	out := mustRun(t, dir, ExitOK, "status", sddtest.Feature)
	contains(t, out, "Implementation (worktree .sdd/worktrees/user-signup, branch sdd/user-signup):",
		"G1   awaiting-review  1/1 tasks  Domain model", "Next: review G1 with `sdd review G1`")
	contains(t, mustRun(t, dir, ExitOK, "review", "g1", "--diff"),
		"Group G1: Domain model  [awaiting-review]", "agent: User entity", "internal/signup/user.go", "test:       passed")
	// Without an editor (SDD_REVIEW_EDITOR=none in TestMain) the diff is printed.
	contains(t, mustRun(t, dir, ExitOK, "review"), "Showing the diff here: review.editor is none", "+++ b/internal/signup/user.go",
		"`/sdd-review G1` in your agent")
	contains(t, mustRun(t, dir, ExitOK, "review", "G1", "--no-open"), "Group G1: Domain model")
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Rules\n"), 0o644)
	contains(t, mustRun(t, dir, ExitOK, "review", "G1", "--context"), "# Group review: user-signup G1 (Domain model)",
		"covers AC-001 (FR-001)", "Review as a senior go engineer", "Project guardrails: CLAUDE.md", ".sdd/features/user-signup/reviews/G1.md", "`sdd approve G1`")
	mustRun(t, dir, ExitUsage, "review", "G1", "--context", "--diff")
	contains(t, mustRun(t, dir, ExitError, "approve", "G2"), "no feature being implemented has G2 as its current group")
	contains(t, mustRun(t, dir, ExitError, "revise", "G1"), "feedback is required")
	contains(t, mustRun(t, dir, ExitOK, "revise", "G1", "-m", "Add a doc comment"), "G1 is back in progress", "`/sdd-implement user-signup` in your agent (`$sdd-implement` in Codex)")
	contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature), "The user asked for changes:", "Add a doc comment")
	mustRun(t, dir, ExitOK, "group", "verify", sddtest.Feature)
	contains(t, mustRun(t, dir, ExitOK, "approve"), "approved G1", "start G2")

	// Phase 4 checks already apply to approved groups.
	contains(t, mustRun(t, dir, ExitError, "sync", "check", sddtest.Feature), "AC-001 is not referenced by any test")
	contains(t, mustRun(t, dir, ExitOK, "trace", "FR-001"), "feat(user-signup): G1 Domain model", "tests:     (none)")
	contains(t, mustRun(t, dir, ExitOK, "trace", "--matrix", sddtest.Feature), "| Commits | Tests |")
	contains(t, mustRun(t, dir, ExitError, "validate", sddtest.Feature), "validation needs every group approved")
	contains(t, mustRun(t, dir, ExitOK, "review", sddtest.Feature, "--context"), "# Deep review: user-signup", "### AC-001 (FR-001)")

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

// TestApprovalCommitsAndMerge follows a feature from approval to merge: every
// approval commits the specs and records, the review commands take group IDs,
// and `sdd merge` merges into the project's branch even when run from inside
// the worktree.
// validatedProject implements and validates the fixture feature through the
// CLI, with the review commands addressed by group ID. It returns the project,
// the worktree and a git runner.
func validatedProject(t *testing.T) (string, string, func(args ...string) string) {
	t.Helper()
	dir := gitProject(t)
	ctx := context.Background()
	g := workspace.Git{Dir: dir}
	git := func(args ...string) string {
		t.Helper()
		out, err := g.Run(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	contains(t, git("log", "--format=%s", "main"), "sdd(user-signup): approve the functional spec",
		"sdd(user-signup): approve the technical spec", "sdd(user-signup): approve the plan")
	contains(t, git("show", "--name-only", "--format=%B", ":/approve the plan"), "SDD-Feature: user-signup",
		"specs/user-signup/plan.md", ".sdd/features/user-signup/state.json")

	wt := filepath.Join(dir, ".sdd", "worktrees", sddtest.Feature)
	files := map[string]map[string]string{
		"T1.1": {"internal/signup/user.go": "package signup\n", "internal/signup/user_test.go": "package signup\n// covers: AC-001\n"},
		"T2.1": {"internal/signup/service.go": "package signup\n", "internal/signup/service_test.go": "package signup\n// covers: AC-002, NFR-001\n"},
		"T3.1": {"internal/mail/mail.go": "package mail\n", "internal/mail/mail_test.go": "package mail\n// covers: AC-003\n"},
	}
	contains(t, mustRun(t, dir, ExitError, "merge"), "no validated feature is waiting to be merged")
	mustRun(t, dir, ExitOK, "implement", sddtest.Feature)
	for i, task := range []string{"T1.1", "T2.1", "T3.1"} {
		id := fmt.Sprintf("G%d", i+1)
		contains(t, mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature), "Task "+task)
		for rel, content := range files[task] {
			os.MkdirAll(filepath.Dir(filepath.Join(wt, rel)), 0o755)
			os.WriteFile(filepath.Join(wt, rel), []byte(content), 0o644)
		}
		mustRun(t, dir, ExitOK, "task", "done", sddtest.Feature)
		contains(t, mustRun(t, dir, ExitOK, "group", "verify", sddtest.Feature), "sdd review "+id)
		contains(t, mustRun(t, wt, ExitOK, "review", id), "Group "+id, "`sdd approve "+id+"`")
		contains(t, mustRun(t, wt, ExitOK, "approve", id), "approved "+id)
	}
	contains(t, mustRun(t, dir, ExitError, "merge", sddtest.Feature), "only a validated feature can be merged")
	contains(t, mustRun(t, dir, ExitOK, "validate", sddtest.Feature), "validation PASSED", "`sdd merge user-signup`")
	return dir, wt, git
}

func TestApprovalCommitsAndMerge(t *testing.T) {
	dir, wt, git := validatedProject(t)

	// From inside the worktree, as an agent's shared shell would be.
	out := mustRun(t, filepath.Join(wt, "internal"), ExitOK, "merge")
	contains(t, out, "merged sdd/user-signup into main (fast-forward", "removed the worktree",
		"committed the specs and records", "Next: nothing: sdd/user-signup was merged into main")
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("the worktree must be removed: %v", err)
	}
	contains(t, git("log", "--format=%s", "main"), "sdd(user-signup): records of the merge into main",
		"feat(user-signup): G3 Confirmation mail")
	if st := git("status", "--porcelain", "--", "specs", ".sdd"); st != "" {
		t.Errorf("records left uncommitted:\n%s", st)
	}
	contains(t, mustRun(t, dir, ExitError, "merge", sddtest.Feature), "already merged into main")
	contains(t, mustRun(t, dir, ExitOK, "status", sddtest.Feature), "git branch -d sdd/user-signup")
}

// TestMergeRecordsAnOutsideMerge: a feature merged with git by hand (branch
// deleted, worktree removed) is recorded by `sdd merge` without merging again.
func TestMergeRecordsAnOutsideMerge(t *testing.T) {
	dir, wt, git := validatedProject(t)
	git("merge", "--ff-only", "sdd/user-signup")
	git("worktree", "remove", wt)
	git("branch", "-d", "sdd/user-signup")
	contains(t, mustRun(t, dir, ExitOK, "clean", sddtest.Feature), "nothing to remove")
	contains(t, mustRun(t, dir, ExitOK, "merge"), "sdd/user-signup was already merged into main: recorded the merge",
		"committed the specs and records", "Next: nothing: sdd/user-signup was merged into main")
	contains(t, git("log", "-1", "--format=%s"), "sdd(user-signup): records of the merge into main")
}

// A deleted branch whose work never reached the current branch is not recorded.
func TestMergeNeedsTheWorkOnTheBranch(t *testing.T) {
	dir, _, git := validatedProject(t)
	mustRun(t, dir, ExitOK, "clean", sddtest.Feature)
	git("branch", "-D", "sdd/user-signup")
	contains(t, mustRun(t, dir, ExitError, "merge", sddtest.Feature), "branch sdd/user-signup not found, and its validated commit")
}
