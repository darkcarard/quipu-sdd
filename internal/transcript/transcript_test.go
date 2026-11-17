package transcript

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/provider"
)

func TestRedact(t *testing.T) {
	r, err := NewRedactor([]string{"hunter2-secret", `pa"ss\word-123`, "short"}, []string{`internal-[0-9]{4}`})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ in, want string }{
		{"key hunter2-secret here", "key [REDACTED] here"},
		{"x-api-key: sk-ant-api03-AbCdEf0123456789", "x-api-key: [REDACTED]"},
		{"Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345", "Authorization: [REDACTED]"},
		{"aws AKIAABCDEFGHIJKLMNOP done", "aws [REDACTED] done"},
		{"gh ghp_" + strings.Repeat("a", 36), "gh [REDACTED]"},
		{"ticket internal-1234", "ticket [REDACTED]"},
		{"short words stay short", "short words stay short"},
		{`-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----`, "[REDACTED]"},
	}
	for _, tt := range tests {
		if got := r.Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// A secret with JSON special characters is also found in its encoded form.
	b, _ := json.Marshal(map[string]string{"v": `pa"ss\word-123`})
	if got := r.Redact(string(b)); got != `{"v":"[REDACTED]"}` {
		t.Errorf("escaped secret: %s", got)
	}
	if _, err := NewRedactor(nil, []string{"("}); err == nil {
		t.Error("an invalid pattern must be rejected")
	}
}

type stub struct {
	resp provider.Response
	err  error
}

func (s stub) Name() string { return "stub" }
func (s stub) Complete(context.Context, provider.Request) (provider.Response, error) {
	return s.resp, s.err
}
func (s stub) Stream(ctx context.Context, req provider.Request) (<-chan provider.StreamEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: provider.EventTextDelta, Text: "x"}
	ch <- provider.StreamEvent{Type: provider.EventDone, Response: &s.resp}
	close(ch)
	return ch, nil
}

func readEntries(t *testing.T, path string) []Entry {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Entry
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

func TestRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs", "r1", "transcript.jsonl")
	red, _ := NewRedactor([]string{"top-secret-value"}, nil)
	w, err := Open(path, "r1", red)
	if err != nil {
		t.Fatal(err)
	}
	w.now = func() time.Time { return time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC) }

	ok := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentBlock{provider.Text("done")}}}
	req := provider.Request{Messages: []provider.Message{provider.UserText("use top-secret-value please")}}
	if _, err := Recording(stub{resp: ok}, w).Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	ch, err := Recording(stub{resp: ok}, w).Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	failErr := &provider.APIError{Message: "overloaded", Retryable: true}
	if _, err := Recording(stub{err: failErr}, w).Complete(context.Background(), req); !errors.Is(err, failErr) {
		t.Fatal(err)
	}
	w.RecordRetry("stub")(1, failErr, 2*time.Second)
	call, _ := w.RecordToolCall("read_file", json.RawMessage(`{"path":"a.md"}`))
	_ = w.RecordToolResult(call, "contents", false)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	es := readEntries(t, path)
	var kinds []string
	for i, e := range es {
		kinds = append(kinds, e.Kind)
		if e.Seq != i+1 || e.RunID != "r1" {
			t.Errorf("entry %d = %+v", i, e)
		}
	}
	if got := strings.Join(kinds, ","); got != "request,response,request,response,request,error,retry,tool_call,tool_result" {
		t.Errorf("kinds = %s", got)
	}
	if es[0].Call != es[1].Call || es[0].Call == es[2].Call || es[7].Call != es[8].Call {
		t.Error("requests must be paired with their outcome by call number")
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "top-secret-value") || !strings.Contains(string(b), Redacted) {
		t.Error("secrets must be redacted")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("transcript mode = %v", fi.Mode().Perm())
	}
}
