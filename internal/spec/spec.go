// Package spec parses and lints functional and technical specifications
// (docs/SPEC_GUIDELINES.md). Parsing is lenient and line-oriented: it builds
// as much of the model as it can and reports structural problems as
// diagnostics instead of failing.
package spec

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/darkcarard/sdd-framework/internal/markdown"
)

// ID patterns (three-digit, never renumbered).
var (
	FRPattern  = regexp.MustCompile(`\bFR-\d{3}\b`)
	NFRPattern = regexp.MustCompile(`\bNFR-\d{3}\b`)
	ACPattern  = regexp.MustCompile(`\bAC-\d{3}\b`)
	ADRPattern = regexp.MustCompile(`\bADR-\d{3}\b`)
)

// anyIDRe matches spec IDs with any number of digits, so malformed ones
// (FR-1, AC-0001) can be reported. Group 1 is the kind, group 2 the digits.
var anyIDRe = regexp.MustCompile(`\b(FR|NFR|AC|ADR)-(\d+)\b`)

// Kind is the prefix of a spec ID.
type Kind string

const (
	KindFR  Kind = "FR"
	KindNFR Kind = "NFR"
	KindAC  Kind = "AC"
	KindADR Kind = "ADR"
)

// KindOf returns the kind of a spec ID, or "" if id is not one.
func KindOf(id string) Kind {
	m := anyIDRe.FindStringSubmatch(id)
	if m == nil || m[0] != id {
		return ""
	}
	return Kind(m[1])
}

// ValidID reports whether id is a well-formed spec ID (three digits).
func ValidID(id string) bool {
	m := anyIDRe.FindStringSubmatch(id)
	return m != nil && m[0] == id && len(m[2]) == 3
}

func idNumber(id string) int {
	_, num, _ := strings.Cut(id, "-")
	n, _ := strconv.Atoi(num)
	return n
}

// SortIDs sorts IDs by kind (FR, NFR, AC, ADR) and then numerically.
func SortIDs(ids []string) {
	rank := map[Kind]int{KindFR: 0, KindNFR: 1, KindAC: 2, KindADR: 3}
	sort.SliceStable(ids, func(i, j int) bool {
		ki, kj := KindOf(ids[i]), KindOf(ids[j])
		if ki != kj {
			return rank[ki] < rank[kj]
		}
		return idNumber(ids[i]) < idNumber(ids[j])
	})
}

// ref is an ID mentioned in a document, with its line.
type ref struct {
	ID   string
	Line int
}

// Meta is the common frontmatter.
type Meta struct {
	ID        string   `yaml:"id"`
	Title     string   `yaml:"title"`
	Status    string   `yaml:"status"`
	Version   int      `yaml:"version"`
	DependsOn []string `yaml:"depends_on"`
}

// Heading is a level-2 section heading.
type Heading struct {
	Title string
	Line  int
}

// Definition is where an ID is defined.
type Definition struct {
	ID      string
	Line    int
	Retired bool
}

// Requirement is an FR-### or NFR-###.
type Requirement struct {
	ID       string
	Title    string
	Text     string
	Line     int
	Criteria []Criterion // only for FRs
	Retired  bool
}

// Criterion is an AC-### (Given/When/Then).
type Criterion struct {
	ID            string
	RequirementID string
	Text          string
	Line          int
	Retired       bool
}

// Functional is a parsed functional.md.
type Functional struct {
	Meta          Meta
	Sections      []Heading
	Requirements  []Requirement
	NFRs          []Requirement
	OpenQuestions []string
	// Defined lists every ID defined in the spec, in document order.
	Defined []Definition

	lines     []markdown.Line
	codeLines []int
	refs      []ref
}

// Commands are the verification commands declared by the technical spec.
type Commands struct {
	Build       string `yaml:"build"`
	Lint        string `yaml:"lint"`
	Test        string `yaml:"test"`
	Integration string `yaml:"integration"`
}

