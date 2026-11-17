package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/provider"
)

// idleTimer cancels the request when it is not reset within d.
type idleTimer struct {
	mu sync.Mutex
	t  *time.Timer
	d  time.Duration
}

func newIdleTimer(d time.Duration, cancel context.CancelCauseFunc) *idleTimer {
	it := &idleTimer{d: d}
	if d > 0 {
		it.t = time.AfterFunc(d, func() { cancel(errIdle) })
	}
	return it
}

func (it *idleTimer) reset() {
	if it.t == nil {
		return
	}
	it.mu.Lock()
	it.t.Reset(it.d)
	it.mu.Unlock()
}

func (it *idleTimer) stop() {
	if it.t != nil {
		it.t.Stop()
	}
}

// idleReader resets the idle timer whenever data arrives (pings included).
type idleReader struct {
	r io.Reader
	t *idleTimer
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.t.reset()
	}
	return n, err
}

// sseEvent is one server-sent event.
type sseEvent struct {
	name string
	data string
}

// readSSE calls fn for each event in r until fn returns false or r ends.
func readSSE(r io.Reader, fn func(sseEvent) (bool, error)) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var ev sseEvent
	var data []string
	for {
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF // the stream must end with message_stop
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if ev.name == "" && len(data) == 0 {
				continue
			}
			ev.data = strings.Join(data, "\n")
			more, ferr := fn(ev)
			if ferr != nil || !more {
				return ferr
			}
			ev, data = sseEvent{}, nil
		case strings.HasPrefix(line, ":"): // comment
		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				ev.name = value
			case "data":
				data = append(data, value)
			}
		}
	}
}

// partial is a content block being assembled.
type partial struct {
	typ       string
	start     json.RawMessage
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	input     strings.Builder
	citations []json.RawMessage
}

type wireUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
}

func (u wireUsage) merge(into *provider.Usage) {
	if u.InputTokens > 0 {
		into.InputTokens = u.InputTokens
	}
	if u.OutputTokens > 0 {
		into.OutputTokens = u.OutputTokens
	}
	if u.CacheCreationTokens > 0 {
		into.CacheCreationTokens = u.CacheCreationTokens
	}
	if u.CacheReadTokens > 0 {
		into.CacheReadTokens = u.CacheReadTokens
	}
}

