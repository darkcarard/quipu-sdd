package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesContainRequiredSections(t *testing.T) {
	required := map[string][]string{
		Functional: {"## Summary", "## Problem & Goals", "## Non-Goals", "## Users & Scenarios",
			"## Functional Requirements", "## Non-Functional Requirements", "## Constraints",
			"## Open Questions", "## Glossary", "FR-001", "AC-001"},
		Technical: {"language:", "functional_hash:", "commands:", "## Overview", "## Architecture",
			"## Components", "## Data Model", "## Interfaces / APIs", "## Requirement Traceability",
			"## Error Handling", "## Testing Strategy", "## Decisions", "## Risks"},
		Plan: {"functional_hash:", "technical_hash:", "## G1:", "### T1.1:", "- covers:",
			"- files:", "- done_when:", "- verify:", "- layer:", "- depends_on:"},
	}
	for name, sections := range required {
		t.Run(name, func(t *testing.T) {
			content, err := Get(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range sections {
				if !strings.Contains(content, s) {
					t.Errorf("%s missing %q", name, s)
				}
			}
		})
	}
}

func TestRender(t *testing.T) {
	got := Render("id: {{feature}} / {{title}}", map[string]string{"feature": "signup", "title": "Signup"})
	if got != "id: signup / Signup" {
		t.Fatalf("got %q", got)
	}
}

func TestInstallPreservesCustomizations(t *testing.T) {
	dir := t.TempDir()
	tpl, prof := filepath.Join(dir, "templates"), filepath.Join(dir, "profiles")

	written, err := Install(tpl, prof, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) < 4 {
		t.Fatalf("expected at least 4 files, got %v", written)
	}
	if _, err := os.Stat(filepath.Join(prof, "go.md")); err != nil {
		t.Fatalf("profile not installed: %v", err)
	}

	custom := filepath.Join(tpl, Functional)
	if err := os.WriteFile(custom, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if written, err = Install(tpl, prof, false); err != nil || len(written) != 0 {
		t.Fatalf("second install should write nothing, wrote %v (err %v)", written, err)
	}
	if b, _ := os.ReadFile(custom); string(b) != "custom" {
		t.Fatal("customization overwritten")
	}
	if _, err := Install(tpl, prof, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(custom); string(b) == "custom" {
		t.Fatal("overwrite=true did not replace file")
	}
}
