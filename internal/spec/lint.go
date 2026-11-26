package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/diag"
	"github.com/darkcarard/quipu-sdd/internal/markdown"
)

// LintOptions tune the deterministic checks.
type LintOptions struct {
	// Feature is the expected frontmatter id; empty skips the check.
	Feature string
	// PreviousIDs are the IDs recorded by the last approval. Any of them that is
	// no longer defined was deleted or renumbered instead of retired.
	PreviousIDs []string
	// ProfileExists reports whether .sdd/profiles/<language>.md exists (technical only; nil skips).
	ProfileExists func(language string) bool
	// OtherFeatures are the project's other features: an ID written right after
	// one of their names ("admin-api AC-018") is reported as a reference that
	// should be qualified, instead of being resolved against this spec.
	OtherFeatures []string
}

// LintFunctional applies the functional-spec guardrails of docs/SPEC_GUIDELINES.md.
func LintFunctional(f *Functional, opts LintOptions) diag.List {
	var ds diag.List
	lintMeta(f.Meta, opts.Feature, &ds)
	lintSections(f.Sections, FunctionalSections, &ds)
	lintIDs(f.Defined, f.refs, opts, []Kind{KindFR, KindNFR, KindAC}, &ds)
	lintListed(f.xrefs, &ds)

	active := 0
	for _, r := range f.Requirements {
		if r.Retired {
			for _, c := range r.Criteria {
				if !c.Retired {
					ds.Warnf(c.Line, "%s belongs to retired %s; retire it too", c.ID, r.ID)
				}
			}
			continue
		}
		active++
		if r.Title == "" {
			ds.Errorf(r.Line, "%s has no title (use \"### %s: <title>\")", r.ID, r.ID)
		}
		if r.Text == "" {
			ds.Errorf(r.Line, "%s has no requirement statement", r.ID)
		} else if !mustShould.MatchString(r.Text) {
			ds.Warnf(r.Line, "%s should be a single testable statement using \"must\" or \"should\"", r.ID)
		}
		n := 0
		for _, c := range r.Criteria {
			if c.Retired {
				continue
			}
			n++
			if !givenWhenThen.MatchString(c.Text) {
				ds.Errorf(c.Line, "%s must be written as Given <context>, when <action>, then <outcome>", c.ID)
			}
		}
		if n == 0 {
			ds.Errorf(r.Line, "%s has no acceptance criteria (add \"- **AC-###:** Given …, when …, then …\")", r.ID)
		}
	}
	if active == 0 && hasSection(f.Sections, secFunctionalReqs) {
		ds.Errorf(sectionLine(f.Sections, secFunctionalReqs), "no functional requirements defined (### FR-###: <title>)")
	}
	for _, r := range f.NFRs {
		if !r.Retired && !hasDigit(r.Text+r.Title) {
			ds.Warnf(r.Line, "%s should be measurable (a number and a unit, e.g. \"p95 < 200 ms\")", r.ID)
		}
	}
	if len(f.OpenQuestions) > 0 {
		ds.Errorf(sectionLine(f.Sections, secOpenQuestions),
			"Open Questions must be empty before approval (%d open: %s)", len(f.OpenQuestions), truncate(f.OpenQuestions[0], 60))
	}
	ds = append(ds, Placeholders(f.lines)...)
	lintImplementationDetails(f.lines, f.codeLines, &ds)
	return ds
}

// LintTechnical applies the technical-spec guardrails that do not need the functional spec.
// See CheckTechnical for the spec↔spec checks.
func LintTechnical(t *Technical, opts LintOptions) diag.List {
	var ds diag.List
	lintMeta(t.Meta, opts.Feature, &ds)
	if t.Language == "" {
		ds.Errorf(1, "frontmatter: language is required (selects .sdd/profiles/<language>.md)")
	} else if opts.ProfileExists != nil && !opts.ProfileExists(t.Language) {
		ds.Warnf(1, "no profile .sdd/profiles/%s.md for language %q; the agent will get no language guidance", t.Language, t.Language)
	}
	for _, c := range []struct{ name, val string }{
		{"build", t.Commands.Build}, {"lint", t.Commands.Lint}, {"test", t.Commands.Test},
	} {
		if strings.TrimSpace(c.val) == "" {
			ds.Errorf(1, "frontmatter: commands.%s is required", c.name)
		}
	}
	if strings.TrimSpace(t.Commands.Integration) == "" {
		ds.Warnf(1, "frontmatter: commands.integration is empty; sdd validate will run no integration tests")
	}
	lintSections(t.Sections, TechnicalSections, &ds)
	lintIDs(t.Defined, t.refs, opts, []Kind{KindADR}, &ds)
	lintListed(t.xrefs, &ds)

	for _, m := range t.Traceability {
		lintMapping(m, secTraceability, []Kind{KindFR, KindNFR}, "component", &ds)
	}
	for _, m := range t.TestStrategy {
		lintMapping(m, secTesting, []Kind{KindAC}, "test level", &ds)
		for _, lv := range m.Targets {
			if !slices.Contains(TestLevelsAllowed, lv) {
				ds.Errorf(m.Line, "unknown test level %q (use %s)", lv, strings.Join(TestLevelsAllowed, ", "))
			}
		}
	}
	for _, d := range t.Decisions {
		if d.Retired {
			continue
		}
		body := strings.ToLower(d.Body)
		for _, part := range []string{"context", "decision", "consequences"} {
			if !strings.Contains(body, part) {
				ds.Warnf(d.Line, "%s should record its %s", d.ID, strings.ToUpper(part[:1])+part[1:])
			}
		}
	}
	ds = append(ds, Placeholders(t.lines)...)
	return ds
}