type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		ID    string    `json:"id"`
		Model string    `json:"model"`
		Usage wireUsage `json:"usage"`
	} `json:"message"`
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
	Delta        struct {
		Type        string          `json:"type"`
		Text        string          `json:"text"`
		Thinking    string          `json:"thinking"`
		Signature   string          `json:"signature"`
		PartialJSON string          `json:"partial_json"`
		Citation    json.RawMessage `json:"citation"`
		StopReason  string          `json:"stop_reason"`
		StopDetails *struct {
			Category    string `json:"category"`
			Explanation string `json:"explanation"`
		} `json:"stop_details"`
	} `json:"delta"`
	Usage *wireUsage `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// errorStatus maps in-stream error types to the HTTP status they stand for.
var errorStatus = map[string]int{
	"invalid_request_error": 400, "authentication_error": 401, "permission_error": 403,
	"not_found_error": 404, "request_too_large": 413, "rate_limit_error": 429,
	"api_error": 500, "overloaded_error": 529,
}

// readStream parses the SSE body, sends deltas to out and returns the final
// event (EventDone or EventError) for the caller to send.
func (c *Client) readStream(ctx context.Context, body io.Reader, out chan<- provider.StreamEvent, requestID string) provider.StreamEvent {
	var resp provider.Response
	resp.Message.Role = provider.RoleAssistant
	blocks := map[int]*partial{}
	var order []int
	var final []provider.ContentBlock
	done := false

	send := func(ev provider.StreamEvent) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	err := readSSE(body, func(e sseEvent) (bool, error) {
		var se streamEvent
		if e.data != "" {
			if err := json.Unmarshal([]byte(e.data), &se); err != nil {
				return false, fmt.Errorf("decoding %s event: %w", e.name, err)
			}
		}
		if se.Type == "" {
			se.Type = e.name
		}
		switch se.Type {
		case "ping":
		case "message_start":
			resp.ID, resp.Model = se.Message.ID, se.Message.Model
			se.Message.Usage.merge(&resp.Usage)
		case "content_block_start":
			var head struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Text  string `json:"text"`
				Think string `json:"thinking"`
				Sig   string `json:"signature"`
			}
			if err := json.Unmarshal(se.ContentBlock, &head); err != nil {
				return false, fmt.Errorf("decoding content_block_start: %w", err)
			}
			p := &partial{typ: head.Type, start: se.ContentBlock}
			p.text.WriteString(head.Text)
			p.thinking.WriteString(head.Think)
			p.signature.WriteString(head.Sig)
			blocks[se.Index] = p
			order = append(order, se.Index)
			if head.Type == "tool_use" {
				b := provider.ContentBlock{Type: provider.BlockToolUse, ToolUseID: head.ID, ToolName: head.Name}
				if !send(provider.StreamEvent{Type: provider.EventToolUse, Block: &b}) {
					return false, ctx.Err()
				}
			}
		case "content_block_delta":
			p := blocks[se.Index]
			if p == nil {
				return false, fmt.Errorf("delta for unknown content block %d", se.Index)
			}
			switch se.Delta.Type {
			case "text_delta":
				p.text.WriteString(se.Delta.Text)
				if !send(provider.StreamEvent{Type: provider.EventTextDelta, Text: se.Delta.Text}) {
					return false, ctx.Err()
				}
			case "thinking_delta":
				p.thinking.WriteString(se.Delta.Thinking)
				if se.Delta.Thinking != "" && !send(provider.StreamEvent{Type: provider.EventThinkingDelta, Text: se.Delta.Thinking}) {
					return false, ctx.Err()
				}
			case "signature_delta":
				p.signature.WriteString(se.Delta.Signature)
			case "input_json_delta":
				p.input.WriteString(se.Delta.PartialJSON)
			case "citations_delta":
				p.citations = append(p.citations, se.Delta.Citation)
			}
		case "content_block_stop":
		case "message_delta":
			if se.Delta.StopReason != "" {
				resp.StopReason, resp.RawStopReason = mapStopReason(se.Delta.StopReason)
			}
			if d := se.Delta.StopDetails; d != nil && resp.StopReason == provider.StopRefusal {
				resp.Refusal = &provider.Refusal{Category: d.Category, Explanation: d.Explanation}
			}
			if se.Usage != nil {
				se.Usage.merge(&resp.Usage)
			}
		case "message_stop":
			done = true
			return false, nil
		case "error":
			return false, &provider.APIError{
				Provider: c.Name(), StatusCode: errorStatus[se.Error.Type], Type: se.Error.Type,
				Message: se.Error.Message, RequestID: requestID,
				Retryable: provider.RetryableStatus(errorStatus[se.Error.Type]),
			}
		}
		return true, nil
	})
	if err == nil && !done {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return provider.StreamEvent{Type: provider.EventError, Err: err}
	}

	for _, i := range order {
		b, err := c.finish(blocks[i], resp.StopReason)
		if err != nil {
			return provider.StreamEvent{Type: provider.EventError, Err: err}
		}
		final = append(final, b)
	}
	resp.Message.Content = afterFallback(final)
	if resp.StopReason == "" {
		resp.StopReason = provider.StopOther
	}
	return provider.StreamEvent{Type: provider.EventDone, Response: &resp}
}

// mapStopReason maps the API stop_reason to the neutral one.
func mapStopReason(s string) (provider.StopReason, string) {
	switch s {
	case "end_turn", "stop_sequence":
		return provider.StopEndTurn, s
	case "tool_use":
		return provider.StopToolUse, s
	case "max_tokens":
		return provider.StopMaxTokens, s
	case "model_context_window_exceeded":
		return provider.StopContextWindow, s
	case "refusal":
		return provider.StopRefusal, s
	case "pause_turn":
		return provider.StopPauseTurn, s
	}
	return provider.StopOther, s
}

// finish turns an assembled block into a neutral block with its exact wire form.
func (c *Client) finish(p *partial, stop provider.StopReason) (provider.ContentBlock, error) {
	b := provider.ContentBlock{RawProvider: c.Name()}
	var err error
	switch p.typ {
	case "text":
		b.Type, b.Text = provider.BlockText, p.text.String()
		b.Raw, err = json.Marshal(struct {
			Type      string            `json:"type"`
			Text      string            `json:"text"`
			Citations []json.RawMessage `json:"citations,omitempty"`
		}{"text", b.Text, p.citations})
	case "thinking":
		b.Type, b.Text = provider.BlockThinking, p.thinking.String()
		b.Raw, err = json.Marshal(struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
		}{"thinking", b.Text, p.signature.String()})
	case "redacted_thinking":
		b.Type, b.Raw = provider.BlockThinking, p.start
	case "tool_use":
		var head struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(p.start, &head); err != nil {
			return b, err
		}
		b.Type, b.ToolUseID, b.ToolName = provider.BlockToolUse, head.ID, head.Name
		input := json.RawMessage(strings.TrimSpace(p.input.String()))
		if len(input) == 0 {
			input = head.Input
		}
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		replay := input
		if !isJSONObject(input) || stop == provider.StopMaxTokens {
			// Invalid or possibly truncated input: never run the tool on it.
			b.InvalidInput = true
			b.Input, _ = json.Marshal(string(input))
			if !isJSONObject(input) {
				replay = json.RawMessage(`{}`)
			}
		} else {
			b.Input = input
		}
		b.Raw, err = json.Marshal(struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}{"tool_use", head.ID, head.Name, replay})
	default:
		// Server tools, fallback markers and future block types: keep the
		// start form, completed with any streamed input or text.
		b.Type = provider.BlockOther
		b.Raw, err = mergeBlock(p)
	}
	return b, err
}

func isJSONObject(b json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(b, &m) == nil
}

func mergeBlock(p *partial) (json.RawMessage, error) {
	if p.input.Len() == 0 && p.text.Len() == 0 {
		return p.start, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(p.start, &m); err != nil {
		return nil, err
	}
	if in := strings.TrimSpace(p.input.String()); in != "" && json.Valid([]byte(in)) {
		m["input"] = json.RawMessage(in)
	}
	if p.text.Len() > 0 {
		t, _ := json.Marshal(p.text.String())
		m["text"] = t
	}
	return json.Marshal(m)
}

// afterFallback applies the echo rule for a turn that a fallback model took
// over mid-output: before the last `fallback` block, only text blocks (and the
// fallback markers themselves) are kept. The dropped reasoning and tool calls
// belong to the declined attempt and must neither run nor be sent back.
func afterFallback(blocks []provider.ContentBlock) []provider.ContentBlock {
	last := -1
	for i, b := range blocks {
		if b.Type == provider.BlockOther && blockType(b.Raw) == "fallback" {
			last = i
		}
	}
	if last < 0 {
		return blocks
	}
	var out []provider.ContentBlock
	for i, b := range blocks {
		if i < last && b.Type != provider.BlockText && blockType(b.Raw) != "fallback" {
			continue
		}
		out = append(out, b)
	}
	return out
}

func blockType(raw json.RawMessage) string {
	var h struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &h)
	return h.Type
}
