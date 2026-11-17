package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/darkcarard/sdd-framework/internal/workflow"
)

func TestInitIsIdempotent(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	for i := 0; i < 2; i++ {
		if err := l.Init(); err != nil {
			t.Fatalf("Init #%d: %v", i+1, err)
		}
	}
	for _, d := range l.Dirs() {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("missing dir %s: %v", d, err)
		}
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.json")
	for _, content := range []string{"one", "two"} {
		if err := WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); string(b) != content {
			t.Fatalf("content = %q, want %q", b, content)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	if _, err := l.Load("signup"); !errors.Is(err, ErrNoFeature) {
		t.Fatalf("Load of unknown feature: %v", err)
	}

	s := NewFeatureState("signup")
	s.Phase = workflow.Implementing
	s.Current = Cursor{Group: "G2", Task: "T2.1"}
	s.Approvals = map[workflow.Artifact]workflow.Approval{
		workflow.Functional: {Hash: workflow.Hash([]byte("x")), By: "user:a@b.c"},
	}
	s.Groups = []GroupState{{ID: "G2", Status: workflow.GroupInProgress,
		Tasks: []TaskState{{ID: "T2.1", Status: workflow.TaskInterrupted}}}}
	if err := l.Save(s); err != nil {
		t.Fatal(err)
	}

	got, err := l.Load("signup")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != workflow.Implementing || got.Current.Task != "T2.1" ||
		got.Groups[0].Tasks[0].Status != workflow.TaskInterrupted ||
		got.Approvals[workflow.Functional].By != "user:a@b.c" || got.UpdatedAt.IsZero() {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
