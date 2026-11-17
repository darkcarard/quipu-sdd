package mcpserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/darkcarard/sdd-framework/internal/app"
	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/sddtest"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/templates"
	"github.com/darkcarard/sdd-framework/internal/workflow"
	"github.com/darkcarard/sdd-framework/internal/workspace"
)

// connect starts the server for a fresh project and returns a client session
// that identifies itself as Claude Code.
func connect(t *testing.T) (*mcp.ClientSession, *app.App) {
	t.Helper()
	dir := t.TempDir()
	l := state.Layout{Root: dir}
	if err := l.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.ConfigFile(), []byte(config.DefaultYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := templates.Install(l.TemplatesDir(), l.ProfilesDir(), false); err != nil {
		t.Fatal(err)
	}
	a, err := app.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(a).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, a
}

// call invokes a tool and returns its text and whether it reported an error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestToolsAndInstructions(t *testing.T) {
	cs, _ := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		if strings.Contains(tool.Name, "approve") || strings.Contains(tool.Name, "revise") {
			t.Errorf("human-only operation exposed: %s", tool.Name)
		}
	}
	slices.Sort(names)
	want := slices.Clone(Tools)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
	if ins := cs.InitializeResult().Instructions; !strings.Contains(ins, "Approvals belong to the user") {
		t.Errorf("instructions = %q", ins)
	}
}

