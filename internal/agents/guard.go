package agents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// HookCommand is the command the agents' PreToolUse hooks run.
const HookCommand = "sdd hook pre-tool-use"

// humanOnly finds a human-only sdd command anywhere in a shell command line,
// so wrappers (bash -c "…", env, xargs, $(…), a path to the binary) do not hide it.
var humanOnly = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])sdd(?:\.exe)?\s+((?:spec|plan)\s+approve|approve|revise|resume|clean|merge)\b`)

// Guard returns why a shell command must not run when an agent issues it, or
// "" when it may.
func Guard(command string) string {
	m := humanOnly.FindStringSubmatch(command)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("`sdd %s` belongs to the user: do not run it in any form (wrappers included). Ask the user to run it themselves.", strings.Join(strings.Fields(m[1]), " "))
}

// HookReply answers one PreToolUse hook call, given its JSON input. It
// understands the Claude Code and Codex format (tool_name "Bash",
// tool_input.command) and the Antigravity one (toolCall.name "run_command",
// toolCall.args.CommandLine), and replies in the same format. It returns nil
// to let the call through. Hooks run on the agent's tool calls only, never on
// commands the user types, which is what tells the two apart.
func HookReply(input []byte) []byte {
	var in struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
		ToolCall *struct {
			Name string `json:"name"`
			Args struct {
				CommandLine string `json:"CommandLine"`
			} `json:"args"`
		} `json:"toolCall"`
	}
	if json.Unmarshal(input, &in) != nil {
		return nil
	}
	var reply map[string]any
	switch {
	case in.ToolCall != nil && in.ToolCall.Name == "run_command":
		if reason := Guard(in.ToolCall.Args.CommandLine); reason != "" {
			reply = map[string]any{"decision": "deny", "reason": reason}
		}
	case in.ToolName == "Bash":
		if reason := Guard(in.ToolInput.Command); reason != "" {
			reply = map[string]any{"hookSpecificOutput": map[string]any{
				"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}}
		}
	}
	if reply == nil {
		return nil
	}
	b, _ := json.Marshal(reply)
	return append(b, '\n')
}
