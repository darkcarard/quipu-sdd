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

func TestHashNormalizesLineEndings(t *testing.T) {
	if Hash([]byte("a\r\nb\r\n")) != Hash([]byte("a\nb\n")) {
		t.Error("CRLF and LF content must hash the same")
	}
	if Hash([]byte("a")) == Hash([]byte("b")) {
		t.Error("different content must hash differently")
	}
}

func TestEffective(t *testing.T) {
	const (
		ap = StatusApproved
		dr = StatusDraft
		mi = StatusMissing
		mo = StatusModified
		st = StatusStale
	)
	tests := []struct {
		name    string
		stored  Phase
		f, t, p ArtifactStatus
		want    Phase
	}{
		{"new feature", FunctionalDrafting, mi, mi, mi, FunctionalDrafting},
		{"functional draft", FunctionalDrafting, dr, mi, mi, FunctionalDrafting},
		{"functional approved", FunctionalDrafting, ap, mi, mi, FunctionalApproved},
		{"technical created", FunctionalApproved, ap, dr, mi, TechnicalDrafting},
		{"technical stale", PlanApproved, ap, st, st, TechnicalDrafting},
		{"technical approved", TechnicalDrafting, ap, ap, mi, TechnicalApproved},
		{"plan drafted", TechnicalApproved, ap, ap, dr, PlanDrafting},
		{"plan approved", PlanDrafting, ap, ap, ap, PlanApproved},
		{"implementing kept", Implementing, ap, ap, ap, Implementing},
		{"edit reopens functional", Implementing, mo, st, st, FunctionalDrafting},
		{"edited plan reopens plan", Implemented, ap, ap, mo, PlanDrafting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Effective(tt.stored, map[Artifact]ArtifactStatus{Functional: tt.f, Technical: tt.t, Plan: tt.p})
			if got != tt.want {
				t.Errorf("Effective = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEffectiveBaseline(t *testing.T) {
	ap, dr, mi, mo := StatusApproved, StatusDraft, StatusMissing, StatusModified
	tests := []struct {
		name    string
		f, t, p ArtifactStatus
		want    Phase
	}{
		{"documenting behavior", dr, mi, mi, FunctionalDrafting},
		{"behavior approved", ap, mi, mi, FunctionalApproved},
		{"documenting design", ap, dr, mi, TechnicalDrafting},
		{"both approved", ap, ap, mi, Baselined},
		{"revised baseline reopens", mo, ap, mi, FunctionalDrafting},
	}
	for _, tt := range tests {
		got := EffectiveFor(Baselined, map[Artifact]ArtifactStatus{Functional: tt.f, Technical: tt.t, Plan: tt.p}, true)
		if got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
	if got := EffectiveFor(TechnicalApproved, map[Artifact]ArtifactStatus{Functional: ap, Technical: ap, Plan: mi}, false); got != TechnicalApproved {
		t.Errorf("a regular feature is not baselined: %s", got)
	}
}