func TestDraftingThroughMCP(t *testing.T) {
	cs, a := connect(t)

	out, isErr := call(t, cs, "status", nil)
	if isErr || !strings.Contains(out, "No features yet") {
		t.Fatalf("status: %v %s", isErr, out)
	}

	out, isErr = call(t, cs, "spec_new", map[string]any{"text": "User Signup", "description": "Sign up with email"})
	if isErr || !strings.Contains(out, "feature  user-signup: User Signup") || !strings.Contains(out, "created  specs/user-signup/functional.md") {
		t.Fatalf("spec_new: %v %s", isErr, out)
	}
	events, err := journal.Read(a.Layout.JournalFile("user-signup"))
	if err != nil || len(events) == 0 || events[0].Actor != "agent:claude-code" {
		t.Errorf("the agent must be journaled as actor: %+v %v", events, err)
	}

	out, isErr = call(t, cs, "lint", map[string]any{"feature": "user-signup"})
	if isErr || !strings.Contains(out, "template placeholder") {
		t.Errorf("lint on the template: %v %s", isErr, out)
	}

	// The agent writes the spec; lint and status reflect it.
	if err := os.WriteFile(filepath.Join(a.Layout.SpecDir("user-signup"), "functional.md"), []byte(sddtest.Functional), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = call(t, cs, "lint", map[string]any{"feature": "User Signup"})
	if !strings.Contains(out, "1 file checked: 0 errors, 0 warnings") {
		t.Errorf("lint: %s", out)
	}
	out, _ = call(t, cs, "status", map[string]any{"feature": "user-signup"})
	if !strings.Contains(out, "Phase:   functional-drafting") || !strings.Contains(out, "sdd spec approve user-signup --functional") {
		t.Errorf("status: %s", out)
	}

	out, isErr = call(t, cs, "guide", map[string]any{"topic": "functional", "feature": "user-signup"})
	for _, want := range []string{"# Drafting a functional spec", "## Template", "## This feature", "- Name: `user-signup`", "Sign up with email"} {
		if isErr || !strings.Contains(out, want) {
			t.Errorf("guide missing %q (error %v)", want, isErr)
		}
	}

	out, isErr = call(t, cs, "trace", map[string]any{"id": "AC-002"})
	if isErr || !strings.Contains(out, "AC-002  (user-signup") {
		t.Errorf("trace: %v %s", isErr, out)
	}
	out, isErr = call(t, cs, "sync_check", nil)
	if isErr || !strings.Contains(out, "0 errors") {
		t.Errorf("sync_check: %v %s", isErr, out)
	}
}

// TestPlanningThroughMCP drafts a plan the way an agent does, after the user
// approved both specs.
func TestPlanningThroughMCP(t *testing.T) {
	cs, a := connect(t)
	ctx := context.Background()
	human := a.Who
	a.Who = func(context.Context) (feature.Identity, error) {
		return feature.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	}
	defer func() { a.Who = human }()

	if _, isErr := call(t, cs, "spec_new", map[string]any{"text": "user-signup"}); isErr {
		t.Fatal("spec_new failed")
	}
	dir := a.Layout.SpecDir("user-signup")
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("functional.md", sddtest.Functional)
	if _, err := a.Approve(ctx, "user-signup", workflow.Functional); err != nil {
		t.Fatal(err)
	}
	if out, isErr := call(t, cs, "plan_new", map[string]any{"feature": "user-signup"}); !isErr || !strings.Contains(out, "technical.md does not exist yet") {
		t.Errorf("plan before the technical spec: %v %s", isErr, out)
	}
	call(t, cs, "spec_new", map[string]any{"text": "user-signup", "technical": true})
	write("technical.md", sddtest.Technical)
	if _, err := a.Approve(ctx, "user-signup", workflow.Technical); err != nil {
		t.Fatal(err)
	}

	out, isErr := call(t, cs, "guide", map[string]any{"topic": "plan", "feature": "user-signup"})
	for _, want := range []string{
		"# Drafting the plan", "## Coverage checklist",
		"- **FR-001** Sign up with email — components: SignupService, HTTP handler",
		"  - **AC-002** Given an email that is already registered", "test level: unit, integration",
		"- **NFR-001** Signup responds in p95 < 300 ms. — components: HTTP handler",
		"At most 5 files per task", "> domain → persistence → service → interface → integration", "Language: go",
	} {
		if isErr || !strings.Contains(out, want) {
			t.Errorf("plan guide missing %q", want)
		}
	}

	out, isErr = call(t, cs, "plan_new", map[string]any{"feature": "user-signup"})
	if isErr || !strings.Contains(out, "created  specs/user-signup/plan.md") {
		t.Fatalf("plan_new: %v %s", isErr, out)
	}
	write("plan.md", strings.Replace(sddtest.Plan, "- layer: integration", "- layer: transport", 1))
	out, _ = call(t, cs, "lint", map[string]any{"feature": "user-signup"})
	if !strings.Contains(out, `G3 uses layer "transport", which the technical spec's Architecture section does not name`) {
		t.Errorf("layer check: %s", out)
	}
	write("plan.md", sddtest.Plan)
	out, _ = call(t, cs, "lint", map[string]any{"feature": "user-signup"})
	if !strings.Contains(out, "0 errors, 0 warnings") {
		t.Errorf("lint: %s", out)
	}
	out, _ = call(t, cs, "status", map[string]any{"feature": "user-signup"})
	if !strings.Contains(out, "Phase:   plan-drafting") || !strings.Contains(out, "the user approves it with `sdd plan approve user-signup`") {
		t.Errorf("status: %s", out)
	}
	// The agent cannot approve, even by calling the use case with its identity.
	if _, err := as(a, nil).Approve(ctx, "user-signup", workflow.Plan); !errors.Is(err, feature.ErrHumanOnly) {
		t.Errorf("agent approval: %v", err)
	}
}

// TestImplementationThroughMCP checks that a failed verification reaches the
// agent as a tool error and that the review gate stops it.
func TestImplementationThroughMCP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("verification commands use sh")
	}
	cs, a := connect(t)
	ctx := context.Background()
	g := workspace.Git{Dir: a.Root}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Ada"},
		{"config", "user.email", "ada@example.com"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	human := feature.Identity{Name: "Ada", Email: "ada@example.com"}
	a.Who = func(context.Context) (feature.Identity, error) { return human, nil }
	dir := a.Layout.SpecDir("user-signup")
	os.MkdirAll(dir, 0o755)
	tech := sddtest.Technical
	i, j := strings.Index(tech, "  build:"), strings.Index(tech, "---\n\n#")
	tech = tech[:i] + "  build: \"true\"\n  lint: \"true\"\n  test: test ! -f TEST_FAIL\n  integration: \"true\"\n" + tech[j:]
	for name, content := range map[string]string{"functional.md": sddtest.Functional, "technical.md": tech, "plan.md": sddtest.Plan} {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	for _, art := range workflow.Artifacts() {
		if _, err := a.Approve(ctx, "user-signup", art); err != nil {
			t.Fatal(err)
		}
	}
	g.Run(ctx, "add", ".")
	g.Run(ctx, "commit", "-q", "-m", "specs")
	if _, err := a.StartImplement(ctx, "user-signup"); err != nil {
		t.Fatal(err)
	}

	out, isErr := call(t, cs, "task_next", map[string]any{"feature": "user-signup"})
	if isErr || !strings.Contains(out, "Task T1.1: User entity") {
		t.Fatalf("task_next: %v %s", isErr, out)
	}
	wt := a.Layout.WorktreeDir("user-signup")
	os.MkdirAll(filepath.Join(wt, "internal", "signup"), 0o755)
	os.WriteFile(filepath.Join(wt, "internal", "signup", "user.go"), []byte("package signup\n"), 0o644)
	if out, isErr := call(t, cs, "task_done", map[string]any{"feature": "user-signup", "summary": "entity"}); isErr || !strings.Contains(out, "T1.1 done") {
		t.Fatalf("task_done: %v %s", isErr, out)
	}
	os.WriteFile(filepath.Join(wt, "TEST_FAIL"), []byte("x"), 0o644)
	if out, isErr := call(t, cs, "group_verify", map[string]any{"feature": "user-signup"}); !isErr || !strings.Contains(out, "test:       FAILED") {
		t.Errorf("a failed verification must be a tool error: %v %s", isErr, out)
	}
	os.Remove(filepath.Join(wt, "TEST_FAIL"))
	if out, isErr := call(t, cs, "group_verify", map[string]any{"feature": "user-signup"}); isErr || !strings.Contains(out, "stop: G1 is committed") {
		t.Fatalf("group_verify: %v %s", isErr, out)
	}
	if out, _ := call(t, cs, "task_next", map[string]any{"feature": "user-signup"}); !strings.Contains(out, "G1 awaits the user's review: stop") {
		t.Errorf("review gate: %s", out)
	}
	events, _ := journal.Read(a.Layout.JournalFile("user-signup"))
	agentEvents := 0
	for _, ev := range events {
		if ev.Actor == "agent:claude-code" && (ev.Action == "task.verify" || ev.Action == "group.verify") {
			agentEvents++
		}
	}
	if agentEvents < 4 {
		t.Errorf("the agent's verifications must be journaled as agent:claude-code (%d found)", agentEvents)
	}
}

func TestToolErrorsReachTheAgent(t *testing.T) {
	cs, _ := connect(t)
	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"spec_new", map[string]any{}, "pass text"},
		{"spec_new", map[string]any{"text": "x", "technical": true}, "feature not found"},
		{"plan_new", map[string]any{"feature": "nope"}, "feature not found"},
		{"task_next", map[string]any{"feature": "nope"}, "feature not found"},
		{"guide", map[string]any{"topic": "poetry"}, "unknown guide topic"},
		{"trace", map[string]any{"id": "bogus"}, "not a traceable ID"},
	}
	for _, tt := range tests {
		out, isErr := call(t, cs, tt.tool, tt.args)
		if !isErr || !strings.Contains(out, tt.want) {
			t.Errorf("%s %v: error=%v %q, want %q", tt.tool, tt.args, isErr, out, tt.want)
		}
	}
	// Missing required arguments are rejected by schema validation.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "guide", Arguments: map[string]any{}})
	if err == nil && !res.IsError {
		t.Error("a call without the required topic must fail")
	}
}
