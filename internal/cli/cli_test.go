package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/state"
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
		l.ConfigFile(), l.GitignoreFile(),
		filepath.Join(l.TemplatesDir(), "functional.md"),
		filepath.Join(l.TemplatesDir(), "technical.md"),
		filepath.Join(l.TemplatesDir(), "plan.md"),
		filepath.Join(l.ProfilesDir(), "go.md"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing file %s", f)
		}
	}
	if _, err := config.Load(dir); err != nil {
		t.Fatalf("generated sdd.yaml does not load: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "sdd.yaml")); strings.Contains(string(b), "provider") || strings.Contains(string(b), "API") {
		t.Errorf("sdd.yaml must not configure an LLM provider:\n%s", b)
	}
	if gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(gi) != "node_modules/" {
		t.Errorf("the project's .gitignore must not be modified:\n%s", gi)
	}
	if gi, _ := os.ReadFile(l.GitignoreFile()); !strings.Contains(string(gi), "worktrees/") || !strings.Contains(string(gi), "locks/") {
		t.Errorf(".sdd/.gitignore = %q", gi)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err == nil {
		t.Error("agent files are only installed with --agent")
	}
	if !strings.Contains(out, "sdd init --agent claude") {
		t.Errorf("should suggest next step, got:\n%s", out)
	}
}

func TestInitIsIdempotentAndKeepsCustomizations(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run(t, dir, "init"); code != ExitOK {
		t.Fatal(e)
	}
	l := state.Layout{Root: dir}
	os.WriteFile(l.ConfigFile(), []byte("limits:\n  max_files_per_task: 7\n"), 0o644)
	tpl := filepath.Join(l.TemplatesDir(), "functional.md")
	os.WriteFile(tpl, []byte("custom template"), 0o644)

	code, out, e := run(t, dir, "init")
	if code != ExitOK {
		t.Fatal(e)
	}
	if !strings.Contains(e, "not a git repository") {
		t.Error("should warn outside a git repo")
	}
	if b, _ := os.ReadFile(l.ConfigFile()); !strings.Contains(string(b), "max_files_per_task: 7") {
		t.Error("sdd.yaml was overwritten")
	}
	if b, _ := os.ReadFile(tpl); string(b) != "custom template" {
		t.Error("template customization was overwritten")
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

func TestInitAgentClaude(t *testing.T) {
	dir := t.TempDir()
	code, out, e := run(t, dir, "init", "--agent", "claude")
	if code != ExitOK {
		t.Fatal(e)
	}
	for _, f := range []string{".mcp.json", ".claude/settings.json", ".claude/skills/sdd-spec/SKILL.md", ".claude/skills/sdd-tech/SKILL.md", ".claude/skills/sdd-plan/SKILL.md", ".claude/skills/sdd-implement/SKILL.md", ".claude/skills/sdd-deep-review/SKILL.md", ".claude/skills/sdd-adopt/SKILL.md"} {
		if !strings.Contains(out, "created  "+f) {
			t.Errorf("%s not created:\n%s", f, out)
		}
	}
	var mcpCfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err := json.Unmarshal(b, &mcpCfg); err != nil || mcpCfg.MCPServers["sdd"].Command != "sdd" || strings.Join(mcpCfg.MCPServers["sdd"].Args, " ") != "mcp" {
		t.Errorf(".mcp.json = %s (%v)", b, err)
	}
	var settings struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"Bash(sdd spec approve *)", "Bash(sdd plan approve *)", "Bash(sdd approve *)", "Bash(sdd revise *)", "Bash(sdd resume *)", "Bash(sdd clean *)", "Bash(sdd merge *)"} {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			t.Errorf("deny rules miss %s: %v", rule, settings.Permissions.Deny)
		}
	}
	if !slices.Contains(settings.Permissions.Allow, "mcp__sdd__*") {
		t.Errorf("allow = %v", settings.Permissions.Allow)
	}
	if !strings.Contains(string(b), `"command": "sdd hook pre-tool-use"`) || !strings.Contains(string(b), `"matcher": "Bash"`) {
		t.Errorf("settings.json has no PreToolUse hook:\n%s", b)
	}
	skill, _ := os.ReadFile(filepath.Join(dir, ".claude", "skills", "sdd-spec", "SKILL.md"))
	if !strings.Contains(string(skill), "description:") || !strings.Contains(string(skill), "$ARGUMENTS") {
		t.Errorf("skill = %s", skill)
	}
	if code, out, _ := run(t, dir, "init", "--agent", "claude", "--check"); code != ExitOK || !strings.Contains(out, "Everything is in place") {
		t.Errorf("check after init: %d\n%s", code, out)
	}
	if code, out, _ := run(t, dir, "init", "--agent", "claude"); code != ExitOK || strings.Contains(out, "created") {
		t.Errorf("second run must create nothing: %d\n%s", code, out)
	}
}

// TestInitNeverModifiesExistingFiles covers projects that already have agent files.
func TestInitNeverModifiesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	existing := map[string]string{
		".mcp.json":             `{"mcpServers":{"other":{"command":"x"}}}`,
		".claude/settings.json": `{"permissions":{"allow":["Bash(make *)"]}}`,
		"AGENTS.md":             "# Our own agent rules\n",
	}
	for p, c := range existing {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644)
	}

	code, out, _ := run(t, dir, "init", "--agent", "claude", "--check")
	if code != ExitError || !strings.Contains(out, "missing  sdd.yaml") || !strings.Contains(out, "kept     .mcp.json") {
		t.Errorf("check: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "sdd.yaml")); err == nil {
		t.Error("--check must not write anything")
	}

	for _, agent := range []string{"claude", "generic"} {
		code, out, e := run(t, dir, "init", "--agent", agent)
		if code != ExitOK {
			t.Fatalf("%s: %s", agent, e)
		}
		if agent == "claude" && (!strings.Contains(out, "kept     .mcp.json (exists; not modified)") || !strings.Contains(out, `"sdd": {`) ||
			!strings.Contains(out, "Bash(sdd spec approve *)") || !strings.Contains(out, "created  .claude/skills/sdd-spec/SKILL.md")) {
			t.Errorf("claude output:\n%s", out)
		}
		if agent == "generic" && !strings.Contains(out, "kept     AGENTS.md") {
			t.Errorf("generic output:\n%s", out)
		}
	}
	for p, c := range existing {
		if b, _ := os.ReadFile(filepath.Join(dir, p)); string(b) != c {
			t.Errorf("%s was modified:\n%s", p, b)
		}
	}

	// A file that already has the integration counts as present.
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, ".mcp.json"), []byte(`{"mcpServers":{"sdd":{"command":"/usr/local/bin/sdd","args":["mcp"]}}}`), 0o644)
	if _, out, _ := run(t, dir2, "init", "--agent", "claude"); !strings.Contains(out, "ok       .mcp.json") {
		t.Errorf("an existing sdd server must be accepted:\n%s", out)
	}
	if _, out, _ := run(t, dir2, "init", "--agent", "generic"); !strings.Contains(out, "created  AGENTS.md") {
		t.Errorf("AGENTS.md:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir2, "AGENTS.md")); !strings.Contains(string(b), "Approvals belong to the user") || strings.Contains(string(b), "{{workflow}}") {
		t.Errorf("AGENTS.md = %s", b)
	}
	if code, _, _ := run(t, dir2, "init", "--agent", "cursor"); code != ExitUsage {
		t.Error("an unknown agent is a usage error")
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
	if code, _, e := run(t, dir, "upgrade"); code != ExitError || !strings.Contains(e, "later") {
		t.Fatalf("unimplemented command should name its milestone: %d %q", code, e)
	}
	if code, _, _ := run(t, dir, "init", "extra"); code != ExitUsage {
		t.Fatalf("init with extra args should be a usage error, got %d", code)
	}
}

func TestHookBlocksHumanOnlyCommands(t *testing.T) {
	hook := func(input string) (int, string) {
		var out, errOut bytes.Buffer
		a := &App{Dir: t.TempDir(), Stdin: strings.NewReader(input), Stdout: &out, Stderr: &errOut}
		return a.Run(context.Background(), []string{"hook", "pre-tool-use"}), out.String()
	}
	code, out := hook(`{"tool_name":"Bash","tool_input":{"command":"bash -c \"sdd approve G2\""}}`)
	if code != ExitOK || !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "`sdd approve`") {
		t.Errorf("wrapped approve: %d %s", code, out)
	}
	if code, out := hook(`{"tool_name":"Bash","tool_input":{"command":"sdd status"}}`); code != ExitOK || out != "" {
		t.Errorf("sdd status must pass: %d %q", code, out)
	}
}
