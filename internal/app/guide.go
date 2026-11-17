package app

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/templates"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

//go:embed guides/*.md
var guides embed.FS

// GuideTopics are the topics `sdd guide` serves.
var GuideTopics = []string{"workflow", "functional", "technical", "plan", "implement", "adopt"}

// upcoming are guide topics that arrive with a later milestone.
var upcoming = map[string]string{}

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
	case "adopt":
		b, err := guides.ReadFile("guides/adopt.md")
		return string(b), err
	case "implement":
		b, err := guides.ReadFile("guides/implement.md")
		if err != nil || arg == "" {
			return string(b), err
		}
		d, err := a.Status(arg)
		if err != nil {
			return "", err
		}
		return string(b) + "\n## This feature\n\n" + FormatDetail(d), nil
	case "functional", "technical", "plan":
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

	// The template is only useful before the file exists; afterwards the agent
	// reads the file itself, so it is left out to save tokens.
	exists := false
	if arg != "" {
		if f, err := a.Load(arg); err == nil {
			exists = f.Files[workflow.Artifact(topic)].Exists
		}
	}
	if exists {
		fmt.Fprintf(&sb, "\n## Template\n\nThe file already exists: read it instead of the template (`.sdd/templates/%s.md`).\n", topic)
	} else {
		tpl, err := a.template(topic + ".md")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "\n## Template\n\nNew files start from this template (`.sdd/templates/%s.md`); `{{feature}}` and `{{title}}` are filled in on creation.\n\n````markdown\n%s````\n", topic, tpl)
	}

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
		if topic == "plan" {
			checklist, err := a.coverageChecklist(d.Name)
			if err != nil {
				return "", err
			}
			sb.WriteString(checklist)
		}
	}
	return sb.String(), nil
}

// coverageChecklist lists what the plan must cover, with what the technical
// spec assigned to each ID, and the limits that apply.
func (a *App) coverageChecklist(name string) (string, error) {
	f, err := feature.Load(a.Layout, a.Config, name)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("\n## Coverage checklist\n\n")
	if f.Functional == nil {
		sb.WriteString("functional.md does not exist yet.\n")
		return sb.String(), nil
	}
	tech := func(id string, levels bool) string {
		if f.Technical == nil {
			return ""
		}
		xs := f.Technical.Components(id)
		label := "components"
		if levels {
			xs, label = f.Technical.TestLevels(id), "test level"
		}
		if len(xs) == 0 {
			return ""
		}
		return fmt.Sprintf(" — %s: %s", label, strings.Join(xs, ", "))
	}
	sb.WriteString("Every FR and AC below must be covered by at least one task:\n\n")
	for _, r := range f.Functional.Requirements {
		if r.Retired {
			continue
		}
		fmt.Fprintf(&sb, "- **%s** %s%s\n", r.ID, r.Title, tech(r.ID, false))
		for _, c := range r.Criteria {
			if !c.Retired {
				fmt.Fprintf(&sb, "  - **%s** %s%s\n", c.ID, c.Text, tech(c.ID, true))
			}
		}
	}
	var nfrs []string
	for _, r := range f.Functional.NFRs {
		if !r.Retired {
			text := r.Text
			if text == "" {
				text = r.Title
			}
			nfrs = append(nfrs, fmt.Sprintf("- **%s** %s%s", r.ID, text, tech(r.ID, false)))
		}
	}
	if len(nfrs) > 0 {
		sb.WriteString("\nNon-functional requirements (cover them where a task addresses them):\n\n" + strings.Join(nfrs, "\n") + "\n")
	}
	sb.WriteString("\nLimits and context:\n\n")
	fmt.Fprintf(&sb, "- At most %d files per task (`limits.max_files_per_task`).\n", a.Config.Limits.MaxFilesPerTask)
	if f.Technical != nil {
		if arch := strings.TrimSpace(f.Technical.Architecture); arch != "" {
			fmt.Fprintf(&sb, "- Architecture (name the layers from here):\n\n%s\n\n", indent(arch, "  > "))
		}
		if f.Technical.Language != "" {
			fmt.Fprintf(&sb, "- Language: %s (profile `.sdd/profiles/%s.md`).\n", f.Technical.Language, f.Technical.Language)
		}
	}
	return sb.String(), nil
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
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
