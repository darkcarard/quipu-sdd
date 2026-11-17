// Package plan parses and validates the implementation plan
// (specs/<feature>/plan.md): layered groups of small, reviewable tasks.
// The format is fixed so it can be parsed; see docs/SPEC_GUIDELINES.md.
package plan

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/markdown"
)

// Plan is a parsed plan.md.
type Plan struct {
	ID             string  `yaml:"id"`
	Title          string  `yaml:"title"`
	FunctionalHash string  `yaml:"functional_hash"`
	TechnicalHash  string  `yaml:"technical_hash"`
	Groups         []Group `yaml:"-"`

	lines []markdown.Line
}

// Group is a G# block, implemented and committed as one unit.
type Group struct {
	ID        string // G1, G2, ...
	Title     string
	Line      int
	Layer     string
	DependsOn []string
	Goal      string
	Tasks     []Task

	fields map[string]int // field name → line, for presence checks
}

// Task is a T#.# block.
type Task struct {
	ID          string // T1.1, ...
	Title       string
	Line        int
	Covers      []string // FR/NFR/AC IDs
	Files       []string
	DoneWhen    string
	Verify      string
	Description string

	fields map[string]int
}

// ID patterns for groups and tasks.
var (
	GroupIDRe = regexp.MustCompile(`^G\d+$`)
	TaskIDRe  = regexp.MustCompile(`^T\d+\.\d+$`)
)

var (
	groupHeadRe = regexp.MustCompile(`^(G\d+)\s*[:.—–-]?\s*(.*)$`)
	taskHeadRe  = regexp.MustCompile(`^(T\d+\.\d+)\s*[:.—–-]?\s*(.*)$`)
	fieldRe     = regexp.MustCompile(`^([A-Za-z][A-Za-z _-]*?)\s*:\s*(.*)$`)
)

// Covered returns, for every ID covered by at least one task, the tasks covering it.
func (p *Plan) Covered() map[string][]string {
	out := map[string][]string{}
	for _, g := range p.Groups {
		for _, t := range g.Tasks {
			for _, id := range t.Covers {
				out[id] = append(out[id], t.ID)
			}
		}
	}
	return out
}

// Task returns the task with the given ID and its group.
func (p *Plan) Task(id string) (Task, Group, bool) {
	for _, g := range p.Groups {
		for _, t := range g.Tasks {
			if t.ID == id {
				return t, g, true
			}
		}
	}
	return Task{}, Group{}, false
}

// Group returns the group with the given ID.
func (p *Plan) Group(id string) (Group, bool) {
	for _, g := range p.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// Parse parses plan.md. The returned plan is never nil.
func Parse(content []byte) (*Plan, diag.List) {
	p := &Plan{}
	var ds diag.List
	doc, err := markdown.Parse(content)
	if err != nil {
		ds.Errorf(1, "%v", err)
		return p, ds
	}
	p.lines = doc.Lines
	if !doc.HasFrontmatter {
		ds.Errorf(1, "missing YAML frontmatter (--- ... ---)")
	} else if err := yaml.Unmarshal([]byte(doc.Frontmatter), p); err != nil {
		ds.Errorf(doc.FrontmatterLine, "invalid frontmatter: %v", err)
	}

	for _, s := range doc.Sections(2) {
		m := groupHeadRe.FindStringSubmatch(strings.TrimSpace(s.Title))
		if m == nil {
			ds.Warnf(s.Line, "section %q is not a group (expected \"## G#: <title>\")", s.Title)
			continue
		}
		g := Group{ID: m[1], Title: strings.TrimSpace(m[2]), Line: s.Line, fields: map[string]int{}}
		pre, subs := markdown.SectionsIn(s.Body, 3)
		for _, l := range pre {
			key, val, ok := field(l.Text)
			if !ok {
				continue
			}
			g.fields[key] = l.N
			switch key {
			case "layer":
				g.Layer = val
			case "depends_on":
				g.DependsOn = markdown.InlineList(val)
			case "goal":
				g.Goal = val
			}
		}
		for _, sub := range subs {
			tm := taskHeadRe.FindStringSubmatch(strings.TrimSpace(sub.Title))
			if tm == nil {
				ds.Warnf(sub.Line, "heading %q in %s is not a task (expected \"### T#.#: <title>\")", sub.Title, g.ID)
				continue
			}
			g.Tasks = append(g.Tasks, parseTask(tm[1], strings.TrimSpace(tm[2]), sub))
		}
		p.Groups = append(p.Groups, g)
	}
	return p, ds
}

func parseTask(id, title string, s markdown.Section) Task {
	t := Task{ID: id, Title: title, Line: s.Line, fields: map[string]int{}}
	var desc []string
	for _, l := range s.Body {
		if indent, _, isBullet := markdown.Bullet(l.Text); isBullet && indent == 0 {
			if key, val, ok := field(l.Text); ok && isTaskField(key) {
				t.fields[key] = l.N
				switch key {
				case "covers":
					t.Covers = markdown.InlineList(val)
				case "files":
					t.Files = markdown.InlineList(val)
				case "done_when":
					t.DoneWhen = val
				case "verify":
					t.Verify = val
				}
				continue
			}
		}
		desc = append(desc, l.Text)
	}
	t.Description = strings.TrimSpace(strings.Join(desc, "\n"))
	return t
}

func isTaskField(key string) bool {
	switch key {
	case "covers", "files", "done_when", "verify":
		return true
	}
	return false
}

// field parses a "- key: value" bullet. Keys are normalized to snake_case.
func field(text string) (key, val string, ok bool) {
	_, content, isBullet := markdown.Bullet(text)
	if !isBullet {
		return "", "", false
	}
	content = strings.ReplaceAll(content, "**", "")
	m := fieldRe.FindStringSubmatch(content)
	if m == nil {
		return "", "", false
	}
	key = strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(m[1])))
	return key, strings.TrimSpace(m[2]), true
}
