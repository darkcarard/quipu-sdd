package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	tests := []struct {
		command string
		blocked string // the subcommand named in the reason; "" lets it through
	}{
		{"sdd approve G2", "approve"},
		{`bash -c "sdd approve G9"`, "approve"},
		{"sh -c 'cd /x && sdd revise G1 -m fix'", "revise"},
		{"env FOO=1 sdd merge", "merge"},
		{"echo G1 | xargs sdd approve", "approve"},
		{"~/go/bin/sdd clean demo --force", "clean"},
		{"x=$(sdd resume demo)", "resume"},
		{"sdd spec approve demo --functional", "spec approve"},
		{"sdd  plan   approve demo", "plan approve"},
		{"sdd status", ""},
		{"sdd review G1 --diff", ""},
		{"sdd spec lint demo", ""},
		{"sdd task done demo --summary 'clean up'", ""},
		{"/sdd-implement demo", ""},
		{"git merge main", ""},
		{"mysdd approve", ""},
	}
	for _, tt := range tests {
		got := Guard(tt.command)
		if (got == "") != (tt.blocked == "") || (tt.blocked != "" && !strings.Contains(got, "`sdd "+tt.blocked+"`")) {
			t.Errorf("%q: got %q, want block of %q", tt.command, got, tt.blocked)
		}
	}
}

func TestHookReply(t *testing.T) {
	// Claude Code and Codex.
	var claude struct {
		H struct{ HookEventName, PermissionDecision, PermissionDecisionReason string } `json:"hookSpecificOutput"`
	}
	out := HookReply([]byte(`{"tool_name":"Bash","tool_input":{"command":"bash -c \"sdd approve G2\""}}`))
	if err := json.Unmarshal(out, &claude); err != nil || claude.H.PermissionDecision != "deny" || claude.H.HookEventName != "PreToolUse" ||
		!strings.Contains(claude.H.PermissionDecisionReason, "`sdd approve`") {
		t.Errorf("claude/codex reply: %s (%v)", out, err)
	}
	// Antigravity.
	var agy struct{ Decision, Reason string }
	out = HookReply([]byte(`{"conversationId":"c","toolCall":{"name":"run_command","args":{"CommandLine":"sdd merge","Cwd":"/x"}}}`))
	if err := json.Unmarshal(out, &agy); err != nil || agy.Decision != "deny" || !strings.Contains(agy.Reason, "`sdd merge`") {
		t.Errorf("antigravity reply: %s (%v)", out, err)
	}
	for _, in := range []string{
		`{"tool_name":"Bash","tool_input":{"command":"sdd status"}}`,
		`{"toolCall":{"name":"run_command","args":{"CommandLine":"sdd review G1"}}}`,
		`{"tool_name":"Read","tool_input":{"command":"sdd approve"}}`,
		`{"toolCall":{"name":"view_file","args":{"CommandLine":"sdd approve"}}}`,
		`not json`,
	} {
		if out := HookReply([]byte(in)); out != nil {
			t.Errorf("%s must pass, got %s", in, out)
		}
	}
}
