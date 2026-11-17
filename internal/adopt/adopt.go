// Package adopt inventories an existing (brownfield) project so a coding agent
// can document it as baseline features: languages, build files and the
// build/lint/test commands they suggest, the top-level layout, tests and docs.
// The inventory is deterministic and compact, since the agent reads it.
package adopt

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/workspace"
)

// Inventory describes an existing project.
type Inventory struct {
	Files     int
	Languages []Count    // by number of files, largest first
	Build     []string   // build and project files found (relative paths)
	Commands  []Commands // suggested verification commands, one set per ecosystem
	Layout    []Count    // top-level entries with their file counts
	Tests     Tests
	Docs      []string
}

// Count is a name with a number of files.
type Count struct {
	Name  string
	Files int
}

// Commands are suggested verification commands for one ecosystem.
type Commands struct {
	Source                               string // the file they come from
	Build, Lint, Test, Integration, Note string
}

// Tests summarizes the test files found.
type Tests struct {
	Files int
	Dirs  []Count // directories holding test files, most first
}

var languages = map[string]string{
	".go": "Go", ".py": "Python", ".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript", ".jsx": "JavaScript",
	".mjs": "JavaScript", ".cjs": "JavaScript", ".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin", ".rb": "Ruby",
	".rs": "Rust", ".cs": "C#", ".php": "PHP", ".swift": "Swift", ".c": "C", ".h": "C", ".cpp": "C++", ".cc": "C++",
	".hpp": "C++", ".scala": "Scala", ".sql": "SQL", ".sh": "Shell", ".vue": "Vue", ".svelte": "Svelte",
	".dart": "Dart", ".ex": "Elixir", ".exs": "Elixir", ".lua": "Lua", ".r": "R", ".m": "Objective-C",
}

// buildFiles are recognized project and build files (base names).
var buildFiles = map[string]bool{
	"go.mod": true, "package.json": true, "pyproject.toml": true, "requirements.txt": true, "setup.py": true,
	"Pipfile": true, "Cargo.toml": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"Makefile": true, "CMakeLists.txt": true, "Gemfile": true, "composer.json": true, "Dockerfile": true,
	"docker-compose.yml": true, "compose.yaml": true, "mix.exs": true, "pubspec.yaml": true, "Taskfile.yml": true,
	"justfile": true, "tox.ini": true, "noxfile.py": true,
}

var testFileRe = regexp.MustCompile(`(?i)(_test\.go|^test_.*\.py|_test\.py|\.(test|spec)\.[jt]sx?|Tests?\.(java|kt|cs)|_spec\.rb|_test\.rb|_test\.exs)$`)

// testDirs are directory names that hold tests.
var testDirs = map[string]bool{"test": true, "tests": true, "__tests__": true, "spec": true, "e2e": true, "integration": true}

// Scan inventories the project at root. It uses the files git sees (tracked
// and untracked, not ignored) when root is a git repository, and walks the
// tree otherwise.
func Scan(ctx context.Context, root string) (Inventory, error) {
	files, err := listFiles(ctx, root)
	if err != nil {
		return Inventory{}, err
	}
	inv := Inventory{}
	langs := map[string]int{}
	layout := map[string]int{}
	testDirCount := map[string]int{}
	for _, f := range files {
		if strings.HasPrefix(f, "specs/") || strings.HasPrefix(f, ".sdd/") || f == "sdd.yaml" {
			continue // sdd's own files
		}
		inv.Files++
		if l, ok := languages[strings.ToLower(path.Ext(f))]; ok {
			langs[l]++
		}
		top, _, nested := strings.Cut(f, "/")
		if nested {
			layout[top+"/"]++
		} else {
			layout[top]++
		}
		base := path.Base(f)
		if buildFiles[base] && strings.Count(f, "/") <= 1 {
			inv.Build = append(inv.Build, f)
		}
		if isDoc(f) {
			inv.Docs = append(inv.Docs, f)
		}
		if isTest(f) {
			inv.Tests.Files++
			testDirCount[path.Dir(f)]++
		}
	}
	inv.Languages = top(langs, 8)
	inv.Layout = top(layout, 25)
	sort.Slice(inv.Layout, func(i, j int) bool { return inv.Layout[i].Name < inv.Layout[j].Name })
	inv.Tests.Dirs = top(testDirCount, 10)
	sort.Strings(inv.Build)
	if len(inv.Docs) > 20 {
		inv.Docs = append(inv.Docs[:20], fmt.Sprintf("… and %d more", len(inv.Docs)-20))
	}
	inv.Commands = suggest(root, inv.Build)
	return inv, nil
}

