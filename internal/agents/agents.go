// Package agents holds the integration files that `sdd init --agent <name>`
// installs for a coding agent, and installs them without ever modifying a
// file that already exists.
package agents

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

//go:embed files
var files embed.FS

// Names lists the supported agents.
var Names = []string{"claude", "codex", "antigravity", "generic"}

// DeniedCommands are the human-only commands the agent must not run.
var DeniedCommands = []string{"sdd spec approve", "sdd plan approve", "sdd approve", "sdd revise", "sdd resume", "sdd clean", "sdd merge"}

// File is one integration file.
type File struct {
	// Path is relative to the project root, with forward slashes.
	Path    string
	Content []byte
	// satisfied reports whether an existing file already provides what this
	// file would (nil: only identical content does).
	satisfied func(existing []byte) bool
	// Snippet is what to add by hand when the file exists but lacks the integration.
	Snippet string
}

// Files returns the integration files of an agent. workflow is the text of the
// workflow guide, embedded in AGENTS.md.
func Files(agent, workflow string) ([]File, error) {
	switch agent {
	case "claude":
		return claudeFiles()
	case "codex":
		return codexFiles(workflow)
	case "antigravity":
		return antigravityFiles(workflow)
	case "generic":
		return []File{agentsMD(workflow)}, nil
	}
	return nil, fmt.Errorf("unknown agent %q (supported: %s)", agent, strings.Join(Names, ", "))
}

func agentsMD(workflow string) File {
	b, _ := files.ReadFile("files/generic/AGENTS.md")
	content := strings.Replace(string(b), "{{workflow}}", strings.TrimPrefix(strings.TrimSpace(workflow), "# Working with sdd\n\n"), 1)
	return File{Path: "AGENTS.md", Content: []byte(content),
		Snippet: "Add a section that tells agents to run `sdd guide workflow` and follow it."}
}

// skillVars fill the placeholders of the shared skill sources for one family
// of agents: Claude Code passes the arguments as $ARGUMENTS; Codex and
// Antigravity read the skill's input from the user's request.
var skillVars = map[string]map[string]string{
	"claude": {
		"{{args}}": "$ARGUMENTS",
		"{{tell}}": "sdd commands as `! sdd approve G2` (run in this session's shell), the skill as `/sdd-implement` (no `!`)",
		// The sdd-reviewer subagent (.claude/agents) does the review in its own
		// context, so the session only receives its summary.
		"{{reviewer}}": "Launch the `sdd-reviewer` subagent with the Agent tool and give it only the group (and the feature, if given): it follows the steps below in its own context and returns a summary. Do the steps yourself only if that subagent is not available.",
	},
	"agents": {
		"{{args}}":     "as given in the user's request (none if they gave none)",
		"{{tell}}":     "sdd commands to run in their terminal (`sdd approve G2`), and this skill to continue (`$sdd-implement` in Codex, `/sdd-implement` in Antigravity)",
		"{{reviewer}}": "If you can hand work to a subagent, give it the steps below with the group (and the feature, if given), so the review runs in its own context; otherwise do them yourself.",
	},
}

