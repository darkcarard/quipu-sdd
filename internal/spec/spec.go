// Package spec models functional and technical specifications
// (docs/SPEC_GUIDELINES.md). Parsing and lint arrive in M1.
package spec

import "regexp"

// ID patterns (three-digit, never renumbered).
var (
	FRPattern  = regexp.MustCompile(`\bFR-\d{3}\b`)
	NFRPattern = regexp.MustCompile(`\bNFR-\d{3}\b`)
	ACPattern  = regexp.MustCompile(`\bAC-\d{3}\b`)
	ADRPattern = regexp.MustCompile(`\bADR-\d{3}\b`)
)

// Meta is the common frontmatter.
type Meta struct {
	ID        string   `yaml:"id"`
	Title     string   `yaml:"title"`
	Status    string   `yaml:"status"`
	Version   int      `yaml:"version"`
	DependsOn []string `yaml:"depends_on"`
}

// Requirement is an FR-### or NFR-###.
type Requirement struct {
	ID       string
	Title    string
	Text     string
	Criteria []Criterion // only for FRs
	Retired  bool
}

// Criterion is an AC-### (Given/When/Then).
type Criterion struct {
	ID            string
	RequirementID string
	Text          string
}

// Functional is a parsed functional.md.
type Functional struct {
	Meta          Meta
	Sections      map[string]string
	Requirements  []Requirement
	NFRs          []Requirement
	OpenQuestions []string
}

// Commands are the verification commands declared by the technical spec.
type Commands struct {
	Build       string `yaml:"build"`
	Lint        string `yaml:"lint"`
	Test        string `yaml:"test"`
	Integration string `yaml:"integration"`
}

// Technical is a parsed technical.md.
type Technical struct {
	Meta           Meta
	Language       string   `yaml:"language"`
	FunctionalHash string   `yaml:"functional_hash"`
	Commands       Commands `yaml:"commands"`
	Sections       map[string]string
	// Traceability maps FR/NFR IDs to components.
	Traceability map[string][]string
	// TestStrategy maps AC IDs to test levels (unit, integration, e2e).
	TestStrategy map[string]string
}

// Required section headings (level-2) per spec type, in order.
var (
	FunctionalSections = []string{"Summary", "Problem & Goals", "Non-Goals", "Users & Scenarios",
		"Functional Requirements", "Non-Functional Requirements", "Constraints", "Open Questions", "Glossary"}
	TechnicalSections = []string{"Overview", "Architecture", "Components", "Data Model",
		"Interfaces / APIs", "Requirement Traceability", "Error Handling", "Testing Strategy",
		"Decisions", "Risks"}
)