func isDoc(f string) bool {
	base := strings.ToUpper(path.Base(f))
	switch {
	case strings.Count(f, "/") == 0 && (strings.HasPrefix(base, "README") || strings.HasPrefix(base, "CHANGELOG") ||
		strings.HasPrefix(base, "CONTRIBUTING") || base == "AGENTS.MD" || base == "CLAUDE.MD" || base == "GEMINI.MD"):
		return true
	case strings.HasPrefix(f, "docs/") && strings.Count(f, "/") <= 2 && strings.HasSuffix(strings.ToLower(f), ".md"):
		return true
	case strings.Contains(strings.ToLower(f), "/adr/") || strings.Contains(strings.ToLower(f), "/decisions/"):
		return strings.HasSuffix(strings.ToLower(f), ".md")
	}
	return false
}

func isTest(f string) bool {
	if testFileRe.MatchString(path.Base(f)) {
		return true
	}
	for _, part := range strings.Split(path.Dir(f), "/") {
		if testDirs[part] && languages[strings.ToLower(path.Ext(f))] != "" {
			return true
		}
	}
	return false
}

func top(m map[string]int, n int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// listFiles returns the project's files, relative and slash-separated.
func listFiles(ctx context.Context, root string) ([]string, error) {
	g := workspace.Git{Dir: root}
	if _, err := g.Run(ctx, "rev-parse", "--git-dir"); err == nil {
		out, err := g.Run(ctx, "ls-files", "--cached", "--others", "--exclude-standard")
		if err != nil {
			return nil, err
		}
		var files []string
		for _, f := range strings.Split(out, "\n") {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		return files, nil
	}
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, ".venv": true, "venv": true}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// suggest derives verification commands from the build files at the root.
func suggest(root string, build []string) []Commands {
	has := map[string]bool{}
	for _, b := range build {
		has[b] = true
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name))
		return string(b)
	}
	var out []Commands
	if has["Makefile"] {
		targets := makeTargets(read("Makefile"))
		c := Commands{Source: "Makefile"}
		for _, t := range []struct {
			field *string
			names []string
		}{{&c.Build, []string{"build"}}, {&c.Lint, []string{"lint", "vet", "check"}}, {&c.Test, []string{"test"}},
			{&c.Integration, []string{"integration", "test-integration", "e2e"}}} {
			for _, n := range t.names {
				if targets[n] {
					*t.field = "make " + n
					break
				}
			}
		}
		if c.Build+c.Lint+c.Test != "" {
			c.Note = "Makefile targets usually wrap the toolchain: prefer them if the team uses them"
			out = append(out, c)
		}
	}
	if has["go.mod"] {
		out = append(out, Commands{Source: "go.mod", Build: "go build ./...", Lint: "go vet ./...", Test: "go test ./...",
			Integration: "go test -tags=integration ./...", Note: "the integration tag is a convention: check how integration tests are selected"})
	}
	if has["package.json"] {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		_ = json.Unmarshal([]byte(read("package.json")), &pkg)
		pm := "npm"
		switch {
		case fileExists(root, "pnpm-lock.yaml"):
			pm = "pnpm"
		case fileExists(root, "yarn.lock"):
			pm = "yarn"
		case fileExists(root, "bun.lockb"), fileExists(root, "bun.lock"):
			pm = "bun"
		}
		c := Commands{Source: "package.json"}
		run := func(script string) string {
			if _, ok := pkg.Scripts[script]; ok {
				return pm + " run " + script
			}
			return ""
		}
		c.Build, c.Lint = run("build"), run("lint")
		if c.Lint == "" {
			c.Lint = run("typecheck")
		}
		if _, ok := pkg.Scripts["test"]; ok {
			c.Test = pm + " test"
		}
		for _, s := range []string{"test:integration", "test:e2e", "e2e", "integration"} {
			if c.Integration = run(s); c.Integration != "" {
				break
			}
		}
		c.Note = fmt.Sprintf("from the package.json scripts (%s)", pm)
		out = append(out, c)
	}
	if has["Cargo.toml"] {
		out = append(out, Commands{Source: "Cargo.toml", Build: "cargo build", Lint: "cargo clippy -- -D warnings", Test: "cargo test"})
	}
	if has["pyproject.toml"] || has["requirements.txt"] || has["setup.py"] {
		src := "pyproject.toml"
		if !has[src] {
			src = "requirements.txt"
			if !has[src] {
				src = "setup.py"
			}
		}
		py := read("pyproject.toml") + read("requirements.txt")
		c := Commands{Source: src, Build: "python -m compileall -q .", Test: "pytest"}
		switch {
		case strings.Contains(py, "ruff"):
			c.Lint = "ruff check ."
		case strings.Contains(py, "flake8"):
			c.Lint = "flake8"
		case strings.Contains(py, "mypy"):
			c.Lint = "mypy ."
		}
		c.Note = "check how the project runs its tests (pytest, tox, nox…)"
		out = append(out, c)
	}
	if has["pom.xml"] {
		out = append(out, Commands{Source: "pom.xml", Build: "mvn -q -DskipTests package", Test: "mvn -q test", Integration: "mvn -q verify"})
	}
	if has["build.gradle"] || has["build.gradle.kts"] {
		gradle := "gradle"
		if fileExists(root, "gradlew") {
			gradle = "./gradlew"
		}
		out = append(out, Commands{Source: "build.gradle", Build: gradle + " assemble", Lint: gradle + " check -x test", Test: gradle + " test"})
	}
	return out
}

