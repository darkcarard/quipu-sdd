// Package anthropic adapts the neutral provider contract to the Anthropic
// Messages API using plain net/http (no vendor SDK).
//
// Every request is streamed (SSE): Complete collects the stream. Long turns
// (thinking counts toward max_tokens) therefore never hit an HTTP timeout;
// instead the connection is abandoned when no data arrives for the idle timeout.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/provider"
)

// DefaultBaseURL is the Anthropic API endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// APIVersion is sent as the anthropic-version header. Since 2026-10-21 the
// sampling parameters (temperature, top_p, top_k) are rejected, so the neutral
// request does not have them.
const APIVersion = "2026-10-21"

// Defaults used when neither the request nor an option sets a value.
const (
	DefaultModel       = "claude-opus-5-5"
	DefaultMaxTokens   = 64000 // thinking counts toward max_tokens; long agentic turns need room
	DefaultEffort      = provider.EffortHigh
	DefaultIdleTimeout = 5 * time.Minute
)

// Beta headers.
const (
	betaFallbacks      = "server-side-fallback-2026-07-01"
	betaDisplayUpdates = "thinking-display-updates-2026-08-18"
)

// fallbackModels accept the server-side `fallbacks` parameter: a request the
// model declines (stop_reason "refusal") is re-served by a fallback model in
// the same call instead of failing the turn.
var fallbackModels = []string{"claude-fable-5-2", "claude-fable-5-1", "claude-opus-5-5", "claude-opus-5", "claude-sonnet-5-5"}

// Thinking display values (thinking.display).
const (
	DisplayOmitted    = "omitted"    // API default: thinking blocks come back with empty text
	DisplaySummarized = "summarized" // a readable summary of the reasoning
	DisplayUpdates    = "updates"    // reasoning hidden, short progress notes between tool calls (beta)
)

// Client is the Anthropic adapter.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client

	model       string
	maxTokens   int
	effort      string
	display     string
	idleTimeout time.Duration
	fallbacks   bool
	caching     bool
	eagerTools  *bool // nil: on only against the official endpoint
}

// Option customizes a Client.
type Option func(*Client)

// WithBaseURL overrides the API endpoint (a proxy, or a test server).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient overrides the HTTP client. Do not set a Timeout on it: streams
// are bounded by the idle timeout instead.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithModel sets the default model.
func WithModel(m string) Option { return func(c *Client) { c.model = m } }

// WithMaxTokens sets the default max_tokens.
func WithMaxTokens(n int) Option { return func(c *Client) { c.maxTokens = n } }

// WithEffort sets the default effort (low, medium, high, xhigh, max).
func WithEffort(e string) Option { return func(c *Client) { c.effort = e } }

// WithThinkingDisplay sets thinking.display (omitted, summarized or updates).
func WithThinkingDisplay(d string) Option { return func(c *Client) { c.display = d } }

// WithIdleTimeout abandons a request when no data arrives for d.
func WithIdleTimeout(d time.Duration) Option { return func(c *Client) { c.idleTimeout = d } }

// WithFallbacks enables or disables server-side refusal fallbacks (on by default
// for the models that support them).
func WithFallbacks(on bool) Option { return func(c *Client) { c.fallbacks = on } }

// WithPromptCaching enables or disables automatic prompt caching (on by default).
func WithPromptCaching(on bool) Option { return func(c *Client) { c.caching = on } }

// WithEagerToolStreaming streams tool inputs as they are generated instead of
// after the API buffers and validates them. Default: on against the official
// endpoint only, since proxies may reject the field.
func WithEagerToolStreaming(on bool) Option { return func(c *Client) { c.eagerTools = &on } }

// New returns a client. The API key is held in memory only and never logged.
func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic: API key is required")
	}
	c := &Client{
		apiKey: apiKey, baseURL: DefaultBaseURL, http: &http.Client{},
		model: DefaultModel, maxTokens: DefaultMaxTokens, effort: DefaultEffort,
		idleTimeout: DefaultIdleTimeout, fallbacks: true, caching: true,
	}
	for _, o := range opts {
		o(c)
	}
	switch c.display {
	case "", DisplayOmitted, DisplaySummarized, DisplayUpdates:
	default:
		return nil, fmt.Errorf("anthropic: unknown thinking display %q (use omitted, summarized or updates)", c.display)
	}
	return c, nil
}

