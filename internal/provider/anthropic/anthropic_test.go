package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkcarard/sdd-framework/internal/provider"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata")

const testKey = "sk-ant-test-0123456789"

// captured is the last request a fake server received.
type captured struct {
	mu     sync.Mutex
	header http.Header
	body   []byte
	path   string
}

func (c *captured) get() (http.Header, []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header, c.body
}

// serve returns a client whose server answers every request with handler.
func serve(t *testing.T, handler func(w http.ResponseWriter, r *http.Request), opts ...Option) (*Client, *captured) {
	t.Helper()
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.mu.Lock()
		cap.header, cap.body, cap.path = r.Header.Clone(), b, r.URL.Path
		cap.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := New(testKey, append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c, cap
}

// fixture streams testdata/<name> as an SSE response.
func fixture(t *testing.T, name string) func(w http.ResponseWriter, r *http.Request) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.Header().Set("request-id", "req_test")
		_, _ = w.Write(b)
	}
}

func hello() provider.Request {
	return provider.Request{Messages: []provider.Message{provider.UserText("Hi")}}
}

// stream runs a request and returns the events and the final response.
func stream(t *testing.T, c *Client, req provider.Request) ([]provider.StreamEvent, provider.Response, error) {
	t.Helper()
	ch, err := c.Stream(context.Background(), req)
	if err != nil {
		return nil, provider.Response{}, err
	}
	var evs []provider.StreamEvent
	for ev := range ch {
		evs = append(evs, ev)
		switch ev.Type {
		case provider.EventDone:
			return evs, *ev.Response, nil
		case provider.EventError:
			return evs, provider.Response{}, ev.Err
		}
	}
	t.Fatal("stream closed without a final event")
	return nil, provider.Response{}, nil
}

func TestTextAndThinking(t *testing.T) {
	c, cap := serve(t, fixture(t, "text.sse"))
	evs, resp, err := stream(t, c, hello())
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "msg_01Text" || resp.Model != "claude-opus-5-5" || resp.StopReason != provider.StopEndTurn {
		t.Errorf("response = %+v", resp)
	}
	if got := resp.Text(); got != `Hello, world "quoted" ✓` {
		t.Errorf("text = %q", got)
	}
	want := provider.Usage{InputTokens: 25, OutputTokens: 42, CacheReadTokens: 1200}
	if resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
	if n := len(resp.Message.Content); n != 2 || resp.Message.Content[0].Type != provider.BlockThinking {
		t.Fatalf("content = %+v", resp.Message.Content)
	}
	if raw := string(resp.Message.Content[0].Raw); raw != `{"type":"thinking","thinking":"","signature":"EqQBCkYIBhgCKkA="}` {
		t.Errorf("thinking raw = %s", raw)
	}
	var deltas []string
	for _, ev := range evs {
		if ev.Type == provider.EventTextDelta {
			deltas = append(deltas, ev.Text)
		}
	}
	if strings.Join(deltas, "|") != `Hello|, world "quoted" ✓` {
		t.Errorf("deltas = %q", deltas)
	}

	h, _ := cap.get()
	for k, v := range map[string]string{
		"x-api-key": testKey, "anthropic-version": "2026-10-21", "content-type": "application/json",
		"accept": "text/event-stream", "anthropic-beta": "server-side-fallback-2026-07-01",
	} {
		if got := h.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if cap.path != "/v1/messages" {
		t.Errorf("path = %s", cap.path)
	}
}

func TestToolUse(t *testing.T) {
	c, _ := serve(t, fixture(t, "tool_use.sse"))
	evs, resp, err := stream(t, c, hello())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != provider.StopToolUse {
		t.Errorf("stop = %s", resp.StopReason)
	}
	uses := resp.ToolUses()
	if len(uses) != 1 || uses[0].ToolUseID != "toolu_01A" || uses[0].ToolName != "read_file" || uses[0].InvalidInput {
		t.Fatalf("tool uses = %+v", uses)
	}
	var in struct{ Path string }
	if err := json.Unmarshal(uses[0].Input, &in); err != nil || in.Path != "specs/user-signup/functional.md" {
		t.Errorf("input = %s (%v)", uses[0].Input, err)
	}
	if resp.Usage.CacheCreationTokens != 2048 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	var sawTool, sawThinking bool
	for _, ev := range evs {
		sawTool = sawTool || (ev.Type == provider.EventToolUse && ev.Block.ToolName == "read_file")
		sawThinking = sawThinking || (ev.Type == provider.EventThinkingDelta && ev.Text == "Reading the spec first.")
	}
	if !sawTool || !sawThinking {
		t.Errorf("events: tool_use %v, thinking %v", sawTool, sawThinking)
	}
	if got := resp.Message.Content[0].Text; got != "Reading the spec first." {
		t.Errorf("thinking text = %q", got)
	}
}

// TestReplayIsAppendOnly checks that an assistant turn is sent back byte for
// byte, which reasoning blocks require and which keeps the prompt cache warm.
func TestReplayIsAppendOnly(t *testing.T) {
	c, _ := serve(t, fixture(t, "tool_use.sse"))
	_, first, err := stream(t, c, hello())
	if err != nil {
		t.Fatal(err)
	}
	next := hello()
	next.Messages = append(next.Messages, first.Message, provider.Message{Role: provider.RoleUser, Content: []provider.ContentBlock{
		provider.ToolResult("toolu_01A", "# User Signup", false),
	}})
	body, _, err := c.buildRequest(next)
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Messages []struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Messages) != 3 {
		t.Fatalf("messages = %d", len(w.Messages))
	}
	for i, b := range first.Message.Content {
		if !bytes.Equal(w.Messages[1].Content[i], b.Raw) {
			t.Errorf("assistant block %d not replayed verbatim:\n got %s\nwant %s", i, w.Messages[1].Content[i], b.Raw)
		}
	}
	if want := `{"type":"tool_result","tool_use_id":"toolu_01A","content":"# User Signup"}`; string(w.Messages[2].Content[0]) != want {
		t.Errorf("tool_result = %s", w.Messages[2].Content[0])
	}
	// The same history must encode to the same bytes every time.
	again, _, _ := c.buildRequest(next)
	if !bytes.Equal(body, again) {
		t.Error("encoding is not deterministic")
	}
}

