package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/state"
)

func run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{Dir: dir, Stdout: &out, Stderr: &errb}
	code := app.Run(context.Background(), args)
	return code, out.String(), errb.String()
}

func TestInitCreatesProjectLayout(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/"), 0o644)

	code, out, errOut := run(t, dir, "init")
	if code != ExitOK {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	if strings.Contains(errOut, "not a git repository") {
		t.Error("should not warn inside a git repo")
	}

	l := state.Layout{Root: dir}
	for _, d := range l.Dirs() {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("missing dir %s", d)
		}
	}
	for _, f := range []string{
		l.ConfigFile(),
		filepath.Join(l.TemplatesDir(), "functional.md"),
		filepath.Join(l.TemplatesDir(), "technical.md"),
		filepath.Join(l.TemplatesDir(), "plan.md"),
		filepath.Join(l.ProfilesDir(), "go.md"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing file %s", f)
		}
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("generated sdd.yaml does not load: %v", err)
	}
	if cfg.Provider.Name != "anthropic" {
		t.Errorf("provider = %q", cfg.Provider.Name)
	}

	gi, _ := os.ReadFile(l.GitignoreFile())
	if !strings.HasPrefix(string(gi), "node_modules/\n") {
		t.Errorf("existing .gitignore content should be preserved:\n%s", gi)
	}
	for _, e := range state.GitignoreEntries {
		if !strings.Contains(string(gi), e+"\n") {
			t.Errorf(".gitignore missing %s", e)
		}
	}
	if !strings.Contains(out, "sdd spec new") {
		t.Errorf("should suggest next step, got:\n%s", out)
	}
}

func TestInitIsIdempotentAndKeepsCustomizations(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run(t, dir, "init"); code != ExitOK {
		t.Fatal(e)
	}
	l := state.Layout{Root: dir}
	os.WriteFile(l.ConfigFile(), []byte("provider:\n  name: anthropic\n  model: custom\n"), 0o644)
	tpl := filepath.Join(l.TemplatesDir(), "functional.md")
	os.WriteFile(tpl, []byte("custom template"), 0o644)
	giBefore, _ := os.ReadFile(l.GitignoreFile())

	code, out, e := run(t, dir, "init")
	if code != ExitOK {
		t.Fatal(e)
	}
	if !strings.Contains(e, "not a git repository") {
		t.Error("should warn outside a git repo")
	}
	if b, _ := os.ReadFile(l.ConfigFile()); !strings.Contains(string(b), "model: custom") {
		t.Error("sdd.yaml was overwritten")
	}
	if b, _ := os.ReadFile(tpl); string(b) != "custom template" {
		t.Error("template customization was overwritten")
	}
	if giAfter, _ := os.ReadFile(l.GitignoreFile()); !bytes.Equal(giBefore, giAfter) {
		t.Errorf(".gitignore changed on re-run:\n%s", giAfter)
	}
	if strings.Contains(out, "created") {
		t.Errorf("second init should create nothing:\n%s", out)
	}

	if code, _, e := run(t, dir, "init", "--force"); code != ExitOK {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(tpl); string(b) == "custom template" {
		t.Error("--force should restore built-in templates")
	}
}

func TestVersionHelpAndUnknown(t *testing.T) {
	dir := t.TempDir()
	if code, out, _ := run(t, dir, "version"); code != ExitOK || !strings.HasPrefix(out, "sdd ") {
		t.Fatalf("version: %d %q", code, out)
	}
	if code, out, _ := run(t, dir, "help"); code != ExitOK || !strings.Contains(out, "implement") {
		t.Fatalf("help: %d %q", code, out)
	}
	if code, _, _ := run(t, dir); code != ExitUsage {
		t.Fatalf("no args should be a usage error, got %d", code)
	}
	if code, _, e := run(t, dir, "bogus"); code != ExitUsage || !strings.Contains(e, "unknown command") {
		t.Fatalf("unknown: %d %q", code, e)
	}
	if code, _, e := run(t, dir, "plan", "x"); code != ExitError || !strings.Contains(e, "M4") {
		t.Fatalf("unimplemented command should name its milestone: %d %q", code, e)
	}
	if code, _, _ := run(t, dir, "init", "extra"); code != ExitUsage {
		t.Fatalf("init with extra args should be a usage error, got %d", code)
	}
}