// Mapping is one row of a traceability table: spec IDs mapped to targets
// (components in Requirement Traceability, test levels in Testing Strategy).
type Mapping struct {
	IDs     []string
	Targets []string
	Line    int
}

// Decision is an ADR-### in technical.md.
type Decision struct {
	ID      string
	Title   string
	Line    int
	Retired bool
	Body    string
}

// Technical is a parsed technical.md.
type Technical struct {
	Meta           Meta     `yaml:",inline"`
	Language       string   `yaml:"language"`
	FunctionalHash string   `yaml:"functional_hash"`
	Commands       Commands `yaml:"commands"`

	Sections []Heading `yaml:"-"`
	// Traceability maps FR/NFR IDs to components.
	Traceability []Mapping `yaml:"-"`
	// TestStrategy maps AC IDs to test levels (unit, integration, e2e).
	TestStrategy []Mapping  `yaml:"-"`
	Decisions    []Decision `yaml:"-"`
	// Architecture is the text of the Architecture section, where the layers are named.
	Architecture string       `yaml:"-"`
	Defined      []Definition `yaml:"-"`

	lines []markdown.Line
	refs  []ref
}

// Components returns the components mapped to id in Requirement Traceability.
func (t *Technical) Components(id string) []string { return targets(t.Traceability, id) }

// TestLevels returns the test levels mapped to id in Testing Strategy.
func (t *Technical) TestLevels(id string) []string { return targets(t.TestStrategy, id) }

func targets(rows []Mapping, id string) []string {
	var out []string
	for _, r := range rows {
		for _, x := range r.IDs {
			if x == id {
				out = append(out, r.Targets...)
			}
		}
	}
	return out
}

// TestLevelsAllowed are the valid levels in the Testing Strategy table.
var TestLevelsAllowed = []string{"unit", "integration", "e2e"}

// Required section headings (level-2) per spec type, in order.
var (
	FunctionalSections = []string{"Summary", "Problem & Goals", "Non-Goals", "Users & Scenarios",
		"Functional Requirements", "Non-Functional Requirements", "Constraints", "Open Questions", "Glossary"}
	TechnicalSections = []string{"Overview", "Architecture", "Components", "Data Model",
		"Interfaces / APIs", "Requirement Traceability", "Error Handling", "Testing Strategy",
		"Decisions", "Risks"}
)

// Criterion returns the AC with the given ID, if defined.
func (f *Functional) Criterion(id string) (Criterion, bool) {
	for _, r := range f.Requirements {
		for _, c := range r.Criteria {
			if c.ID == id {
				return c, true
			}
		}
	}
	return Criterion{}, false
}

// Requirement returns the FR or NFR with the given ID, if defined.
func (f *Functional) Requirement(id string) (Requirement, bool) {
	for _, list := range [][]Requirement{f.Requirements, f.NFRs} {
		for _, r := range list {
			if r.ID == id {
				return r, true
			}
		}
	}
	return Requirement{}, false
}

// Lookup returns the definition of id, if any.
func (f *Functional) Lookup(id string) (Definition, bool) { return lookup(f.Defined, id) }

// Lookup returns the definition of id (an ADR), if any.
func (t *Technical) Lookup(id string) (Definition, bool) { return lookup(t.Defined, id) }

func lookup(defs []Definition, id string) (Definition, bool) {
	for _, d := range defs {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

// Active returns the IDs of the given kinds that are defined and not retired, in document order.
func (f *Functional) Active(kinds ...Kind) []string {
	var out []string
	for _, d := range f.Defined {
		if !d.Retired && containsKind(kinds, KindOf(d.ID)) {
			out = append(out, d.ID)
		}
	}
	return out
}

// IDs returns every defined ID (including retired ones).
func (f *Functional) IDs() []string { return defIDs(f.Defined) }

// IDs returns every defined ADR ID (including retired ones).
func (t *Technical) IDs() []string { return defIDs(t.Defined) }

func defIDs(defs []Definition) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.ID)
	}
	return out
}

func containsKind(kinds []Kind, k Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}
