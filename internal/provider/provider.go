// Package provider defines the vendor-neutral LLM contract. The orchestrator
// depends only on these types; each vendor adapter (anthropic, gemini, openai)
// translates them to and from its own HTTP schema.
//
// Conversations are append-only: an assistant turn returned by a provider is
// sent back unchanged on later requests. Blocks produced by a provider carry
// their exact wire form in Raw so the adapter can replay them byte for byte
// (reasoning blocks are only valid when replayed unmodified).
package provider

import (
	"context"
	"encoding/json"
)

// Role of a message author.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// BlockType discriminates ContentBlock.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
	// BlockThinking is model reasoning. Text holds a summary or a progress
	// note when the provider returns one (often empty); Raw is what matters.
	BlockThinking BlockType = "thinking"
	// BlockOther is a provider-specific block kept only for replay (Raw).
	BlockOther BlockType = "other"
)

// ContentBlock is one piece of a message. Only the fields for its Type are set.
type ContentBlock struct {
	Type BlockType `json:"type"`

	// text, thinking
	Text string `json:"text,omitempty"`

	// tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	// InvalidInput is set when the model's tool input is not valid JSON (or
	// was cut off). Input then holds the raw text as a JSON string; do not run
	// the tool, answer with InvalidInputResult instead.
	InvalidInput bool `json:"invalid_input,omitempty"`

	// tool_result (ToolUseID references the originating tool_use)
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"is_error,omitempty"`

	// Raw is the block exactly as RawProvider returned it, replayed verbatim
	// to that provider. Other providers ignore it.
	Raw         json.RawMessage `json:"raw,omitempty"`
	RawProvider string          `json:"raw_provider,omitempty"`
}

// Text returns a text block.
func Text(s string) ContentBlock { return ContentBlock{Type: BlockText, Text: s} }

// ToolResult returns a tool_result block answering a tool_use.
func ToolResult(toolUseID, output string, isError bool) ContentBlock {
	return ContentBlock{Type: BlockToolResult, ToolUseID: toolUseID, Output: output, IsError: isError}
}

// InvalidInputResult answers a tool_use whose input could not be parsed, so the
// model can retry the call.
func InvalidInputResult(b ContentBlock) ContentBlock {
	var raw string
	if err := json.Unmarshal(b.Input, &raw); err != nil {
		raw = string(b.Input)
	}
	out, _ := json.Marshal(map[string]string{"INVALID_JSON": raw})
	return ToolResult(b.ToolUseID, string(out), true)
}

// Message is a conversation turn.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// UserText returns a user message with one text block.
func UserText(s string) Message { return Message{Role: RoleUser, Content: []ContentBlock{Text(s)}} }

// ToolDef describes a tool the model may call. InputSchema is a JSON Schema object.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Effort levels trade thoroughness for tokens and latency.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortXHigh  = "xhigh"
	EffortMax    = "max"
)

// Request is a provider-neutral completion request. Zero values fall back to
// the adapter's configured defaults (model, max tokens, effort).
type Request struct {
	Model     string    `json:"model,omitempty"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Tools     []ToolDef `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
	Effort    string    `json:"effort,omitempty"`
}

// StopReason explains why generation ended.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	// StopContextWindow: the conversation no longer fits the model's context.
	StopContextWindow StopReason = "context_window"
	// StopRefusal: the model declined; never run the tools of such a turn.
	StopRefusal StopReason = "refusal"
	// StopPauseTurn: a long server-side turn paused; send the history back unchanged to resume.
	StopPauseTurn StopReason = "pause_turn"
	StopOther     StopReason = "other"
)

// Usage reports token consumption.
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// Add returns the sum of two usages.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:         u.InputTokens + o.InputTokens,
		OutputTokens:        u.OutputTokens + o.OutputTokens,
		CacheCreationTokens: u.CacheCreationTokens + o.CacheCreationTokens,
		CacheReadTokens:     u.CacheReadTokens + o.CacheReadTokens,
	}
}

// Total returns every token billed: input (fresh, cache writes and cache reads) and output.
func (u Usage) Total() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationTokens + u.CacheReadTokens
}

// Refusal details a StopRefusal.
type Refusal struct {
	Category    string `json:"category,omitempty"`
	Explanation string `json:"explanation,omitempty"`
}

// Response is a complete assistant turn.
type Response struct {
	ID         string     `json:"id,omitempty"`
	Model      string     `json:"model,omitempty"` // the model that served the turn (may be a fallback)
	Message    Message    `json:"message"`
	StopReason StopReason `json:"stop_reason"`
	// RawStopReason is the provider's own value, for reasons mapped to StopOther.
	RawStopReason string   `json:"raw_stop_reason,omitempty"`
	Refusal       *Refusal `json:"refusal,omitempty"`
	Usage         Usage    `json:"usage"`
}

// ToolUses returns the tool_use blocks of the response.
func (r Response) ToolUses() []ContentBlock {
	var out []ContentBlock
	for _, b := range r.Message.Content {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

// Text returns the concatenated text blocks of the response.
func (r Response) Text() string {
	var s string
	for _, b := range r.Message.Content {
		if b.Type == BlockText {
			s += b.Text
		}
	}
	return s
}

// StreamEventType discriminates StreamEvent.
type StreamEventType string

const (
	EventTextDelta     StreamEventType = "text_delta"
	EventThinkingDelta StreamEventType = "thinking_delta" // reasoning summary or progress note
	EventToolUse       StreamEventType = "tool_use"       // a tool call started (Block has its ID and name)
	EventDone          StreamEventType = "done"           // Response holds the assembled turn
	EventError         StreamEventType = "error"          // Err holds the failure
)

// StreamEvent is emitted while streaming. The channel is closed after Done or Error.
type StreamEvent struct {
	Type     StreamEventType
	Text     string
	Block    *ContentBlock
	Response *Response
	Err      error
}

// Provider is implemented by every vendor adapter.
type Provider interface {
	// Name returns the provider identifier (e.g. "anthropic").
	Name() string
	// Complete sends a request and returns the full assistant turn.
	Complete(ctx context.Context, req Request) (Response, error)
	// Stream sends a request and streams deltas, ending with exactly one
	// EventDone or EventError. Cancel ctx to abandon the stream.
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

// Collect drains a stream into a Response.
func Collect(ch <-chan StreamEvent) (Response, error) {
	for ev := range ch {
		switch ev.Type {
		case EventDone:
			return *ev.Response, nil
		case EventError:
			return Response{}, ev.Err
		}
	}
	return Response{}, &APIError{Message: "stream ended without a result", Retryable: true}
}
