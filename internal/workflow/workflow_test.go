package workflow

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from, to Phase
		want     bool
	}{
		{FunctionalDrafting, FunctionalApproved, true},
		{FunctionalApproved, TechnicalDrafting, true},
		{TechnicalApproved, PlanDrafting, true},
		{Implemented, Validated, true},
		// cannot skip gates
		{FunctionalDrafting, TechnicalDrafting, false},
		{FunctionalApproved, PlanDrafting, false},
		{PlanDrafting, Implementing, false},
		// reopen earlier drafting phases (edited or stale artifacts)
		{Implementing, FunctionalDrafting, true},
		{PlanApproved, TechnicalDrafting, true},
		{Validated, PlanDrafting, true},
		// cannot jump back to an approved phase
		{Implementing, TechnicalApproved, false},
		// self and unknown
		{PlanDrafting, PlanDrafting, false},
		{"bogus", FunctionalApproved, false},
	}
	for _, tt := range tests {
		if got := CanTransition(tt.from, tt.to); got != tt.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
	if err := Transition(FunctionalDrafting, Validated); err == nil {
		t.Error("Transition should return an error for an invalid move")
	}
}

func TestGroupAndTaskTransitions(t *testing.T) {
	if !CanTransitionGroup(GroupAwaitingReview, GroupInProgress) {
		t.Error("revise should reopen a group under review")
	}
	if CanTransitionGroup(GroupPending, GroupApproved) {
		t.Error("a group cannot be approved without running")
	}
	if !CanTransitionTask(TaskInterrupted, TaskInProgress) {
		t.Error("an interrupted task must be resumable")
	}
	if CanTransitionTask(TaskPending, TaskDone) {
		t.Error("a task cannot finish without starting")
	}
}

func TestEvaluate(t *testing.T) {
	f1, f2 := Hash([]byte("functional v1")), Hash([]byte("functional v2"))
	t1, t2 := Hash([]byte("technical v1")), Hash([]byte("technical v2"))
	p1 := Hash([]byte("plan v1"))

	approvals := map[Artifact]Approval{
		Functional: {Hash: f1},
		Technical:  {Hash: t1, SourceHashes: map[Artifact]string{Functional: f1}},
		Plan:       {Hash: p1, SourceHashes: map[Artifact]string{Functional: f1, Technical: t1}},
	}

	tests := []struct {
		name    string
		appr    map[Artifact]Approval
		current map[Artifact]string
		want    map[Artifact]ArtifactStatus
	}{
		{"all in sync", approvals,
			map[Artifact]string{Functional: f1, Technical: t1, Plan: p1},
			map[Artifact]ArtifactStatus{Functional: StatusApproved, Technical: StatusApproved, Plan: StatusApproved}},
		{"functional edited makes downstream stale", approvals,
			map[Artifact]string{Functional: f2, Technical: t1, Plan: p1},
			map[Artifact]ArtifactStatus{Functional: StatusModified, Technical: StatusStale, Plan: StatusStale}},
		{"technical edited makes plan stale", approvals,
			map[Artifact]string{Functional: f1, Technical: t2, Plan: p1},
			map[Artifact]ArtifactStatus{Functional: StatusApproved, Technical: StatusModified, Plan: StatusStale}},
		{"drafts and missing", map[Artifact]Approval{},
			map[Artifact]string{Functional: f1},
			map[Artifact]ArtifactStatus{Functional: StatusDraft, Technical: StatusMissing, Plan: StatusMissing}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.appr, tt.current)
			for a, want := range tt.want {
				if got[a] != want {
					t.Errorf("%s = %s, want %s", a, got[a], want)
				}
			}
		})
	}
}
