// Package workflow defines the SDD feature state machine (docs/WORKFLOW.md):
// feature phases, group and task statuses, valid transitions and hash-based
// staleness detection between artifacts.
package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"
)

// Phase is the state of a feature.
type Phase string

const (
	FunctionalDrafting Phase = "functional-drafting"
	FunctionalApproved Phase = "functional-approved"
	TechnicalDrafting  Phase = "technical-drafting"
	TechnicalApproved  Phase = "technical-approved"
	PlanDrafting       Phase = "plan-drafting"
	PlanApproved       Phase = "plan-approved"
	Implementing       Phase = "implementing"
	Implemented        Phase = "implemented"
	Validated          Phase = "validated"
	// Baselined: a baseline feature documents code that already exists. Both
	// specs are approved; it has no plan and no implementation.
	Baselined Phase = "baselined"
)

// phaseOrder lists phases in workflow order.
var phaseOrder = []Phase{
	FunctionalDrafting, FunctionalApproved, TechnicalDrafting, TechnicalApproved,
	PlanDrafting, PlanApproved, Implementing, Implemented, Validated, Baselined,
}

// Phases returns all phases in workflow order.
func Phases() []Phase { return slices.Clone(phaseOrder) }

func (p Phase) index() int { return slices.Index(phaseOrder, p) }

// Valid reports whether p is a known phase.
func (p Phase) Valid() bool { return p.index() >= 0 }

// isDrafting reports whether p is one of the *-drafting phases a feature can be reopened to.
func (p Phase) isDrafting() bool {
	return p == FunctionalDrafting || p == TechnicalDrafting || p == PlanDrafting
}

// CanTransition reports whether a feature may move from one phase to another.
// Allowed moves are the next phase in order (the forward gates) and reopening an
// earlier *-drafting phase, which is how an edited or stale artifact is reworked.
func CanTransition(from, to Phase) bool {
	fi, ti := from.index(), to.index()
	if fi < 0 || ti < 0 {
		return false
	}
	return ti == fi+1 || (ti < fi && to.isDrafting())
}

// Transition returns an error if the move is not allowed.
func Transition(from, to Phase) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid phase transition %s → %s", from, to)
	}
	return nil
}

// GroupStatus is the state of a plan group during implementation.
type GroupStatus string

const (
	GroupPending        GroupStatus = "pending"
	GroupInProgress     GroupStatus = "in-progress"
	GroupAwaitingReview GroupStatus = "awaiting-review"
	GroupApproved       GroupStatus = "approved"
	GroupFailed         GroupStatus = "failed"
)

var groupTransitions = map[GroupStatus][]GroupStatus{
	GroupPending:        {GroupInProgress},
	GroupInProgress:     {GroupAwaitingReview, GroupFailed},
	GroupAwaitingReview: {GroupApproved, GroupInProgress}, // approve | revise
	GroupFailed:         {GroupInProgress},                // resume / retry
}

// CanTransitionGroup reports whether a group may move between statuses.
func CanTransitionGroup(from, to GroupStatus) bool {
	return slices.Contains(groupTransitions[from], to)
}

// TaskStatus is the state of a single plan task.
type TaskStatus string

const (
	TaskPending     TaskStatus = "pending"
	TaskInProgress  TaskStatus = "in-progress"
	TaskDone        TaskStatus = "done"
	TaskFailed      TaskStatus = "failed"
	TaskInterrupted TaskStatus = "interrupted"
)

var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskPending:     {TaskInProgress},
	TaskInProgress:  {TaskDone, TaskFailed, TaskInterrupted},
	TaskFailed:      {TaskInProgress},
	TaskInterrupted: {TaskInProgress},
	TaskDone:        {TaskInProgress}, // a revise request may reopen a finished task
}

// CanTransitionTask reports whether a task may move between statuses.
func CanTransitionTask(from, to TaskStatus) bool {
	return slices.Contains(taskTransitions[from], to)
}

