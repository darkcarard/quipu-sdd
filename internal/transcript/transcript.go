// Package transcript records every provider request and response, and every
// tool call, of a run in .sdd/runs/<run-id>/transcript.jsonl
// (docs/TRACEABILITY.md §2). Every entry is redacted before it is written:
// configured secrets and anything matching a secret pattern are replaced.
package transcript

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/provider"
)

// Entry kinds.
const (
	KindRequest    = "request"
	KindResponse   = "response"
	KindError      = "error"
	KindRetry      = "retry"
	KindToolCall   = "tool_call"
	KindToolResult = "tool_result"
)

// Entry is one line of the transcript.
type Entry struct {
	Seq      int             `json:"seq"`
	TS       time.Time       `json:"ts"`
	RunID    string          `json:"run_id,omitempty"`
	Kind     string          `json:"kind"`
	Provider string          `json:"provider,omitempty"`
	Call     int             `json:"call,omitempty"` // pairs a request with its response or error
	Data     json.RawMessage `json:"data"`
}

// Redacted replaces every secret.
const Redacted = "[REDACTED]"

// DefaultPatterns match common credential formats.
var DefaultPatterns = []string{
	`sk-ant-[A-Za-z0-9_-]{8,}`,             // Anthropic keys
	`sk-[A-Za-z0-9_-]{20,}`,                // OpenAI-style keys
	`AIza[0-9A-Za-z_-]{35}`,                // Google API keys
	`AKIA[0-9A-Z]{16}`,                     // AWS access key IDs
	`gh[pousr]_[A-Za-z0-9]{36,}`,           // GitHub tokens
	`github_pat_[A-Za-z0-9_]{22,}`,         // GitHub fine-grained tokens
	`xox[abposr]-[A-Za-z0-9-]{10,}`,        // Slack tokens
	`(?i)bearer\s+[A-Za-z0-9._~+/=-]{20,}`, // bearer tokens
	`-----BEGIN [A-Z ]*PRIVATE KEY-----[^"]*?-----END [A-Z ]*PRIVATE KEY-----`, // PEM keys (JSON-escaped)
}

// Redactor removes secrets from text.
type Redactor struct {
	secrets  []string
	patterns []*regexp.Regexp
}

// MinSecretLen is the shortest literal secret that is redacted; shorter values
// would blank out ordinary words.
const MinSecretLen = 8

// NewRedactor builds a redactor from literal secrets and extra regular
// expressions (in addition to DefaultPatterns).
func NewRedactor(secrets []string, patterns []string) (*Redactor, error) {
	r := &Redactor{}
	seen := map[string]bool{}
	for _, s := range secrets {
		if len(s) >= MinSecretLen && !seen[s] {
			seen[s] = true
			r.secrets = append(r.secrets, s)
			// Also match the JSON-escaped form, since entries are redacted after encoding.
			if b, err := json.Marshal(s); err == nil {
				if esc := string(b[1 : len(b)-1]); esc != s && !seen[esc] {
					seen[esc] = true
					r.secrets = append(r.secrets, esc)
				}
			}
		}
	}
	// Longest first, so a secret containing another is replaced whole.
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
	for _, p := range append(append([]string(nil), DefaultPatterns...), patterns...) {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redaction pattern %q: %w", p, err)
		}
		r.patterns = append(r.patterns, re)
	}
	return r, nil
}

// Redact returns s with every secret replaced by Redacted.
func (r *Redactor) Redact(s string) string {
	if r == nil {
		return s
	}
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	for _, re := range r.patterns {
		s = re.ReplaceAllString(s, Redacted)
	}
	return s
}

// Writer appends redacted entries to a transcript file. It is safe for concurrent use.
type Writer struct {
	mu    sync.Mutex
	f     *os.File
	red   *Redactor
	runID string
	seq   int
	call  int
	now   func() time.Time
}

// Open creates (or appends to) the transcript at path.
func Open(path, runID string, red *Redactor) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f, red: red, runID: runID, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Close closes the file.
func (w *Writer) Close() error { return w.f.Close() }

// NextCall returns a new call number for pairing a request with its outcome.
func (w *Writer) NextCall() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.call++
	return w.call
}

// Record appends one entry. data is encoded to JSON and redacted.
func (w *Writer) Record(kind, providerName string, call int, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	redacted := json.RawMessage(w.red.Redact(string(raw)))
	if !json.Valid(redacted) { // a pattern cut through JSON syntax: keep a safe placeholder
		redacted, _ = json.Marshal(map[string]string{"redacted": "entry could not be redacted safely"})
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	line, err := json.Marshal(Entry{
		Seq: w.seq, TS: w.now(), RunID: w.runID, Kind: kind, Provider: providerName, Call: call, Data: redacted,
	})
	if err != nil {
		return err
	}
	_, err = w.f.Write(append(line, '\n'))
	return err
}

// errorData is how errors are recorded.
type errorData struct {
	Error     string `json:"error"`
	Retryable bool   `json:"retryable,omitempty"`
	Delay     string `json:"delay,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
}

// RecordRetry records a retry decision (use it as provider.RetryPolicy.OnRetry).
func (w *Writer) RecordRetry(providerName string) func(attempt int, err error, delay time.Duration) {
	return func(attempt int, err error, delay time.Duration) {
		_ = w.Record(KindRetry, providerName, 0, errorData{Error: err.Error(), Retryable: true, Delay: delay.String(), Attempt: attempt})
	}
}

// RecordToolCall records a tool call and returns its call number for RecordToolResult.
func (w *Writer) RecordToolCall(name string, input json.RawMessage) (int, error) {
	call := w.NextCall()
	return call, w.Record(KindToolCall, "", call, map[string]any{"name": name, "input": input})
}

// RecordToolResult records the outcome of a tool call.
func (w *Writer) RecordToolResult(call int, output string, isError bool) error {
	return w.Record(KindToolResult, "", call, map[string]any{"output": output, "is_error": isError})
}

type recording struct {
	provider.Provider
	w *Writer
}

// Recording wraps p so that every request, response and error is recorded.
// A failure to write the transcript never fails the call.
func Recording(p provider.Provider, w *Writer) provider.Provider {
	return &recording{Provider: p, w: w}
}

func (r *recording) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	call := r.w.NextCall()
	_ = r.w.Record(KindRequest, r.Name(), call, req)
	resp, err := r.Provider.Complete(ctx, req)
	r.outcome(call, &resp, err)
	return resp, err
}

func (r *recording) Stream(ctx context.Context, req provider.Request) (<-chan provider.StreamEvent, error) {
	call := r.w.NextCall()
	_ = r.w.Record(KindRequest, r.Name(), call, req)
	in, err := r.Provider.Stream(ctx, req)
	if err != nil {
		r.outcome(call, nil, err)
		return nil, err
	}
	out := make(chan provider.StreamEvent)
	go func() {
		defer close(out)
		for ev := range in {
			switch ev.Type {
			case provider.EventDone:
				r.outcome(call, ev.Response, nil)
			case provider.EventError:
				r.outcome(call, nil, ev.Err)
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (r *recording) outcome(call int, resp *provider.Response, err error) {
	if err != nil {
		_ = r.w.Record(KindError, r.Name(), call, errorData{Error: err.Error(), Retryable: provider.IsRetryable(err)})
		return
	}
	_ = r.w.Record(KindResponse, r.Name(), call, resp)
}
