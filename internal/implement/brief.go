package implement

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/workflow"
)

// GroupReviewFile is where the reviewer writes its report on a group, relative
// to the feature's state directory (%s is the group ID).
const GroupReviewFile = "reviews/%s.md"

// guardrailFiles are the project instruction files a reviewer reads, when present.
var guardrailFiles = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md", ".github/copilot-instructions.md"}

// ReviewBrief is the brief for an expert review of the session's group (the
// one awaiting review, or the one in progress): where its changes are, the
// spec text its tasks cover, the language profile and guardrails to apply, and
// the format of the report. The reviewer reads the code itself.
func ReviewBrief(ctx context.Context, s *Session) (string, error) {
	r, err := ReviewGroup(ctx, s, false)
	if err != nil {
		return "", err
	}
	f, g := s.F, r.Group
	root := f.Layout.Root
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Group review: %s %s (%s)\n\n", f.Name, g.ID, g.Title)
	fmt.Fprintf(&sb, "Status: %s.", g.Status)
	if g.Layer != "" {
		fmt.Fprintf(&sb, " Layer: %s.", g.Layer)
	}
	if g.Goal != "" {
		fmt.Fprintf(&sb, " Goal: %s", g.Goal)
	}
	sb.WriteString("\n\n## Changes\n\n")
	fmt.Fprintf(&sb, "Code: %s. Read the diff with `git -C %s diff %s` and the changed files in full.\n\n%s\n", r.Worktree, r.Worktree, r.Base, r.DiffStat)

	sb.WriteString("\n## Tasks\n")
	for _, t := range g.Tasks {
		fmt.Fprintf(&sb, "\n### %s %s\n", t.ID, t.Title)
		b, err := s.brief(t.ID, false)
		if err != nil {
			return "", err
		}
		if b.Description != "" {
			fmt.Fprintf(&sb, "%s\n", b.Description)
		}
		if b.DoneWhen != "" {
			fmt.Fprintf(&sb, "- done when: %s\n", b.DoneWhen)
		}
		if len(t.Files) > 0 {
			fmt.Fprintf(&sb, "- declared files: %s\n", strings.Join(t.Files, ", "))
		}
		if len(t.Undeclared) > 0 {
			fmt.Fprintf(&sb, "- undeclared files changed: %s\n", strings.Join(t.Undeclared, ", "))
		}
		for _, c := range b.Covers {
			fmt.Fprintf(&sb, "- covers %s", c.ID)
			if c.Parent != "" {
				fmt.Fprintf(&sb, " (%s)", c.Parent)
			}
			if c.Text != "" {
				fmt.Fprintf(&sb, ": %s", c.Text)
			}
			if len(c.Technical) > 0 {
				fmt.Fprintf(&sb, " [%s]", strings.Join(c.Technical, ", "))
			}
			sb.WriteString("\n")
		}
		if t.Summary != "" {
			fmt.Fprintf(&sb, "- implementer's summary: %s\n", t.Summary)
		}
	}

	sb.WriteString("\n## Standards\n\n")
	if f.Technical != nil && f.Technical.Language != "" {
		lang := f.Technical.Language
		fmt.Fprintf(&sb, "- Language: %s. Review as a senior %s engineer; apply the profile .sdd/profiles/%s.md.\n", lang, lang, lang)
	}
	fmt.Fprintf(&sb, "- Technical spec (architecture, constraints, ADRs): specs/%s/technical.md.\n", f.Name)
	var found []string
	for _, name := range guardrailFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err == nil {
			found = append(found, name)
		}
	}
	if len(found) > 0 {
		fmt.Fprintf(&sb, "- Project guardrails: %s (and any nested ones next to the changed files).\n", strings.Join(found, ", "))
	}
	if g.Verification != "" {
		fmt.Fprintf(&sb, "- sdd already ran the verification commands: %s\n", verdicts(g.Verification))
	}

	report := filepath.ToSlash(filepath.Join(".sdd", "features", f.Name, fmt.Sprintf(GroupReviewFile, g.ID)))
	next := "`sdd approve " + g.ID + "` or `sdd revise " + g.ID + " -m \"<what to change>\"`"
	if g.Status != string(workflow.GroupAwaitingReview) {
		next = "nothing to approve: " + r.Next
	}
	fmt.Fprintf(&sb, `
## Output

Look for bugs, unmet criteria, tests that do not really verify their criterion, violations of the profile, the guardrails or the architecture, security problems and needless complexity. Do not report style a formatter or linter already enforces.

Write %s with this structure, then return a short summary:

`+"```markdown"+`
# Review: %s %s

Recommendation: approve | revise

| Severity | Location | Finding | Suggested change |
|---|---|---|---|
| blocker / major / minor | file:line | ... | ... |

## Criteria
- <ID>: met | partial | not met (evidence)
`+"```"+`

Do not change the code, the specs or the plan. The decision is the user's: %s.
`, report, f.Name, g.ID, next)
	return sb.String(), nil
}

// verdicts condenses a verification summary to "name: status" pairs.
func verdicts(summary string) string {
	var out []string
	for _, line := range strings.Split(summary, "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		if i := strings.Index(line, "  ["); i >= 0 {
			line = line[:i]
		}
		out = append(out, strings.Join(strings.Fields(line), " "))
	}
	return strings.Join(out, "; ")
}