// Artifact identifies a versioned spec artifact of a feature.
type Artifact string

const (
	Functional Artifact = "functional"
	Technical  Artifact = "technical"
	Plan       Artifact = "plan"
)

// Sources returns the artifacts a given artifact is derived from.
func Sources(a Artifact) []Artifact {
	switch a {
	case Technical:
		return []Artifact{Functional}
	case Plan:
		return []Artifact{Functional, Technical}
	}
	return nil
}

// Artifacts returns every artifact in dependency order.
func Artifacts() []Artifact { return []Artifact{Functional, Technical, Plan} }

// Approval records the exact content a user approved.
type Approval struct {
	Hash         string              `json:"hash"`
	SourceHashes map[Artifact]string `json:"source_hashes,omitempty"`
	By           string              `json:"by"`
	At           time.Time           `json:"at"`
	// IDs are the IDs defined by the approved content (including retired ones),
	// so a later edit that deletes or renumbers an ID can be detected.
	IDs []string `json:"ids,omitempty"`
}

// ArtifactStatus is the derived status of an artifact.
type ArtifactStatus string

const (
	StatusMissing  ArtifactStatus = "missing"  // the file does not exist
	StatusDraft    ArtifactStatus = "draft"    // exists, never approved
	StatusApproved ArtifactStatus = "approved" // content and sources match the approval
	StatusModified ArtifactStatus = "modified" // edited after approval
	StatusStale    ArtifactStatus = "stale"    // a source changed after this artifact was approved
)

// Hash returns the canonical content hash used for approvals ("sha256:<hex>").
// CRLF line endings are normalized to LF first, so a checkout with
// core.autocrlf on another machine does not make approved artifacts look edited.
func Hash(content []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Evaluate derives the status of every artifact from its approvals and the
// current content hashes (an empty hash means the file is missing).
// Modification takes precedence over staleness: a modified artifact must be
// re-approved regardless of its sources.
func Evaluate(approvals map[Artifact]Approval, current map[Artifact]string) map[Artifact]ArtifactStatus {
	out := map[Artifact]ArtifactStatus{}
	for _, a := range Artifacts() {
		cur := current[a]
		appr, approved := approvals[a]
		switch {
		case cur == "":
			out[a] = StatusMissing
		case !approved:
			out[a] = StatusDraft
		case appr.Hash != cur:
			out[a] = StatusModified
		default:
			out[a] = StatusApproved
			for _, src := range Sources(a) {
				if appr.SourceHashes[src] != current[src] {
					out[a] = StatusStale
					break
				}
			}
		}
	}
	return out
}

// Effective returns the phase a feature is really in, given its stored phase and
// the derived artifact statuses. Up to plan-approved the phase follows from the
// artifacts alone, so hand edits and pulls from other machines are always
// reflected; an edited or stale artifact reopens its drafting phase. From
// implementing onwards the stored phase is kept while the artifacts are approved.
func Effective(stored Phase, st map[Artifact]ArtifactStatus) Phase {
	return EffectiveFor(stored, st, false)
}

// EffectiveFor is Effective for a feature that may be a baseline: a baseline
// feature whose two specs are approved is baselined (it has no plan).
func EffectiveFor(stored Phase, st map[Artifact]ArtifactStatus, baseline bool) Phase {
	if baseline && st[Functional] == StatusApproved && st[Technical] == StatusApproved {
		return Baselined
	}
	var derived Phase
	switch {
	case st[Functional] != StatusApproved:
		derived = FunctionalDrafting
	case st[Technical] == StatusMissing:
		derived = FunctionalApproved
	case st[Technical] != StatusApproved:
		derived = TechnicalDrafting
	case st[Plan] == StatusMissing:
		derived = TechnicalApproved
	case st[Plan] != StatusApproved:
		derived = PlanDrafting
	default:
		derived = PlanApproved
	}
	if derived == PlanApproved && stored.index() > PlanApproved.index() {
		return stored
	}
	return derived
}