var targetRe = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)\s*:([^=]|$)`)

func makeTargets(makefile string) map[string]bool {
	out := map[string]bool{}
	for _, m := range targetRe.FindAllStringSubmatch(makefile, -1) {
		out[m[1]] = true
	}
	return out
}

func fileExists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

// Format renders the inventory for the agent.
func Format(inv Inventory) string {
	var sb strings.Builder
	sb.WriteString("# Project inventory\n\n")
	fmt.Fprintf(&sb, "%d files.", inv.Files)
	if len(inv.Languages) > 0 {
		var ls []string
		for _, l := range inv.Languages {
			ls = append(ls, fmt.Sprintf("%s %d", l.Name, l.Files))
		}
		fmt.Fprintf(&sb, " Languages (files): %s.", strings.Join(ls, ", "))
	}
	sb.WriteString("\n\n## Layout\n\n")
	for _, e := range inv.Layout {
		if strings.HasSuffix(e.Name, "/") {
			fmt.Fprintf(&sb, "- %s (%d files)\n", e.Name, e.Files)
		} else {
			fmt.Fprintf(&sb, "- %s\n", e.Name)
		}
	}
	sb.WriteString("\n## Build files\n\n")
	if len(inv.Build) == 0 {
		sb.WriteString("None recognized: ask the user how the project is built and tested.\n")
	} else {
		sb.WriteString(strings.Join(inv.Build, ", ") + "\n")
	}
	if len(inv.Commands) > 0 {
		sb.WriteString("\n## Suggested commands (confirm with the user before using them)\n\n")
		for _, c := range inv.Commands {
			fmt.Fprintf(&sb, "- From %s: build `%s`, lint `%s`, test `%s`, integration `%s`", c.Source, or(c.Build), or(c.Lint), or(c.Test), or(c.Integration))
			if c.Note != "" {
				fmt.Fprintf(&sb, " (%s)", c.Note)
			}
			sb.WriteString("\n")
		}
	}
	fmt.Fprintf(&sb, "\n## Tests\n\n%d test files", inv.Tests.Files)
	if len(inv.Tests.Dirs) > 0 {
		var ds []string
		for _, d := range inv.Tests.Dirs {
			ds = append(ds, fmt.Sprintf("%s (%d)", d.Name, d.Files))
		}
		fmt.Fprintf(&sb, " in %s", strings.Join(ds, ", "))
	}
	sb.WriteString(".\n")
	if len(inv.Docs) > 0 {
		sb.WriteString("\n## Documentation\n\n" + strings.Join(inv.Docs, ", ") + "\n")
	}
	return sb.String()
}

func or(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
