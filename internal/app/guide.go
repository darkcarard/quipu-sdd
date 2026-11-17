package app

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/templates"
)

//go:embed guides/*.md
var guides embed.FS

// GuideTopics are the topics `sdd guide` serves.
var GuideTopics = []string{"workflow", "functional", "technical"}

// upcoming are guide topics that arrive with a later milestone.
var upcoming = map[string]string{"plan": "M4", "implement": "M5"}

// WorkflowGuide is the overview of how an agent works with sdd.
func WorkflowGuide() string {
	b, _ := guides.ReadFile("guides/workflow.md")
	return string(b)
}

// Guide returns the instructions for drafting an artifact (topic), followed by
// the project's template and, when arg names a feature, its current status.
func (a *App) Guide(topic, arg string) (string, error) {
	switch topic {
	case "workflow":
		return WorkflowGuide(), nil
	case "functional", "technical":
	default:
		if m, ok := upcoming[topic]; ok {
			return "", fmt.Errorf("the %s guide is not available yet (planned for %s, see docs/ROADMAP.md)", topic, m)
		}
		return "", fmt.Errorf("unknown guide topic %q (use %s)", topic, strings.Join(GuideTopics, ", "))
	}
	b, err := guides.ReadFile("guides/" + topic + ".md")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Write(b)

	tpl, err := a.template(topic + ".md")
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&sb, "\n## Template\n\nNew files start from this template (`.sdd/templates/%s.md`); `{{feature}}` and `{{title}}` are filled in on creation.\n\n````markdown\n%s````\n", topic, tpl)

	if arg != "" {
		d, err := a.Status(arg)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "\n## This feature\n\n- Name: `%s`\n- Title: %s\n", d.Name, d.Title)
		if d.Description != "" {
			fmt.Fprintf(&sb, "- Description: %s\n", d.Description)
		}
		fmt.Fprintf(&sb, "- Phase: %s\n", d.Phase)
		for _, art := range d.Artifacts {
			line := fmt.Sprintf("- `specs/%s/%s`: %s", d.Name, art.File, art.Status)
			if art.Linted {
				line += fmt.Sprintf(" (lint: %d errors, %d warnings)", art.Errors, art.Warnings)
			}
			sb.WriteString(line + "\n")
		}
		fmt.Fprintf(&sb, "- Next: %s\n", d.Next)
	}
	return sb.String(), nil
}

// template returns the project's copy of a template, or the built-in one.
func (a *App) template(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(a.Layout.TemplatesDir(), name))
	if err == nil {
		return string(b), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return templates.Get(name)
}