func TestRequestGolden(t *testing.T) {
	c, err := New(testKey)
	if err != nil {
		t.Fatal(err)
	}
	req := provider.Request{
		System: "You draft specs.",
		Tools: []provider.ToolDef{{
			Name: "read_file", Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
		Effort: provider.EffortXHigh,
		Messages: []provider.Message{
			provider.UserText("Read the spec"),
			{Role: provider.RoleAssistant, Content: []provider.ContentBlock{
				provider.Text("Reading."),
				{Type: provider.BlockToolUse, ToolUseID: "toolu_1", ToolName: "read_file", Input: json.RawMessage(`{"path":"a.md"}`)},
				{Type: provider.BlockThinking, Text: "from another provider", Raw: json.RawMessage(`{"x":1}`), RawProvider: "openai"},
			}},
			{Role: provider.RoleUser, Content: []provider.ContentBlock{
				provider.ToolResult("toolu_1", "no such file", true), provider.Text(""),
			}},
		},
	}
	body, betas, err := c.buildRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty.WriteByte('\n')
	golden := filepath.Join("testdata", "request.golden.json")
	if *update {
		if err := os.WriteFile(golden, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Errorf("request body differs from %s:\n%s", golden, pretty.String())
	}
	if strings.Join(betas, ",") != betaFallbacks {
		t.Errorf("betas = %v", betas)
	}
}

func TestRequestOptions(t *testing.T) {
	tool := provider.Request{
		Messages: hello().Messages,
		Tools:    []provider.ToolDef{{Name: "t"}},
	}
	tests := []struct {
		name  string
		opts  []Option
		req   provider.Request
		want  []string // substrings of the body
		avoid []string
		betas string
	}{
		{"defaults", nil, tool,
			[]string{`"model":"claude-opus-5-5"`, `"max_tokens":64000`, `"stream":true`, `"thinking":{"type":"adaptive"}`,
				`"output_config":{"effort":"high"}`, `"cache_control":{"type":"ephemeral"}`, `"fallbacks":"default"`,
				`"eager_input_streaming":true`, `"input_schema":{"type":"object","properties":{}}`},
			[]string{"temperature", "system"}, betaFallbacks},
		{"display updates", []Option{WithThinkingDisplay(DisplayUpdates)}, hello(),
			[]string{`"thinking":{"type":"adaptive","display":"updates"}`}, nil, betaFallbacks + "," + betaDisplayUpdates},
		{"no fallbacks", []Option{WithFallbacks(false)}, hello(), nil, []string{"fallbacks"}, ""},
		{"model without fallbacks", []Option{WithModel("claude-haiku-5-5")}, hello(),
			[]string{`"model":"claude-haiku-5-5"`}, []string{"fallbacks"}, ""},
		{"request overrides", nil, provider.Request{Model: "claude-sonnet-5-5", MaxTokens: 1000, Effort: "low", Messages: hello().Messages},
			[]string{`"model":"claude-sonnet-5-5"`, `"max_tokens":1000`, `"effort":"low"`}, nil, betaFallbacks},
		{"proxy disables eager tools", []Option{WithBaseURL("https://proxy.example")}, tool, nil, []string{"eager_input_streaming"}, betaFallbacks},
		{"eager forced on", []Option{WithBaseURL("https://proxy.example"), WithEagerToolStreaming(true)}, tool,
			[]string{`"eager_input_streaming":true`}, nil, betaFallbacks},
		{"no caching", []Option{WithPromptCaching(false)}, hello(), nil, []string{"cache_control"}, betaFallbacks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(testKey, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			body, betas, err := c.buildRequest(tt.req)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(string(body), w) {
					t.Errorf("body missing %s:\n%s", w, body)
				}
			}
			for _, a := range tt.avoid {
				if strings.Contains(string(body), a) {
					t.Errorf("body should not contain %s:\n%s", a, body)
				}
			}
			if got := strings.Join(betas, ","); got != tt.betas {
				t.Errorf("betas = %q, want %q", got, tt.betas)
			}
		})
	}

	c, _ := New(testKey)
	for name, req := range map[string]provider.Request{
		"no messages":            {},
		"empty message":          {Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.ContentBlock{provider.Text("")}}}},
		"tool_result from model": {Messages: []provider.Message{{Role: provider.RoleAssistant, Content: []provider.ContentBlock{provider.ToolResult("x", "y", false)}}}},
		"tool_use from user":     {Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.ContentBlock{{Type: provider.BlockToolUse, ToolUseID: "x"}}}}},
	} {
		if _, _, err := c.buildRequest(req); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := New(""); err == nil {
		t.Error("an empty key must be rejected")
	}
	if _, err := New(testKey, WithThinkingDisplay("verbose")); err == nil {
		t.Error("an unknown display must be rejected")
	}
}