var _ provider.Provider = (*Client)(nil)

// Name implements provider.Provider.
func (c *Client) Name() string { return "anthropic" }

// Complete implements provider.Provider by collecting a stream.
func (c *Client) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	ch, err := c.Stream(ctx, req)
	if err != nil {
		return provider.Response{}, err
	}
	return provider.Collect(ch)
}

// Stream implements provider.Provider.
func (c *Client) Stream(ctx context.Context, req provider.Request) (<-chan provider.StreamEvent, error) {
	body, betas, err := c.buildRequest(req)
	if err != nil {
		return nil, err
	}
	caller := ctx
	ctx, cancel := context.WithCancelCause(ctx)
	idle := newIdleTimer(c.idleTimeout, cancel)

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		idle.stop()
		cancel(nil)
		return nil, err
	}
	hreq.Header.Set("content-type", "application/json")
	hreq.Header.Set("accept", "text/event-stream")
	hreq.Header.Set("x-api-key", c.apiKey)
	hreq.Header.Set("anthropic-version", APIVersion)
	if len(betas) > 0 {
		hreq.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}

	resp, err := c.http.Do(hreq)
	if err != nil {
		idle.stop()
		err = c.transportError(ctx, err)
		cancel(nil)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		idle.stop()
		defer cancel(nil)
		defer resp.Body.Close()
		return nil, c.httpError(resp)
	}

	out := make(chan provider.StreamEvent, 16)
	go func() {
		defer close(out)
		defer cancel(nil)
		defer idle.stop()
		defer resp.Body.Close()
		r := &idleReader{r: resp.Body, t: idle}
		ev := c.readStream(ctx, r, out, resp.Header.Get("request-id"))
		if ev.Type == provider.EventError {
			ev.Err = c.transportError(ctx, ev.Err)
		}
		select { // the request context may be cancelled by the idle timer: use the caller's
		case out <- ev:
		case <-caller.Done():
		}
	}()
	return out, nil
}

// errIdle is the cancellation cause when the idle timer fires.
var errIdle = errors.New("idle timeout")

// transportError classifies an error from the connection or the stream body.
func (c *Client) transportError(ctx context.Context, err error) error {
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return err
	}
	if cause := context.Cause(ctx); errors.Is(cause, errIdle) {
		return &provider.APIError{Provider: c.Name(), Retryable: true, Err: err,
			Message: fmt.Sprintf("no data received for %s", c.idleTimeout)}
	}
	if ctx.Err() != nil {
		return ctx.Err() // cancelled by the caller: not retryable
	}
	return &provider.APIError{Provider: c.Name(), Retryable: true, Err: err, Message: err.Error()}
}

// httpError decodes an error response.
func (c *Client) httpError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	e := &provider.APIError{
		Provider: c.Name(), StatusCode: resp.StatusCode, RequestID: resp.Header.Get("request-id"),
		Retryable: provider.RetryableStatus(resp.StatusCode), RetryAfter: retryAfter(resp.Header),
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(b, &body) == nil && body.Error.Message != "" {
		e.Type, e.Message = body.Error.Type, body.Error.Message
		if e.RequestID == "" {
			e.RequestID = body.RequestID
		}
	} else {
		e.Message = strings.TrimSpace(string(b))
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
	}
	switch strings.ToLower(resp.Header.Get("x-should-retry")) {
	case "true":
		e.Retryable = true
	case "false":
		e.Retryable = false
	}
	return e
}

// retryAfter parses retry-after-ms or retry-after (seconds or an HTTP date).
func retryAfter(h http.Header) time.Duration {
	if ms, err := strconv.ParseFloat(h.Get("retry-after-ms"), 64); err == nil && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}
	v := h.Get("retry-after")
	if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// --- request body ---------------------------------------------------------

