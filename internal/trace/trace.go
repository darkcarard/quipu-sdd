// Package trace builds the requirement → technical → plan → commit → test chain
// used by `sdd trace` and `sdd sync check` (docs/TRACEABILITY.md §1).
// Spec/plan links arrive in M1; commit and test links in M6.
package trace

// Kind is the kind of artifact a link points to.
type Kind string

const (
	KindRequirement Kind = "requirement" // FR/NFR in functional.md
	KindCriterion   Kind = "criterion"   // AC in functional.md
	KindComponent   Kind = "component"   // technical.md traceability
	KindTask        Kind = "task"        // plan.md
	KindCommit      Kind = "commit"      // git commit with SDD trailers
	KindTest        Kind = "test"        // test referencing `covers: AC-###`
)

// Node is one element of the chain.
type Node struct {
	Kind     Kind
	ID       string // FR-001, T2.1, a1b2c3d, TestFoo
	Location string // file:line or commit SHA
	Title    string
}

// Link connects an ID to a node that references it.
type Link struct {
	From string
	To   Node
}

// Gap is a missing link reported by sync checks (e.g. an AC with no test).
type Gap struct {
	ID      string
	Missing Kind
	Message string
}
