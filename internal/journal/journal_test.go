package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendReadAndResumeSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(Event{Actor: "system", Action: "feature.created", Status: Info}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(Event{Action: "x"}); err == nil {
		t.Fatal("missing fields should be rejected")
	}

	// A new process reopens the journal and continues the sequence.
	j2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := j2.Append(Event{Actor: "user:a@b.c", Action: "spec.approved", Status: Info})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 2 {
		t.Fatalf("seq = %d, want 2", ev.Seq)
	}
	events, err := Read(path)
	if err != nil || len(events) != 2 || events[1].Action != "spec.approved" {
		t.Fatalf("Read = %+v, %v", events, err)
	}
}

func TestInterruptedDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, _ := Open(path)
	agent := Event{Actor: "agent:claude-opus-5-5", Phase: "implement", RunID: "r1"}

	a := agent
	a.Action, a.IDs = "task.run", []string{"T1.1"}
	s1, _ := j.Start(a)
	if _, err := j.End(s1, Completed, Event{}); err != nil {
		t.Fatal(err)
	}
	b := agent
	b.Action, b.IDs = "task.run", []string{"T1.2"}
	s2, _ := j.Start(b) // process "crashes" here

	if _, err := j.End(s2, "bogus", Event{}); err == nil {
		t.Fatal("invalid end status should be rejected")
	}

	events, _ := Read(path)
	got := Interrupted(events)
	if len(got) != 1 || got[0].Seq != s2.Seq || got[0].IDs[0] != "T1.2" {
		t.Fatalf("Interrupted = %+v", got)
	}
	if events[1].Ref != s1.Seq || events[1].Action != "task.run" || events[1].RunID != "r1" {
		t.Fatalf("End should inherit fields and reference start: %+v", events[1])
	}
}

func TestReadToleratesTornLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, _ := Open(path)
	j.Append(Event{Actor: "system", Action: "a", Status: Info})
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"seq":2,"act`)
	f.Close()

	events, err := Read(path)
	if err != nil || len(events) != 1 {
		t.Fatalf("Read = %d events, %v", len(events), err)
	}

	// Corruption in the middle is an error.
	os.WriteFile(path, []byte("garbage\n{\"seq\":1,\"actor\":\"s\",\"action\":\"a\",\"status\":\"info\"}\n"), 0o644)
	if _, err := Read(path); err == nil {
		t.Fatal("expected error for corrupted middle line")
	}
}

func TestRenderLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, _ := Open(path)
	s, _ := j.Start(Event{Actor: "agent:m", Action: "task.run", IDs: []string{"T1.1"}, Detail: "a|b"})
	j.End(s, Failed, Event{Detail: "build failed"})
	events, _ := Read(path)
	out := RenderLog("signup", events)
	for _, want := range []string{"# Journal: signup", "task.run", "failed (#1)", `a\|b`, "T1.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}
