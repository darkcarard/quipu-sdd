package spec

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/darkcarard/quipu-sdd/internal/diag"
	"github.com/darkcarard/quipu-sdd/internal/markdown"
)

// defRe matches an ID definition at the start of a heading or bullet, after
// bold markers are removed: "FR-001: title", "~~FR-004~~ (removed)", "~~AC-002: text~~".
var defRe = regexp.MustCompile(`^(~~)?\s*((?:FR|NFR|AC|ADR)-\d+)\s*(~~)?\s*[:.—–-]?\s*(.*)$`)

// parseDef parses an ID definition. ok is false when text does not start with an ID.
func parseDef(text string) (id string, rest string, retired bool, ok bool) {
	text = strings.NewReplacer("**", "", "__", "").Replace(strings.TrimSpace(text))
	m := defRe.FindStringSubmatch(text)
	if m == nil {
		return "", "", false, false
	}
	rest = strings.TrimSpace(strings.TrimSuffix(m[4], "~~"))
	return m[2], rest, m[1] != "", true
}

const (
	secFunctionalReqs = "Functional Requirements"
	secNonFunctional  = "Non-Functional Requirements"
	secOpenQuestions  = "Open Questions"
)

// ParseFunctional parses functional.md. The returned spec is never nil; the
// diagnostics report problems that prevented parts of it from being read.
func ParseFunctional(content []byte) (*Functional, diag.List) {
	f := &Functional{}
	var ds diag.List
	doc, err := markdown.Parse(content)
	if err != nil {
		ds.Errorf(1, "%v", err)
		return f, ds
	}
	f.lines, f.codeLines = doc.Lines, doc.CodeLines
	decodeFrontmatter(doc, &f.Meta, &ds)

	for _, s := range doc.Sections(2) {
		f.Sections = append(f.Sections, Heading{Title: s.Title, Line: s.Line})
		switch {
		case sameTitle(s.Title, secFunctionalReqs):
			f.parseFRs(s, &ds)
		case sameTitle(s.Title, secNonFunctional):
			f.parseNFRs(s, &ds)
		case sameTitle(s.Title, secOpenQuestions):
			f.OpenQuestions = openQuestions(s.Body)
		}
	}
	f.refs = findRefs(doc.Lines)
	return f, ds
}

func decodeFrontmatter(doc *markdown.Doc, into any, ds *diag.List) {
	if !doc.HasFrontmatter {
		ds.Errorf(1, "missing YAML frontmatter (--- ... ---)")
		return
	}
	if err := yaml.Unmarshal([]byte(doc.Frontmatter), into); err != nil {
		ds.Errorf(doc.FrontmatterLine, "invalid frontmatter: %v", err)
	}
}

func sameTitle(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), b) }

func (f *Functional) define(id string, line int, retired bool, ds *diag.List) {
	if d, dup := f.Lookup(id); dup {
		ds.Errorf(line, "%s is defined twice (first at line %d); IDs must be unique", id, d.Line)
		return
	}
	f.Defined = append(f.Defined, Definition{ID: id, Line: line, Retired: retired})
}

func (f *Functional) parseFRs(s markdown.Section, ds *diag.List) {
	pre, subs := markdown.SectionsIn(s.Body, 3)
	for _, l := range pre {
		if _, content, ok := markdown.Bullet(l.Text); ok {
			if id, _, _, ok := parseDef(content); ok && KindOf(id) == KindAC {
				ds.Errorf(l.N, "%s is not under an FR-### heading", id)
			}
		}
	}
	for _, sub := range subs {
		id, title, retired, ok := parseDef(sub.Title)
		if !ok || KindOf(id) != KindFR {
			ds.Warnf(sub.Line, "heading %q under %s is not an FR-### definition", sub.Title, secFunctionalReqs)
			continue
		}
		r := Requirement{ID: id, Title: title, Line: sub.Line, Retired: retired}
		f.define(id, sub.Line, retired, ds)
		var text []string
		inAC := false
		for _, l := range sub.Body {
			if strings.TrimSpace(l.Text) == "" {
				inAC = false
				continue
			}
			if _, content, ok := markdown.Bullet(l.Text); ok {
				if acID, acText, acRetired, ok := parseDef(content); ok && KindOf(acID) == KindAC {
					r.Criteria = append(r.Criteria, Criterion{
						ID: acID, RequirementID: id, Text: acText, Line: l.N, Retired: acRetired,
					})
					f.define(acID, l.N, acRetired, ds)
					inAC = true
					continue
				}
				inAC = false
			}
			if inAC && startsIndented(l.Text) {
				c := &r.Criteria[len(r.Criteria)-1]
				c.Text = strings.TrimSpace(c.Text + " " + strings.TrimSpace(l.Text))
				continue
			}
			inAC = false
			text = append(text, strings.TrimSpace(l.Text))
		}
		r.Text = strings.Join(text, " ")
		f.Requirements = append(f.Requirements, r)
	}
}

func startsIndented(s string) bool { return strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t") }

func (f *Functional) parseNFRs(s markdown.Section, ds *diag.List) {
	pre, subs := markdown.SectionsIn(s.Body, 3)
	for _, l := range pre {
		_, content, ok := markdown.Bullet(l.Text)
		if !ok {
			continue
		}
		id, text, retired, ok := parseDef(content)
		if !ok {
			continue
		}
		if KindOf(id) != KindNFR {
			if KindOf(id) == KindAC {
				ds.Errorf(l.N, "%s must be defined under an FR, not in %s", id, secNonFunctional)
			}
			continue
		}
		f.NFRs = append(f.NFRs, Requirement{ID: id, Text: text, Line: l.N, Retired: retired})
		f.define(id, l.N, retired, ds)
	}
	for _, sub := range subs {
		id, title, retired, ok := parseDef(sub.Title)
		if !ok || KindOf(id) != KindNFR {
			ds.Warnf(sub.Line, "heading %q under %s is not an NFR-### definition", sub.Title, secNonFunctional)
			continue
		}
		var text []string
		for _, l := range sub.Body {
			if t := strings.TrimSpace(l.Text); t != "" {
				text = append(text, t)
			}
		}
		f.NFRs = append(f.NFRs, Requirement{ID: id, Title: title, Text: strings.Join(text, " "), Line: sub.Line, Retired: retired})
		f.define(id, sub.Line, retired, ds)
	}
}

// emptyAnswers are Open Questions entries that mean "there are none".
var emptyAnswers = map[string]bool{"none": true, "none.": true, "n/a": true, "-": true, "—": true}

func openQuestions(body []markdown.Line) []string {
	var out []string
	for _, l := range body {
		t := strings.TrimSpace(l.Text)
		if _, content, ok := markdown.Bullet(l.Text); ok {
			t = content
		}
		if t == "" || emptyAnswers[strings.ToLower(strings.Trim(t, "_*"))] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// findRefs returns every spec ID mentioned in lines, skipping cross-feature
// references written as <feature>/<ID>.
func findRefs(lines []markdown.Line) []ref {
	var out []ref
	for _, l := range lines {
		for _, loc := range anyIDRe.FindAllStringIndex(l.Text, -1) {
			if loc[0] > 0 && l.Text[loc[0]-1] == '/' {
				continue
			}
			out = append(out, ref{ID: l.Text[loc[0]:loc[1]], Line: l.N})
		}
	}
	return out
}