// CheckTechnical runs the spec↔spec checks: every active FR and NFR is mapped to a
// component, every active AC to a test level, and every referenced ID exists.
// Diagnostics are located in the technical spec.
//
// An ID written right after the name of one of otherFeatures is skipped: lint
// reports it as a reference to qualify.
func CheckTechnical(f *Functional, t *Technical, otherFeatures ...string) diag.List {
	var ds diag.List
	trLine, tsLine := sectionLine(t.Sections, secTraceability), sectionLine(t.Sections, secTesting)
	for _, id := range f.Active(KindFR, KindNFR) {
		if len(t.Components(id)) == 0 {
			ds.Errorf(trLine, "%s is not mapped to any component in %s", id, secTraceability)
		}
	}
	for _, id := range f.Active(KindAC) {
		if len(t.TestLevels(id)) == 0 {
			ds.Errorf(tsLine, "%s is not mapped to a test level in %s", id, secTesting)
		}
	}
	seen := map[string]bool{}
	for _, r := range t.refs {
		k := KindOf(r.ID)
		if k == KindADR || !ValidID(r.ID) || seen[r.ID+fmt.Sprint(r.Line)] || slices.Contains(otherFeatures, r.Prefix) {
			continue
		}
		seen[r.ID+fmt.Sprint(r.Line)] = true
		switch d, ok := f.Lookup(r.ID); {
		case !ok:
			ds.Errorf(r.Line, "%s is not defined in functional.md", r.ID)
		case d.Retired:
			ds.Warnf(r.Line, "%s is retired in functional.md", r.ID)
		}
	}
	return ds
}

var (
	mustShould    = regexp.MustCompile(`(?i)\b(must|should|shall)\b`)
	givenWhenThen = regexp.MustCompile(`(?is)\bgiven\b.*\bwhen\b.*\bthen\b`)
	digitRe       = regexp.MustCompile(`\d`)
	// placeholderRe matches template placeholders such as <requirement title>.
	placeholderRe = regexp.MustCompile(`<([A-Za-z][^>\n]*)>`)
	htmlTagRe     = regexp.MustCompile(`(?i)^/?(a|b|i|u|s|p|br|hr|em|strong|sub|sup|kbd|code|pre|div|span|img|details|summary|table|thead|tbody|tr|td|th|ul|ol|li|mark|small)(\s[^>]*)?/?$`)
	inlineCodeRe  = regexp.MustCompile("`[^`]*`")
	// implementationTerms hint that a functional spec describes HOW instead of WHAT.
	implementationTerms = regexp.MustCompile(`\b(SQL|PostgreSQL|Postgres|MySQL|SQLite|MongoDB|DynamoDB|Redis|Kafka|RabbitMQ|Kubernetes|Docker|React|Angular|Vue|Django|Flask|Spring Boot|Rails|Express\.js|GraphQL|gRPC)\b`)
)

func hasDigit(s string) bool { return digitRe.MatchString(s) }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func lintMeta(m Meta, feature string, ds *diag.List) {
	if feature != "" && m.ID != "" && m.ID != feature {
		ds.Errorf(1, "frontmatter: id %q does not match the feature directory %q", m.ID, feature)
	}
	if strings.TrimSpace(m.Title) == "" {
		ds.Warnf(1, "frontmatter: title is empty")
	}
}

func hasSection(secs []Heading, title string) bool { return sectionLine(secs, title) > 0 }

func sectionLine(secs []Heading, title string) int {
	for _, s := range secs {
		if sameTitle(s.Title, title) {
			return s.Line
		}
	}
	return 0
}

func lintSections(have []Heading, required []string, ds *diag.List) {
	seen := map[string]int{}
	var order []int
	for _, h := range have {
		for i, r := range required {
			if sameTitle(h.Title, r) {
				if first, dup := seen[r]; dup {
					ds.Warnf(h.Line, "section %q appears twice (first at line %d)", r, first)
					continue
				}
				seen[r] = h.Line
				order = append(order, i)
			}
		}
	}
	for _, r := range required {
		if _, ok := seen[r]; !ok {
			ds.Errorf(0, "missing required section \"## %s\"", r)
		}
	}
	if !slices.IsSorted(order) {
		ds.Warnf(0, "required sections are out of order (expected: %s)", strings.Join(required, ", "))
	}
}

