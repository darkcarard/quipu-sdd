// Package provider defines the vendor-neutral LLM contract. The orchestrator
// depends only on these types; each vendor adapter (anthropic, gemini, openai)
// translates them to and from its own HTTP schema.
package provider

import (
	"context"
	"encoding/json"
	"errors"
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
)

// ContentBlock is one piece of a message. Only the fields for its Type are set.
type ContentBlock struct {
	Type BlockType `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`

	// tool_result (ToolUseID references the originating tool_use)
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// Text returns a text block.
func Text(s string) ContentBlock { return ContentBlock{Type: BlockText, Text: s} }

// ToolResult returns a tool_result block answering a tool_use.
func ToolResult(toolUseID, output string, isError bool) ContentBlock {
	return ContentBlock{Type: BlockToolResult, ToolUseID: toolUseID, Output: output, IsError: isError}
}

// Message is a conversation turn.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ToolDef describes a tool the model may call. InputSchema is a JSON Schema object.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Request is a provider-neutral completion request.
type Request struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// StopReason explains why generation ended.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopOther     StopReason = "other"
)

// Usage reports token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is a complete assistant turn.
type Response struct {
	Message    Message    `json:"message"`
	StopReason StopReason `json:"stop_reason"`
	Usage      Usage      `json:"usage"`
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

// StreamEventType discriminates StreamEvent.
type StreamEventType string

const (
	EventTextDelta StreamEventType = "text_delta"
	EventDone      StreamEventType = "done"  // Response holds the assembled turn
	EventError     StreamEventType = "error" // Err holds the failure
)

// StreamEvent is emitted while streaming. The channel is closed after Done or Error.
type StreamEvent struct {
	Type     StreamEventType
	Text     string
	Response *Response
	Err      error
}

// Provider is implemented by every vendor adapter.
type Provider interface {
	// Name returns the provider identifier (e.g. "anthropic").
	Name() string
	// Complete sends a request and returns the full assistant turn.
	Complete(ctx context.Context, req Request) (Response, error)
	// Stream sends a request and streams text deltas, ending with EventDone or EventError.
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

// ErrNotImplemented is returned by adapters that are not built yet.
var ErrNotImplemented = errors.New("provider: not implemented")

// RetryableError marks transient failures (network, 429, 5xx, 529) that the
// orchestrator's retry policy may retry.
type RetryableError struct {
	Err        error
	StatusCode int
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// IsRetryable reports whether err is transient.
func IsRetryable(err error) bool {
	var r *RetryableError
	return errors.As(err, &r)
}
