package adopt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkcarard/sdd-framework/internal/workspace"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var project = map[string]string{
	".gitignore":                    "node_modules/\n",
	"go.mod":                        "module example.com/shop\n",
	"Makefile":                      "build:\n\tgo build ./...\ntest: build\n\tgo test ./...\nVAR := x\n",
	"cmd/shop/main.go":              "package main\n",
	"internal/cart/cart.go":         "package cart\n",
	"internal/cart/cart_test.go":    "package cart\n",
	"web/package.json":              `{"scripts":{"build":"vite build","test":"vitest"}}`,
	"web/src/app.ts":                "export {}\n",
	"web/src/app.test.ts":           "test()\n",
	"web/node_modules/x/index.js":   "ignored\n",
	"README.md":                     "# Shop\n",
	"docs/architecture.md":          "# Arch\n",
	"docs/adr/0001-use-postgres.md": "# ADR\n",
	"specs/old/functional.md":       "sdd's own\n",
}

func TestScanGitRepository(t *testing.T) {
	root := t.TempDir()
	write(t, root, project)
	if _, err := (workspace.Git{Dir: root}).Run(context.Background(), "init", "-q"); err != nil {
		t.Fatal(err)
	}
	inv, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Files != 12 { // node_modules is ignored, specs/ is sdd's
		t.Errorf("files = %d", inv.Files)
	}
	if inv.Languages[0] != (Count{"Go", 3}) || inv.Languages[1] != (Count{"TypeScript", 2}) {
		t.Errorf("languages = %v", inv.Languages)
	}
	if strings.Join(inv.Build, ",") != "Makefile,go.mod,web/package.json" {
		t.Errorf("build = %v", inv.Build)
	}
	if inv.Tests.Files != 2 {
		t.Errorf("tests = %+v", inv.Tests)
	}
	out := Format(inv)
	for _, want := range []string{
		"From Makefile: build `make build`, lint `?`, test `make test`",
		"From go.mod: build `go build ./...`, lint `go vet ./...`, test `go test ./...`",
		"- internal/ (2 files)", "README.md, docs/adr/0001-use-postgres.md, docs/architecture.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("inventory missing %q:\n%s", want, out)
		}
	}
	if len(out) > 2000 {
		t.Errorf("the inventory must stay compact (%d chars)", len(out))
	}
}

func TestSuggestCommands(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"package.json":   `{"scripts":{"build":"tsc","lint":"eslint .","test":"vitest","test:e2e":"playwright test"}}`,
		"pnpm-lock.yaml": "",
		"pyproject.toml": "[tool.ruff]\n",
		"Cargo.toml":     "[package]\n",
	})
	inv, err := Scan(context.Background(), root) // not a git repository: walks the tree
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Commands{}
	for _, c := range inv.Commands {
		got[c.Source] = c
	}
	if c := got["package.json"]; c.Build != "pnpm run build" || c.Lint != "pnpm run lint" || c.Test != "pnpm test" || c.Integration != "pnpm run test:e2e" {
		t.Errorf("node = %+v", c)
	}
	if c := got["pyproject.toml"]; c.Lint != "ruff check ." || c.Test != "pytest" {
		t.Errorf("python = %+v", c)
	}
	if c := got["Cargo.toml"]; c.Test != "cargo test" {
		t.Errorf("rust = %+v", c)
	}
}
