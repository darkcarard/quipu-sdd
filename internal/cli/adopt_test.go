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

// TestAdoptionThenNewFeature is the M7 exit criterion: an existing project is
// adopted into baseline specs without changing its code, and a new feature
// that depends on the baseline goes through all four phases.
func TestAdoptionThenNewFeature(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := workspace.Git{Dir: dir}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Ada"},
		{"config", "user.email", "ada@example.com"}, {"config", "commit.gpgsign", "false"}} {
		g.Run(ctx, args...)
	}
	existing := map[string]string{
		"go.mod":                         "module example.com/shop\n",
		"internal/accounts/account.go":   "package accounts\n",
		"internal/accounts/account_test": "package accounts\n// covers: AC-001\n",
	}
	for p, c := range existing {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644)
	}
	g.Run(ctx, "add", ".")
	g.Run(ctx, "commit", "-q", "-m", "existing project")

	mustRun(t, dir, ExitOK, "init", "--agent", "claude")
	contains(t, mustRun(t, dir, ExitOK, "adopt", "--context"), "# Project inventory", "From go.mod: build `go build ./...`", "Next: follow `guide adopt`")
	contains(t, mustRun(t, dir, ExitOK, "guide", "adopt"), "# Adopting an existing project")

	// Adopt: a baseline feature that documents the existing code.
	commands := "  build: \"true\"\n  lint: \"true\"\n  test: \"true\"\n  integration: \"true\"\n"
	withCommands := func(tech string) string {
		i, j := strings.Index(tech, "  build:"), strings.Index(tech, "---\n\n#")
		return tech[:i] + commands + tech[j:]
	}
	mustRun(t, dir, ExitOK, "spec", "new", "Accounts", "--baseline")
	base := strings.Replace(strings.Replace(sddtest.Functional, "id: user-signup", "id: accounts", 1), "depends_on: []", "depends_on: []\norigin: existing", 1)
	os.WriteFile(filepath.Join(dir, "specs", "accounts", "functional.md"), []byte(base), 0o644)
	mustRun(t, dir, ExitOK, "spec", "approve", "accounts", "--functional")
	mustRun(t, dir, ExitOK, "spec", "new", "accounts", "--technical")
	os.WriteFile(filepath.Join(dir, "specs", "accounts", "technical.md"),
		[]byte(strings.Replace(withCommands(sddtest.Technical), "id: user-signup", "id: accounts", 1)), 0o644)
	contains(t, mustRun(t, dir, ExitOK, "spec", "approve", "accounts", "--technical"), "phase:   baselined", "depends_on: [accounts]")
	contains(t, mustRun(t, dir, ExitError, "plan", "new", "accounts"), "a baseline feature has no plan")
	contains(t, mustRun(t, dir, ExitOK, "trace", "accounts/AC-001"), "plan:      (baseline: existing code)", "tests:     internal/accounts/account_test:2")
	contains(t, mustRun(t, dir, ExitOK, "status"), "accounts  Accounts  baselined  approved    approved   n/a")

	// The existing code is untouched: only new files were added.
	status, _ := g.Run(ctx, "status", "--porcelain")
	for _, line := range strings.Split(status, "\n") {
		if line != "" && !strings.HasPrefix(line, "??") {
			t.Errorf("adoption changed an existing file: %q", line)
		}
	}
	g.Run(ctx, "add", ".")
	g.Run(ctx, "commit", "-q", "-m", "adopt accounts")

	// A new feature that builds on the baseline goes through every phase.
	mustRun(t, dir, ExitOK, "spec", "new", sddtest.Feature)
	writeSpec(t, dir, "functional.md", strings.Replace(sddtest.Functional, "depends_on: []", "depends_on: [accounts]", 1))
	mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--functional")
	mustRun(t, dir, ExitOK, "spec", "new", sddtest.Feature, "--technical")
	writeSpec(t, dir, "technical.md", withCommands(sddtest.Technical))
	mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--technical")
	mustRun(t, dir, ExitOK, "plan", "new", sddtest.Feature)
	writeSpec(t, dir, "plan.md", sddtest.Plan)
	mustRun(t, dir, ExitOK, "plan", "approve", sddtest.Feature)
	contains(t, mustRun(t, dir, ExitOK, "sync", "check"), "0 errors")
	g.Run(ctx, "add", ".")
	g.Run(ctx, "commit", "-q", "-m", "user-signup specs")

	mustRun(t, dir, ExitOK, "implement", sddtest.Feature)
	wt := filepath.Join(dir, ".sdd", "worktrees", sddtest.Feature)
	tests := map[string]string{"T1.1": "AC-001", "T2.1": "AC-002", "T3.1": "AC-003"}
	for range 3 {
		out := mustRun(t, dir, ExitOK, "task", "next", sddtest.Feature)
		var task string
		for id := range tests {
			if strings.Contains(out, "Task "+id+":") {
				task = id
			}
		}
		if task == "" {
			t.Fatalf("unexpected task_next:\n%s", out)
		}
		os.MkdirAll(filepath.Join(wt, "internal", "signup"), 0o755)
		os.WriteFile(filepath.Join(wt, "internal", "signup", task+"_test.go"), []byte("package signup\n// covers: "+tests[task]+"\n"), 0o644)
		mustRun(t, dir, ExitOK, "task", "done", sddtest.Feature)
		mustRun(t, dir, ExitOK, "group", "verify", sddtest.Feature)
		mustRun(t, dir, ExitOK, "approve", sddtest.Feature)
	}
	contains(t, mustRun(t, dir, ExitOK, "validate", sddtest.Feature), "validation PASSED")
	contains(t, mustRun(t, dir, ExitOK, "status"), "user-signup  User Signup  validated")

	// When the new feature changes adopted behavior, the baseline is revised and re-approved.
	os.WriteFile(filepath.Join(dir, "specs", "accounts", "functional.md"), []byte(strings.Replace(base, "p95 < 300 ms", "p95 < 200 ms", 1)), 0o644)
	contains(t, mustRun(t, dir, ExitOK, "status", "accounts"), "Phase:   functional-drafting")
	mustRun(t, dir, ExitOK, "spec", "approve", "accounts", "--functional")
	contains(t, mustRun(t, dir, ExitOK, "spec", "approve", "accounts", "--technical"), "phase:   baselined")
}
