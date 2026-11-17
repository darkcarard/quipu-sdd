package agents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// HookCommand is the command a Claude Code PreToolUse hook runs.
const HookCommand = "sdd hook pre-tool-use"

// humanOnly finds a human-only sdd command anywhere in a shell command line,
// so wrappers (bash -c "…", env, xargs, $(…), a path to the binary) do not hide it.
var humanOnly = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])sdd(?:\.exe)?\s+((?:spec|plan)\s+approve|approve|revise|resume|clean|merge)\b`)

// Guard decides one Claude Code PreToolUse hook call (its JSON input): it
// returns why a Bash command must be blocked, or "" to let the call through.
// Hooks run on the agent's tool calls only, never on the commands the user
// types with `!`, so this tells the agent apart from the user where
// environment variables cannot.
func Guard(input []byte) string {
	var in struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(input, &in) != nil || in.ToolName != "Bash" {
		return ""
	}
	m := humanOnly.FindStringSubmatch(in.ToolInput.Command)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("`sdd %s` belongs to the user: do not run it in any form (wrappers included). Ask the user to run it with `!`.", strings.Join(strings.Fields(m[1]), " "))
}

// HookResponse is the PreToolUse hook output that denies a tool call.
func HookResponse(reason string) []byte {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
	return append(b, '\n')
}
