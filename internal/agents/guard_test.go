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
		in, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": tt.command}})
		got := Guard(in)
		if (got == "") != (tt.blocked == "") || (tt.blocked != "" && !strings.Contains(got, "`sdd "+tt.blocked+"`")) {
			t.Errorf("%q: got %q, want block of %q", tt.command, got, tt.blocked)
		}
	}
	if Guard([]byte(`{"tool_name":"Read","tool_input":{"command":"sdd approve"}}`)) != "" || Guard([]byte("not json")) != "" {
		t.Error("only Bash calls are checked, and bad input passes")
	}
	var out struct {
		H struct{ HookEventName, PermissionDecision, PermissionDecisionReason string } `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(HookResponse("no"), &out); err != nil || out.H.PermissionDecision != "deny" || out.H.HookEventName != "PreToolUse" {
		t.Errorf("response: %+v %v", out, err)
	}
}
