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
var Names = []string{"claude", "generic"}

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
// workflow guide, embedded in generic instructions.
func Files(agent, workflow string) ([]File, error) {
	switch agent {
	case "claude":
		return claudeFiles()
	case "generic":
		b, _ := files.ReadFile("files/generic/AGENTS.md")
		content := strings.Replace(string(b), "{{workflow}}", strings.TrimPrefix(strings.TrimSpace(workflow), "# Working with sdd\n\n"), 1)
		return []File{{Path: "AGENTS.md", Content: []byte(content),
			Snippet: "Add a section that tells agents to run `sdd guide workflow` and follow it."}}, nil
	}
	return nil, fmt.Errorf("unknown agent %q (supported: %s)", agent, strings.Join(Names, ", "))
}

func claudeFiles() ([]File, error) {
	var out []File
	err := fs.WalkDir(files, "files/claude/skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		rel := ".claude/" + strings.TrimPrefix(p, "files/claude/")
		out = append(out, File{Path: rel, Content: b,
			Snippet: "This file differs from the built-in version; delete it and run `sdd init --agent claude` again to install the current one."})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	server := map[string]any{"command": "sdd", "args": []string{"mcp"}}
	mcpJSON := mustJSON(map[string]any{"mcpServers": map[string]any{"sdd": server}})
	out = append(out, File{
		Path: ".mcp.json", Content: mcpJSON,
		satisfied: func(b []byte) bool {
			var m struct {
				MCPServers map[string]json.RawMessage `json:"mcpServers"`
			}
			return json.Unmarshal(b, &m) == nil && m.MCPServers["sdd"] != nil
		},
		Snippet: "Add this server under \"mcpServers\":\n  \"sdd\": " + string(mustJSON(server)),
	})

	var deny []string
	for _, c := range DeniedCommands {
		deny = append(deny, "Bash("+c+" *)")
	}
	allow := []string{"mcp__sdd__*"}
	perms := map[string]any{"allow": allow, "deny": deny}
	out = append(out, File{
		Path: ".claude/settings.json", Content: mustJSON(map[string]any{"permissions": perms}),
		satisfied: func(b []byte) bool {
			var m struct {
				Permissions struct {
					Deny []string `json:"deny"`
				} `json:"permissions"`
			}
			if json.Unmarshal(b, &m) != nil {
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
			string(mustJSON(allow)) + ",\n  \"deny\": " + string(mustJSON(deny)),
	})
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
