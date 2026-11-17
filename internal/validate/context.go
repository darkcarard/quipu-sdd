package validate

import (
	"context"
	"fmt"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/spec"
	"github.com/darkcarard/quipu-sdd/internal/trace"
)

// DeepReviewFile is where the agent writes the deep review, relative to the feature's state directory.
const DeepReviewFile = "deep-review.md"

// ReviewContext is the brief for an agent-assisted semantic review: every
// active acceptance criterion with the tasks, commits and tests linked to it,
// where the code is, and the format of the review to write. The agent reads
// the code itself.
func ReviewContext(ctx context.Context, f *feature.Feature) (string, error) {
	if !started(f) {
		return "", fmt.Errorf("%s has no implementation to review yet (phase %s)", f.Name, f.Phase)
	}
	ix, _, err := Index(ctx, f)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Deep review: %s\n\n", f.Name)
	fmt.Fprintf(&sb, "Code: %s (branch sdd/%s). Specs: specs/%s/.\n\n", codeDir(f), f.Name, f.Name)
	sb.WriteString("For each acceptance criterion below, read the code and the tests linked to it and decide whether the implementation actually satisfies it.\n\n")
	sb.WriteString("## Criteria\n\n")
	for _, id := range f.Functional.Active(spec.KindAC) {
		e, ok := ix.Lookup(id)
		if !ok || e.Kind != trace.KindCriterion {
			continue
		}
		fmt.Fprintf(&sb, "### %s (%s)\n%s\n", e.ID, e.Parent, e.Title)
		if len(e.Technical) > 0 {
			fmt.Fprintf(&sb, "- test level: %s\n", strings.Join(e.Technical, ", "))
		}
		fmt.Fprintf(&sb, "- tasks: %s\n", list(e.Tasks))
		fmt.Fprintf(&sb, "- commits: %s\n", list(e.Commits))
		fmt.Fprintf(&sb, "- tests: %s\n\n", list(e.Tests))
	}
	fmt.Fprintf(&sb, `## Output

Write .sdd/features/%s/%s with this structure, then summarize it for the user:

`+"```markdown"+`
# Deep review: %s

| Criterion | Verdict | Evidence | Notes |
|---|---|---|---|
| AC-001 | satisfied | internal/x/y.go:42, internal/x/y_test.go:10 | |

## Findings
- <anything that does not satisfy a criterion, or is not covered by a meaningful test>
`+"```"+`

Verdicts: satisfied, partial, not satisfied, not verifiable. The review is informational: it never blocks a gate. Do not change code or specs while reviewing.
`, f.Name, DeepReviewFile, f.Name)
	return sb.String(), nil
}

func list(xs []string) string {
	if len(xs) == 0 {
		return "(none)"
	}
	return strings.Join(xs, ", ")
}
