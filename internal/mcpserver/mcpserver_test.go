package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/darkcarard/sdd-framework/internal/app"
	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/sddtest"
	"github.com/darkcarard/sdd-framework/internal/state"
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
		{"guide", map[string]any{"topic": "plan"}, "M4"},
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