// lintIDs checks ID format, numbering gaps, references to undefined IDs and IDs
// deleted since the last approval. kinds are the kinds this document defines.
// lintListed asks for the unqualified IDs of a list that starts with a
// qualified one to be qualified as well.
func lintListed(xs []CrossRef, ds *diag.List) {
	for _, x := range xs {
		if x.Listed {
			ds.Warnf(x.Line, "%s follows %s in a list, so it is checked against %s: write %s/%s, or separate it from the list if it means this spec's %s", x.ID, x.After, x.Feature, x.Feature, x.ID, x.ID)
		}
	}
}

func lintIDs(defs []Definition, refs []ref, opts LintOptions, kinds []Kind, ds *diag.List) {
	previous := opts.PreviousIDs
	defined := map[string]Definition{}
	nums := map[Kind][]int{}
	for _, d := range defs {
		defined[d.ID] = d
		if ValidID(d.ID) {
			nums[KindOf(d.ID)] = append(nums[KindOf(d.ID)], idNumber(d.ID))
		}
	}
	reported := map[string]bool{}
	for _, r := range refs {
		key := fmt.Sprintf("%s@%d", r.ID, r.Line)
		if reported[key] {
			continue
		}
		reported[key] = true
		if !ValidID(r.ID) {
			ds.Errorf(r.Line, "malformed ID %s: use the kind and three digits (e.g. %s-001)", r.ID, KindOf(r.ID))
			continue
		}
		if r.Prefix != "" && slices.Contains(opts.OtherFeatures, r.Prefix) && r.After != "" {
			ds.Warnf(r.Line, "%s follows %s in a list, so it is not checked against this spec: write %s/%s, or separate it from the list if it means this spec's %s", r.ID, r.After, r.Prefix, r.ID, r.ID)
			continue
		}
		if r.Prefix != "" && slices.Contains(opts.OtherFeatures, r.Prefix) {
			ds.Warnf(r.Line, "%s %s reads as a reference to feature %s: write %s/%s (an unqualified ID always means this spec's own)", r.Prefix, r.ID, r.Prefix, r.Prefix, r.ID)
			continue
		}
		if !slices.Contains(kinds, KindOf(r.ID)) {
			continue // defined in another document; checked by sync
		}
		d, ok := defined[r.ID]
		switch {
		case !ok:
			ds.Errorf(r.Line, "%s is referenced but not defined", r.ID)
		case d.Retired && d.Line != r.Line:
			ds.Warnf(r.Line, "%s is retired", r.ID)
		}
	}
	for _, k := range kinds {
		ns := nums[k]
		if len(ns) == 0 {
			continue
		}
		slices.Sort(ns)
		var missing []string
		for want, i := 1, 0; want <= ns[len(ns)-1]; want++ {
			for i < len(ns) && ns[i] < want {
				i++
			}
			if i >= len(ns) || ns[i] != want {
				missing = append(missing, fmt.Sprintf("%s-%03d", k, want))
			}
		}
		if len(missing) > 0 {
			ds.Errorf(0, "%s numbering has gaps (%s missing): IDs are sequential and never deleted; retire them as ~~%s~~ (removed)",
				k, strings.Join(missing, ", "), missing[0])
		}
	}
	for _, id := range previous {
		if _, ok := defined[id]; !ok && slices.Contains(kinds, KindOf(id)) {
			ds.Errorf(0, "%s was approved before and is now gone: never delete or renumber IDs; retire it as ~~%s~~ (removed)", id, id)
		}
	}
}

func lintMapping(m Mapping, section string, kinds []Kind, target string, ds *diag.List) {
	if len(m.IDs) == 0 {
		ds.Warnf(m.Line, "row in %s does not reference any %s ID", section, kindNames(kinds))
		return
	}
	for _, id := range m.IDs {
		if !slices.Contains(kinds, KindOf(id)) {
			ds.Errorf(m.Line, "%s does not belong in %s (expected %s IDs)", id, section, kindNames(kinds))
		}
	}
	if len(m.Targets) == 0 {
		ds.Errorf(m.Line, "%s has no %s in %s", strings.Join(m.IDs, ", "), target, section)
	}
}

func kindNames(kinds []Kind) string {
	var s []string
	for _, k := range kinds {
		s = append(s, string(k))
	}
	return strings.Join(s, "/")
}

// Placeholders reports template placeholders such as <requirement title> left in the text.
func Placeholders(lines []markdown.Line) diag.List {
	var ds diag.List
	for _, l := range lines {
		text := inlineCodeRe.ReplaceAllString(l.Text, "")
		for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
			inner := m[1]
			if htmlTagRe.MatchString(inner) || strings.Contains(inner, "://") || strings.Contains(inner, "@") {
				continue
			}
			ds.Errorf(l.N, "template placeholder %s has not been filled in", m[0])
		}
	}
	return ds
}

func lintImplementationDetails(lines []markdown.Line, codeLines []int, ds *diag.List) {
	if len(codeLines) > 0 {
		ds.Warnf(codeLines[0], "code block in a functional spec: describe WHAT, not HOW")
	}
	seen := map[string]bool{}
	for _, l := range lines {
		for _, term := range implementationTerms.FindAllString(l.Text, -1) {
			if !seen[term] {
				seen[term] = true
				ds.Warnf(l.N, "%q looks like an implementation detail: keep it in technical.md", term)
			}
		}
	}
}
