// Package tools defines the tool hub contract through which the orchestrator
// executes the model's tool calls. The MCP-backed implementation (official Go
// SDK, stdio transport, allowlist and worktree path confinement) arrives in M5.
package tools

import (
	"context"
	"encoding/json"

	"github.com/darkcarard/quipu-sdd/internal/provider"
)

// Result is the outcome of a tool call, returned to the model as tool_result.
type Result struct {
	Output  string
	IsError bool
}

// Hub exposes allowed tools and executes calls.
type Hub interface {
	// List returns the tool definitions the model may call (already filtered by the allowlist).
	List(ctx context.Context) ([]provider.ToolDef, error)
	// Call executes a tool. Tool-level failures are reported in Result.IsError;
	// a non-nil error means the hub itself failed (e.g. a server crashed).
	Call(ctx context.Context, name string, input json.RawMessage) (Result, error)
	// Close stops every spawned server.
	Close() error
}
