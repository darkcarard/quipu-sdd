package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/provider"
	"github.com/darkcarard/quipu-sdd/internal/transcript"
)

const key = "sk-ant-registry-test-key-0123456789"

func loadConfig(t *testing.T, yaml string) config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sdd.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNewErrors(t *testing.T) {
	t.Setenv("SDD_TEST_NO_KEY", "")
	cfg := loadConfig(t, "provider:\n  name: anthropic\n  model: m\n  api_key_env: SDD_TEST_NO_KEY\n")
	if _, err := New(cfg, Options{}); err == nil || !strings.Contains(err.Error(), "SDD_TEST_NO_KEY is not set") {
		t.Errorf("missing key: %v", err)
	}
	t.Setenv("SDD_TEST_KEY", key)
	cfg = loadConfig(t, "provider:\n  name: gemini\n  model: m\n  api_key_env: SDD_TEST_KEY\n")
	if _, err := New(cfg, Options{}); err == nil || !strings.Contains(err.Error(), "M7") {
		t.Errorf("gemini: %v", err)
	}
}

// TestEndToEnd drives the decorated provider against a fake API that is
// overloaded once: the retry is recorded, the key never reaches the transcript,
// and the meter counts the completed turn.
func TestEndToEnd(t *testing.T) {
	var calls atomic.Int32
	sse, err := os.ReadFile(filepath.Join("..", "anthropic", "testdata", "text.sse"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != key {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write(sse)
	}))
	defer srv.Close()

	t.Setenv("SDD_TEST_KEY", key)
	cfg := loadConfig(t, "provider:\n  name: anthropic\n  model: claude-opus-5-5\n  api_key_env: SDD_TEST_KEY\n  base_url: "+srv.URL+"\n")
	red, err := Redactor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tw, err := transcript.Open(path, "run-1", red)
	if err != nil {
		t.Fatal(err)
	}
	meter := &provider.Meter{}
	retry := provider.DefaultRetryPolicy(3)
	retry.Sleep = func(context.Context, time.Duration) error { return nil }
	p, err := New(cfg, Options{Transcript: tw, Meter: meter, Retry: &retry})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Complete(context.Background(), provider.Request{Messages: []provider.Message{
		provider.UserText("my key is " + key),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text() == "" || calls.Load() != 2 {
		t.Errorf("text = %q, calls = %d", resp.Text(), calls.Load())
	}
	if u, n := meter.Usage(); n != 1 || u.OutputTokens != 42 {
		t.Errorf("meter = %+v (%d)", u, n)
	}
	_ = tw.Close()
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Contains(s, key) {
		t.Error("the API key leaked into the transcript")
	}
	for _, kind := range []string{`"kind":"request"`, `"kind":"error"`, `"kind":"retry"`, `"kind":"response"`} {
		if !strings.Contains(s, kind) {
			t.Errorf("transcript missing %s:\n%s", kind, s)
		}
	}
}