type wireRequest struct {
	Model        string            `json:"model"`
	MaxTokens    int               `json:"max_tokens"`
	Stream       bool              `json:"stream"`
	System       string            `json:"system,omitempty"`
	Messages     []wireMessage     `json:"messages"`
	Tools        []wireTool        `json:"tools,omitempty"`
	Thinking     wireThinking      `json:"thinking"`
	OutputConfig *wireOutputConfig `json:"output_config,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
	Fallbacks    string            `json:"fallbacks,omitempty"`
}

type wireMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type wireTool struct {
	Name                string          `json:"name"`
	Description         string          `json:"description,omitempty"`
	InputSchema         json.RawMessage `json:"input_schema"`
	EagerInputStreaming bool            `json:"eager_input_streaming,omitempty"`
}

type wireThinking struct {
	Type    string `json:"type"`
	Display string `json:"display,omitempty"`
}

type wireOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type wireCacheControl struct {
	Type string `json:"type"`
}

// buildRequest returns the JSON body and the beta headers for req.
func (c *Client) buildRequest(req provider.Request) ([]byte, []string, error) {
	if len(req.Messages) == 0 {
		return nil, nil, errors.New("anthropic: a request needs at least one message")
	}
	w := wireRequest{
		Model: or(req.Model, c.model), MaxTokens: req.MaxTokens, Stream: true, System: req.System,
		Thinking: wireThinking{Type: "adaptive", Display: c.display},
	}
	if w.MaxTokens == 0 {
		w.MaxTokens = c.maxTokens
	}
	if e := or(req.Effort, c.effort); e != "" {
		w.OutputConfig = &wireOutputConfig{Effort: e}
	}
	if c.caching {
		w.CacheControl = &wireCacheControl{Type: "ephemeral"}
	}
	var betas []string
	if c.fallbacks && slices.Contains(fallbackModels, w.Model) {
		w.Fallbacks = "default"
		betas = append(betas, betaFallbacks)
	}
	if c.display == DisplayUpdates {
		betas = append(betas, betaDisplayUpdates)
	}
	eager := c.baseURL == DefaultBaseURL
	if c.eagerTools != nil {
		eager = *c.eagerTools
	}
	for _, t := range req.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		w.Tools = append(w.Tools, wireTool{Name: t.Name, Description: t.Description, InputSchema: schema, EagerInputStreaming: eager})
	}
	for i, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role)}
		for _, b := range m.Content {
			raw, err := c.encodeBlock(m.Role, b)
			if err != nil {
				return nil, nil, fmt.Errorf("anthropic: messages[%d]: %w", i, err)
			}
			if raw != nil {
				wm.Content = append(wm.Content, raw)
			}
		}
		if len(wm.Content) == 0 {
			return nil, nil, fmt.Errorf("anthropic: messages[%d] has no content", i)
		}
		w.Messages = append(w.Messages, wm)
	}
	body, err := json.Marshal(w)
	return body, betas, err
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// encodeBlock returns the wire form of a block, or nil to leave it out.
func (c *Client) encodeBlock(role provider.Role, b provider.ContentBlock) (json.RawMessage, error) {
	if len(b.Raw) > 0 && b.RawProvider == c.Name() {
		return b.Raw, nil // replay the provider's own block byte for byte
	}
	switch b.Type {
	case provider.BlockText:
		if b.Text == "" {
			return nil, nil // the API rejects empty text blocks
		}
		return json.Marshal(map[string]string{"type": "text", "text": b.Text})
	case provider.BlockToolUse:
		if role != provider.RoleAssistant {
			return nil, errors.New("tool_use blocks belong to assistant messages")
		}
		input := b.Input
		if b.InvalidInput || len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		return json.Marshal(struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}{"tool_use", b.ToolUseID, b.ToolName, input})
	case provider.BlockToolResult:
		if role != provider.RoleUser {
			return nil, errors.New("tool_result blocks belong to user messages")
		}
		return json.Marshal(struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   string `json:"content"`
			IsError   bool   `json:"is_error,omitempty"`
		}{"tool_result", b.ToolUseID, b.Output, b.IsError})
	case provider.BlockThinking, provider.BlockOther:
		return nil, nil // another provider's internal blocks cannot be replayed here
	}
	return nil, fmt.Errorf("unknown block type %q", b.Type)
}