// skills renders the shared skills under dir (".claude/skills" or ".agents/skills").
func skills(dir, family string) ([]File, error) {
	var out []File
	err := fs.WalkDir(files, "files/skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		content := string(b)
		for k, v := range skillVars[family] {
			content = strings.ReplaceAll(content, k, v)
		}
		out = append(out, File{Path: dir + "/" + strings.TrimPrefix(p, "files/skills/"), Content: []byte(content),
			Snippet: "This file differs from the built-in version; delete it and run `sdd init` with the same --agent again to install the current one."})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// hookConfig is the PreToolUse hook entry for agents that share Claude Code's
// hooks format (Claude Code, Codex).
var hookConfig = map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": HookCommand}}}

func hasHookCommand(b []byte) bool { return bytes.Contains(b, []byte(HookCommand)) }

func claudeFiles() ([]File, error) {
	out, err := skills(".claude/skills", "claude")
	if err != nil {
		return nil, err
	}
	reviewer, err := files.ReadFile("files/claude/agents/sdd-reviewer.md")
	if err != nil {
		return nil, err
	}
	out = append(out, File{Path: ".claude/agents/sdd-reviewer.md", Content: reviewer,
		Snippet: "This file differs from the built-in version; delete it and run `sdd init --agent claude` again to install the current one."})
	server := map[string]any{"command": "sdd", "args": []string{"mcp"}}
	mcpJSON := mustJSON(map[string]any{"mcpServers": map[string]any{"sdd": server}})
	out = append(out, File{
		Path: ".mcp.json", Content: mcpJSON,
		satisfied: hasServer,
		Snippet:   "Add this server under \"mcpServers\":\n  \"sdd\": " + string(mustJSON(server)),
	})

	var deny []string
	for _, c := range DeniedCommands {
		deny = append(deny, "Bash("+c+" *)")
	}
	allow := []string{"mcp__sdd__*"}
	perms := map[string]any{"allow": allow, "deny": deny}
	// The hook catches what the deny rules cannot: the same commands inside
	// wrappers such as bash -c. It runs on the agent's calls, not on `!`.
	hooks := map[string]any{"PreToolUse": []any{hookConfig}}
	out = append(out, File{
		Path: ".claude/settings.json", Content: mustJSON(map[string]any{"permissions": perms, "hooks": hooks}),
		satisfied: func(b []byte) bool {
			var m struct {
				Permissions struct {
					Deny []string `json:"deny"`
				} `json:"permissions"`
			}
			if json.Unmarshal(b, &m) != nil || !hasHookCommand(b) {
				return false
			}
			for _, d := range deny {
				if !slices.Contains(m.Permissions.Deny, d) {
					return false
				}
			}
			return true
		},
		Snippet: "Add these rules under \"permissions\" (the deny rules keep approvals human-only):\n  \"allow\": " +
			string(mustJSON(allow)) + ",\n  \"deny\": " + strings.TrimSpace(string(mustJSON(deny))) +
			"\nand this hook under \"hooks\" (it also blocks those commands inside wrappers such as bash -c):\n  \"PreToolUse\": [" + strings.TrimSpace(string(mustJSON(hookConfig))) + "]",
	})
	return out, nil
}

// hasServer reports whether an mcpServers JSON file registers the sdd server.
func hasServer(b []byte) bool {
	var m struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	return json.Unmarshal(b, &m) == nil && m.MCPServers["sdd"] != nil
}

const codexConfig = `# sdd (Quipu): the MCP server. Codex loads project config only in trusted projects.
[mcp_servers.sdd]
command = "sdd"
args = ["mcp"]
# The sdd tools never approve anything (approvals are not MCP tools).
default_tools_approval_mode = "auto"
`

const codexRules = `# sdd (Quipu): approvals, reviews and merges belong to the user.
# The hook in .codex/hooks.json also blocks them inside wrappers such as bash -c.
prefix_rule(
    pattern = ["sdd", ["approve", "revise", "resume", "clean", "merge"]],
    decision = "forbidden",
    justification = "sdd approvals, reviews and merges belong to the user: ask them to run it",
    match = ["sdd approve G2", "sdd merge demo"],
    not_match = ["sdd review G2", "sdd status"],
)

prefix_rule(
    pattern = ["sdd", ["spec", "plan"], "approve"],
    decision = "forbidden",
    justification = "sdd approvals belong to the user: ask them to run it",
    match = ["sdd spec approve demo --functional", "sdd plan approve demo"],
)
`

func codexFiles(workflow string) ([]File, error) {
	out, err := skills(".agents/skills", "agents")
	if err != nil {
		return nil, err
	}
	out = append(out, agentsMD(workflow),
		File{Path: ".codex/config.toml", Content: []byte(codexConfig),
			satisfied: func(b []byte) bool { return bytes.Contains(b, []byte("[mcp_servers.sdd]")) },
			Snippet:   "Add the sdd MCP server:\n" + strings.TrimSpace(codexConfig)},
		File{Path: ".codex/hooks.json", Content: mustJSON(map[string]any{"hooks": map[string]any{"PreToolUse": []any{hookConfig}}}),
			satisfied: hasHookCommand,
			Snippet:   "Add this hook under \"hooks\" (it blocks human-only sdd commands, also inside wrappers):\n  \"PreToolUse\": [" + strings.TrimSpace(string(mustJSON(hookConfig))) + "]"},
		File{Path: ".codex/rules/sdd.rules", Content: []byte(codexRules),
			Snippet: "This file differs from the built-in version; delete it and run `sdd init --agent codex` again to install the current one."},
	)
	return out, nil
}

func antigravityFiles(workflow string) ([]File, error) {
	out, err := skills(".agents/skills", "agents")
	if err != nil {
		return nil, err
	}
	server := map[string]any{"command": "sdd", "args": []string{"mcp"}}
	// Antigravity's hooks.json is keyed by hook name, with the shell tool
	// named run_command; `sdd hook` answers in its format.
	hook := map[string]any{"PreToolUse": []any{map[string]any{"matcher": "run_command",
		"hooks": []any{map[string]any{"type": "command", "command": HookCommand}}}}}
	out = append(out, agentsMD(workflow),
		File{Path: ".agents/mcp_config.json", Content: mustJSON(map[string]any{"mcpServers": map[string]any{"sdd": server}}),
			satisfied: hasServer,
			Snippet:   "Add this server under \"mcpServers\":\n  \"sdd\": " + string(mustJSON(server))},
		File{Path: ".agents/hooks.json", Content: mustJSON(map[string]any{"sdd-guard": hook}),
			satisfied: hasHookCommand,
			Snippet:   "Add this hook (it blocks human-only sdd commands, also inside wrappers):\n  \"sdd-guard\": " + strings.TrimSpace(string(mustJSON(hook)))},
	)
	return out, nil
}

func mustJSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// Status of a file after Install.
type Status string

const (
	Created Status = "created" // written now
	Present Status = "present" // already there with the integration
	Kept    Status = "kept"    // exists without the integration: left untouched, see Snippet
	Missing Status = "missing" // check only: would be created
)

// Result reports what happened to one file.
type Result struct {
	File   File
	Status Status
}

// Install creates the missing files under root. An existing file is never
// modified. With check, nothing is written and missing files are reported.
func Install(root string, list []File, check bool) ([]Result, error) {
	var out []Result
	for _, f := range list {
		path := filepath.Join(root, filepath.FromSlash(f.Path))
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			ok := bytes.Equal(b, f.Content) || (f.satisfied != nil && f.satisfied(b))
			if ok {
				out = append(out, Result{f, Present})
			} else {
				out = append(out, Result{f, Kept})
			}
			continue
		case !errors.Is(err, os.ErrNotExist):
			return out, err
		}
		if check {
			out = append(out, Result{f, Missing})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return out, err
		}
		if err := writeNew(path, f.Content); err != nil {
			return out, err
		}
		out = append(out, Result{f, Created})
	}
	return out, nil
}

// writeNew creates path; it fails rather than overwrite a file created meanwhile.
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
