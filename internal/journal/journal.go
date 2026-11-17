// Package journal implements the append-only, per-feature audit log
// (.sdd/features/<feature>/journal.jsonl). See docs/TRACEABILITY.md §2–3.
//
// Actions are recorded write-ahead: Start appends a "started" event before the
// action runs and End appends a "completed"/"failed" event referencing it. A
// started event with no matching end marks an interrupted action.
package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

// Status values.
const (
	Started   = "started"
	Completed = "completed"
	Failed    = "failed"
	Info      = "info" // standalone event with no start/end pairing
)

// Usage records LLM token consumption attributable to an event.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Event is one line of the journal.
type Event struct {
	Seq    int               `json:"seq"`
	TS     time.Time         `json:"ts"`
	Ref    int               `json:"ref,omitempty"` // for completed/failed: Seq of the started event
	RunID  string            `json:"run_id,omitempty"`
	Actor  string            `json:"actor"` // user:<email> | agent:<model> | system
	Phase  string            `json:"phase,omitempty"`
	Action string            `json:"action"` // e.g. spec.approved, task.run, group.committed
	Status string            `json:"status"`
	IDs    []string          `json:"ids,omitempty"`
	Hashes map[string]string `json:"hashes,omitempty"`
	Usage  *Usage            `json:"usage,omitempty"`
	Detail string            `json:"detail,omitempty"`
}

// Journal appends events to a JSONL file. It is safe for concurrent use within
// one process; cross-process exclusion is provided by the feature lock.
type Journal struct {
	mu   sync.Mutex
	path string
	next int
	now  func() time.Time
}

// Open opens (or prepares to create) the journal at path and resumes its sequence.
func Open(path string) (*Journal, error) {
	events, err := Read(path)
	if err != nil {
		return nil, err
	}
	next := 1
	if n := len(events); n > 0 {
		next = events[n-1].Seq + 1
	}
	return &Journal{path: path, next: next, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Append writes ev, assigning Seq and TS, and fsyncs before returning.
func (j *Journal) Append(ev Event) (Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if ev.Action == "" || ev.Status == "" || ev.Actor == "" {
		return ev, errors.New("journal: action, status and actor are required")
	}
	ev.Seq = j.next
	if ev.TS.IsZero() {
		ev.TS = j.now()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return ev, err
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return ev, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return ev, err
	}
	if err := f.Sync(); err != nil {
		return ev, err
	}
	j.next++
	return ev, nil
}

// Start records the beginning of an action and returns its event (use its Seq with End).
func (j *Journal) Start(ev Event) (Event, error) {
	ev.Status = Started
	ev.Ref = 0
	return j.Append(ev)
}

// End records the outcome of a started action. status must be Completed or Failed.
func (j *Journal) End(started Event, status string, ev Event) (Event, error) {
	if status != Completed && status != Failed {
		return ev, fmt.Errorf("journal: invalid end status %q", status)
	}
	ev.Status = status
	ev.Ref = started.Seq
	if ev.Action == "" {
		ev.Action = started.Action
	}
	if ev.Actor == "" {
		ev.Actor = started.Actor
	}
	if ev.RunID == "" {
		ev.RunID = started.RunID
	}
	if ev.Phase == "" {
		ev.Phase = started.Phase
	}
	return j.Append(ev)
}

// Read returns all events in path. A missing file yields no events. A truncated
// final line (crash mid-write) is ignored; corruption elsewhere is an error.
func Read(path string) ([]Event, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var events []Event
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var lines []string
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			if i == len(lines)-1 && !bytes.HasSuffix(b, []byte("\n")) {
				break // torn last write
			}
			return nil, fmt.Errorf("%s: line %d: %w", path, i+1, err)
		}
		events = append(events, ev)
	}
	return events, nil
}

// Interrupted returns started events that have no matching completed/failed event.
func Interrupted(events []Event) []Event {
	ended := map[int]bool{}
	for _, ev := range events {
		if ev.Ref != 0 {
			ended[ev.Ref] = true
		}
	}
	var out []Event
	for _, ev := range events {
		if ev.Status == Started && !ended[ev.Seq] {
			out = append(out, ev)
		}
	}
	return out
}

// RenderLog renders events as the human-readable log.md.
func RenderLog(feature string, events []Event) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Journal: %s\n\n", feature)
	sb.WriteString("<!-- Generated from journal.jsonl. Do not edit. -->\n\n")
	sb.WriteString("| # | Time (UTC) | Actor | Phase | Action | Status | IDs | Detail |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, ev := range events {
		status := ev.Status
		if ev.Ref != 0 {
			status = fmt.Sprintf("%s (#%d)", ev.Status, ev.Ref)
		}
		fmt.Fprintf(&sb, "| %d | %s | %s | %s | %s | %s | %s | %s |\n",
			ev.Seq, ev.TS.Format(time.RFC3339), cell(ev.Actor), cell(ev.Phase), cell(ev.Action),
			status, cell(strings.Join(ev.IDs, ", ")), cell(ev.Detail))
	}
	return sb.String()
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}
