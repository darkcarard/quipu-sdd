package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/sddtest"
)

// runAs runs the CLI with a fixed identity and clock.
func runAs(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{
		Dir: dir, Stdout: &out, Stderr: &errb,
		Identity: func(context.Context) (feature.Identity, error) {
			return feature.Identity{Name: "Ada", Email: "ada@example.com"}, nil
		},
		Now: func() time.Time { return time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC) },
	}
	code := app.Run(context.Background(), args)
	return code, out.String(), errb.String()
}

func mustRun(t *testing.T, dir string, wantCode int, args ...string) string {
	t.Helper()
	code, out, errOut := runAs(t, dir, args...)
	if code != wantCode {
		t.Fatalf("sdd %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, wantCode, out, errOut)
	}
	return out + errOut
}

func writeSpec(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "specs", sddtest.Feature, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

// TestHandWrittenSpecWorkflow is the M1 exit criterion: a spec written by hand can
// be linted, approved and traced, and editing it marks downstream artifacts stale.
func TestHandWrittenSpecWorkflow(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, ExitOK, "init")
	contains(t, mustRun(t, dir, ExitOK, "status"), "No features yet")

	contains(t, mustRun(t, dir, ExitError, "spec", "new", "x", "--name", "Bad_Name"), "invalid feature name")
	contains(t, mustRun(t, dir, ExitOK, "spec", "new", sddtest.Feature), "created  specs/user-signup/functional.md")
	contains(t, mustRun(t, dir, ExitError, "spec", "lint"), "template placeholder <requirement title>")
	contains(t, mustRun(t, dir, ExitError, "spec", "approve", sddtest.Feature, "--functional"), "not approved")

	writeSpec(t, dir, "functional.md", sddtest.Functional)
	contains(t, mustRun(t, dir, ExitOK, "spec", "lint", sddtest.Feature), "1 file checked: 0 errors, 0 warnings")
	contains(t, mustRun(t, dir, ExitError, "spec", "new", sddtest.Feature, "--technical"), "approve it first")
	contains(t, mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--functional"),
		"approved specs/user-signup/functional.md", "phase:   functional-approved", "sdd spec new user-signup --technical")
	contains(t, mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--functional"), "already approved")

	mustRun(t, dir, ExitOK, "spec", "new", "--technical", sddtest.Feature)
	writeSpec(t, dir, "technical.md", strings.Replace(sddtest.Technical, "| FR-002 | Mailer | |\n", "", 1))
	contains(t, mustRun(t, dir, ExitError, "sync", "check", sddtest.Feature), "FR-002 is not mapped to any component")
	contains(t, mustRun(t, dir, ExitError, "spec", "approve", sddtest.Feature, "--technical"), "FR-002 is not mapped")
	writeSpec(t, dir, "technical.md", sddtest.Technical)
	mustRun(t, dir, ExitOK, "spec", "approve", sddtest.Feature, "--technical")

	mustRun(t, dir, ExitOK, "plan", "new", sddtest.Feature)
	writeSpec(t, dir, "plan.md", sddtest.Plan)
	contains(t, mustRun(t, dir, ExitOK, "plan", "approve", sddtest.Feature), "phase:   plan-approved", "sdd implement user-signup")
	contains(t, mustRun(t, dir, ExitOK, "sync", "check"), "0 errors, 0 warnings")

	contains(t, mustRun(t, dir, ExitOK, "trace", "AC-002"),
		"AC-002  (user-signup · specs/user-signup/functional.md:", "technical: unit, integration", "plan:      T2.1")
	contains(t, mustRun(t, dir, ExitOK, "trace", "user-signup/T3.1"), "covers:    FR-002, AC-003")
	contains(t, mustRun(t, dir, ExitError, "trace", "FR-042"), "not defined in any feature")
	contains(t, mustRun(t, dir, ExitError, "trace", "bogus"), "not a traceable ID")
	contains(t, mustRun(t, dir, ExitOK, "trace", "--matrix", sddtest.Feature), "| FR-001 | — | Sign up with email | SignupService, HTTP handler | T1.1, T2.1 |")

	// Editing the approved functional spec marks the technical spec and the plan stale.
	writeSpec(t, dir, "functional.md", strings.Replace(sddtest.Functional, "p95 < 300 ms", "p95 < 250 ms", 1))
	contains(t, mustRun(t, dir, ExitOK, "status"), "user-signup  User Signup  functional-drafting  modified    stale      stale")
	contains(t, mustRun(t, dir, ExitOK, "status", sddtest.Feature),
		"Phase:   functional-drafting (state.json still says plan-approved", "technical.md   stale",
		"Next: review the edits to specs/user-signup/functional.md, then the user re-approves it with `sdd spec approve user-signup --functional`")
	contains(t, mustRun(t, dir, ExitError, "sync", "check"), "plan.md is stale: functional.md changed after it was approved")

	if _, err := os.Stat(filepath.Join(dir, ".sdd", "locks", sddtest.Feature+".lock")); !os.IsNotExist(err) {
		t.Errorf("the feature lock must be released: %v", err)
	}
}

func TestFreeTextFeatureNames(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, ExitOK, "init")
	contains(t, mustRun(t, dir, ExitOK, "spec", "new", "Registro de Usuarios (v2)", "--description", "Alta con email"),
		"feature  registro-de-usuarios-v2: Registro de Usuarios (v2)", "created  specs/registro-de-usuarios-v2/functional.md")
	b, err := os.ReadFile(filepath.Join(dir, "specs", "registro-de-usuarios-v2", "functional.md"))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, string(b), "id: registro-de-usuarios-v2", "title: Registro de Usuarios (v2)", "Alta con email")
	st, err := os.ReadFile(filepath.Join(dir, ".sdd", "features", "registro-de-usuarios-v2", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, string(st), `"title": "Registro de Usuarios (v2)"`, `"description": "Alta con email"`, `"created_by": "Ada <ada@example.com>"`)

	// The same text maps to the existing feature; --name picks another one.
	contains(t, mustRun(t, dir, ExitError, "spec", "new", "registro de usuarios (V2)"), "already exists: choose another name with --name")
	contains(t, mustRun(t, dir, ExitOK, "spec", "new", "Registro de Usuarios (v2)", "--name", "signup-v3"), "feature  signup-v3: Registro de Usuarios (v2)")

	// Other commands accept the name, the text or the title.
	for _, arg := range []string{"registro-de-usuarios-v2", "Registro de usuarios (v2)"} {
		contains(t, mustRun(t, dir, ExitOK, "status", arg), "Feature: registro-de-usuarios-v2 (Registro de Usuarios (v2))", "Alta con email")
	}
	contains(t, mustRun(t, dir, ExitOK, "status"), "registro-de-usuarios-v2  Registro de Usuarios (v2)")

	contains(t, mustRun(t, dir, ExitOK, "feature", "describe", "signup-v3", "--title", "Signup v3", "--description", "Nueva versión"),
		"title:       Signup v3", "description: Nueva versión")
	contains(t, mustRun(t, dir, ExitOK, "feature", "describe", "Signup V3"), "feature:     signup-v3", "created:     2026-11-17T10:00:00Z by Ada <ada@example.com>")
	contains(t, mustRun(t, dir, ExitOK, "feature", "describe", "signup-v3", "--title", "Signup v3"), "nothing changed")
	contains(t, mustRun(t, dir, ExitError, "feature", "describe", "signup-v3", "--title", ""), "cannot be empty")
	if code, _, _ := runAs(t, dir, "feature", "rename", "x"); code != ExitUsage {
		t.Errorf("unknown feature subcommand: exit %d", code)
	}

	contains(t, mustRun(t, dir, ExitError, "spec", "new", "con"), "reserved")
	contains(t, mustRun(t, dir, ExitError, "spec", "new", "x", "--name", "approve"), "reserved")
	contains(t, mustRun(t, dir, ExitError, "spec", "new", "¿?"), "cannot derive a feature name")
	contains(t, mustRun(t, dir, ExitError, "status", "nope"), "feature not found")
}

func TestSubcommandUsage(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, ExitOK, "init")
	for _, args := range [][]string{
		{"spec"}, {"spec", "bogus"}, {"spec", "approve", "x"}, {"spec", "approve", "x", "--functional", "--technical"},
		{"sync"}, {"trace"}, {"plan", "approve"}, {"status", "a", "b"},
	} {
		if code, _, _ := runAs(t, dir, args...); code != ExitUsage {
			t.Errorf("sdd %s: exit %d, want usage error", strings.Join(args, " "), code)
		}
	}
	contains(t, mustRun(t, dir, ExitUsage, "spec", "chat", "x"), "usage: sdd spec new")
	contains(t, mustRun(t, dir, ExitUsage, "plan", "x"), "usage: sdd plan new")
	contains(t, mustRun(t, dir, ExitOK, "guide", "workflow"), "Approvals belong to the user")
	contains(t, mustRun(t, dir, ExitOK, "guide", "implement"), "# Implementing a feature")
	contains(t, mustRun(t, dir, ExitUsage, "guide"), "usage: sdd guide")
	contains(t, mustRun(t, dir, ExitError, "status", "missing"), "feature not found")
}

func TestLintReportsInvalidFeatureDir(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, ExitOK, "init")
	if err := os.MkdirAll(filepath.Join(dir, "specs", "Bad_Dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	contains(t, mustRun(t, dir, ExitError, "spec", "lint"), "specs/Bad_Dir: error: invalid feature name")
	contains(t, mustRun(t, dir, ExitOK, "status"), "Bad_Dir")
}

func TestCommandsWorkFromSubdirectories(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, ExitOK, "init")
	mustRun(t, dir, ExitOK, "spec", "new", "User Signup")
	sub := filepath.Join(dir, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	contains(t, mustRun(t, sub, ExitOK, "status"), "user-signup  User Signup")
	contains(t, mustRun(t, sub, ExitOK, "guide", "technical", "--feature", "user-signup"), "# Drafting a technical spec", "functional_hash")
}

func TestCommandsNeedInit(t *testing.T) {
	contains(t, mustRun(t, t.TempDir(), ExitError, "status"), "run `sdd init` first")
}