func TestRefusal(t *testing.T) {
	c, _ := serve(t, fixture(t, "refusal.sse"))
	_, resp, err := stream(t, c, hello())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != provider.StopRefusal || resp.Refusal == nil || resp.Refusal.Category != "cyber" {
		t.Errorf("response = %+v refusal = %+v", resp, resp.Refusal)
	}
}

func TestMaxTokensMarksToolInputInvalid(t *testing.T) {
	c, _ := serve(t, fixture(t, "max_tokens_tool.sse"))
	resp, err := c.Complete(context.Background(), hello())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != provider.StopMaxTokens {
		t.Errorf("stop = %s", resp.StopReason)
	}
	use := resp.ToolUses()[0]
	if !use.InvalidInput {
		t.Fatal("truncated input must be marked invalid")
	}
	if !strings.Contains(string(resp.Message.Content[0].Raw), `"input":{}`) {
		t.Errorf("an invalid input is replayed as {}: %s", resp.Message.Content[0].Raw)
	}
	res := provider.InvalidInputResult(use)
	if !res.IsError || !strings.Contains(res.Output, `"INVALID_JSON":"{\"path\": \"a.go\"`) {
		t.Errorf("result = %+v", res)
	}
}

func TestFallbackDropsDeclinedAttempt(t *testing.T) {
	c, _ := serve(t, fixture(t, "fallback.sse"))
	resp, err := c.Complete(context.Background(), hello())
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, b := range resp.Message.Content {
		types = append(types, string(b.Type)+":"+blockType(b.Raw))
	}
	if got := strings.Join(types, " "); got != "text:text other:fallback text:text" {
		t.Errorf("blocks = %s", got)
	}
	if len(resp.ToolUses()) != 0 {
		t.Error("a tool call from the declined attempt must not be returned")
	}
	if resp.Text() != "Let me look at the file." {
		t.Errorf("text = %q", resp.Text())
	}
}

func TestHTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		headers    map[string]string
		body       string
		retryable  bool
		typ, msg   string
		retryAfter time.Duration
	}{
		{"invalid request", 400, nil, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: too large"},"request_id":"req_body"}`,
			false, "invalid_request_error", "max_tokens: too large", 0},
		{"auth", 401, nil, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
			false, "authentication_error", "invalid x-api-key", 0},
		{"rate limit", 429, map[string]string{"retry-after": "7"}, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
			true, "rate_limit_error", "slow down", 7 * time.Second},
		{"overloaded", 529, map[string]string{"retry-after-ms": "1500"}, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			true, "overloaded_error", "Overloaded", 1500 * time.Millisecond},
		{"server says do not retry", 500, map[string]string{"x-should-retry": "false"}, `{"type":"error","error":{"type":"api_error","message":"boom"}}`,
			false, "api_error", "boom", 0},
		{"not json", 502, nil, "<html>bad gateway</html>", true, "", "<html>bad gateway</html>", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("request-id", "req_hdr")
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := c.Stream(context.Background(), hello())
			var e *provider.APIError
			if !errors.As(err, &e) {
				t.Fatalf("err = %v", err)
			}
			if e.StatusCode != tt.status || e.Retryable != tt.retryable || e.Type != tt.typ || e.Message != tt.msg ||
				e.RetryAfter != tt.retryAfter || e.RequestID != "req_hdr" {
				t.Errorf("err = %+v", e)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Error("the API key must never appear in errors")
			}
		})
	}
}

func TestStreamFailures(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		check   func(t *testing.T, err error)
	}{
		{"error event", "error_midstream.sse", func(t *testing.T, err error) {
			var e *provider.APIError
			if !errors.As(err, &e) || e.Type != "overloaded_error" || e.StatusCode != 529 || !e.Retryable || e.RequestID != "req_test" {
				t.Errorf("err = %#v", err)
			}
		}},
		{"truncated", "truncated.sse", func(t *testing.T, err error) {
			if !errors.Is(err, io.ErrUnexpectedEOF) || !provider.IsRetryable(err) {
				t.Errorf("err = %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := serve(t, fixture(t, tt.fixture))
			_, _, err := stream(t, c, hello())
			tt.check(t, err)
		})
	}
}

func TestIdleTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: ping\ndata: {\"type\":\"ping\"}\n\n"))
		w.(http.Flusher).Flush()
		select { // then go silent
		case <-release:
		case <-r.Context().Done():
		}
	}, WithIdleTimeout(50*time.Millisecond))
	_, _, err := stream(t, c, hello())
	if !provider.IsRetryable(err) || !strings.Contains(err.Error(), "no data received for 50ms") {
		t.Errorf("err = %v", err)
	}
}

func TestCallerCancellation(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Stream(ctx, hello())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for ev := range ch { // the channel must close
		if ev.Type == provider.EventError && provider.IsRetryable(ev.Err) {
			t.Errorf("a cancelled request must not be retryable: %v", ev.Err)
		}
	}
}

func TestReadSSE(t *testing.T) {
	in := ": comment\r\nevent: a\r\ndata: one\r\ndata: two\r\n\r\n\nevent: b\ndata:{}\n\n"
	var got []string
	err := readSSE(strings.NewReader(in), func(e sseEvent) (bool, error) {
		got = append(got, e.name+"="+e.data)
		return e.name != "b", nil
	})
	if err != nil || strings.Join(got, ";") != "a=one\ntwo;b={}" {
		t.Errorf("got %q, %v", got, err)
	}
}
