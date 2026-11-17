package workspace

import "testing"

func TestCommitMessage(t *testing.T) {
	got := CommitMessage("feat(signup): G2 Signup service", Trailers{
		Feature: "signup", Group: "G2", Tasks: []string{"T2.1", "T2.2"},
		Covers: []string{"FR-001", "AC-001"}, RunID: "r1",
	})
	want := "feat(signup): G2 Signup service\n\n" +
		"SDD-Feature: signup\nSDD-Group: G2\nSDD-Tasks: T2.1, T2.2\nSDD-Covers: FR-001, AC-001\nSDD-Run: r1\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if s := (Trailers{Group: "G1"}).String(); s != "SDD-Group: G1\n" {
		t.Fatalf("empty trailers should be omitted, got %q", s)
	}
}
