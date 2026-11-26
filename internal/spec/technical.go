package spec

import (
	"strings"

	"github.com/darkcarard/quipu-sdd/internal/diag"
	"github.com/darkcarard/quipu-sdd/internal/markdown"
)

const (
	secTraceability = "Requirement Traceability"
	secTesting      = "Testing Strategy"
	secDecisions    = "Decisions"
	secArchitecture = "Architecture"
)

// ParseTechnical parses technical.md. The returned spec is never nil.
func ParseTechnical(content []byte) (*Technical, diag.List) {
	t := &Technical{}
	var ds diag.List
	doc, err := markdown.Parse(content)
	if err != nil {
		ds.Errorf(1, "%v", err)
		return t, ds
	}
	t.lines = doc.Lines
	decodeFrontmatter(doc, t, &ds)

	for _, s := range doc.Sections(2) {
		t.Sections = append(t.Sections, Heading{Title: s.Title, Line: s.Line})
		switch {
		case sameTitle(s.Title, secTraceability):
			t.Traceability = mappings(s, func(cell string) []string { return markdown.SplitList(cell) })
		case sameTitle(s.Title, secTesting):
			t.TestStrategy = mappings(s, func(cell string) []string {
				levels := markdown.SplitList(strings.ReplaceAll(cell, "/", ","))
				for i := range levels {
					levels[i] = strings.ToLower(levels[i])
				}
				return levels
			})
		case sameTitle(s.Title, secDecisions):
			t.parseDecisions(s, &ds)
		case sameTitle(s.Title, secArchitecture):
			var lines []string
			for _, l := range s.Body {
				lines = append(lines, l.Text)
			}
			t.Architecture = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	t.refs, t.xrefs = findRefs(doc.Lines)
	return t, ds
}

// mappings reads the first table of a section: the first column holds spec IDs,
// the second the targets.
func mappings(s markdown.Section, split func(string) []string) []Mapping {
	_, rows := markdown.Table(s.Body)
	var out []Mapping
	for _, r := range rows {
		if len(r.Cells) == 0 {
			continue
		}
		m := Mapping{Line: r.N, IDs: anyIDRe.FindAllString(r.Cells[0], -1)}
		if len(r.Cells) > 1 {
			m.Targets = split(r.Cells[1])
		}
		out = append(out, m)
	}
	return out
}

func (t *Technical) parseDecisions(s markdown.Section, ds *diag.List) {
	_, subs := markdown.SectionsIn(s.Body, 3)
	for _, sub := range subs {
		id, title, retired, ok := parseDef(sub.Title)
		if !ok || KindOf(id) != KindADR {
			ds.Warnf(sub.Line, "heading %q under %s is not an ADR-### definition", sub.Title, secDecisions)
			continue
		}
		if d, dup := t.Lookup(id); dup {
			ds.Errorf(sub.Line, "%s is defined twice (first at line %d); IDs must be unique", id, d.Line)
			continue
		}
		var body []string
		for _, l := range sub.Body {
			body = append(body, l.Text)
		}
		t.Decisions = append(t.Decisions, Decision{
			ID: id, Title: title, Line: sub.Line, Retired: retired, Body: strings.Join(body, "\n"),
		})
		t.Defined = append(t.Defined, Definition{ID: id, Line: sub.Line, Retired: retired})
	}
}
