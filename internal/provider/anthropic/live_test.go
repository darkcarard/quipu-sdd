//go:build live

// Live smoke test against the real API. It spends a few cents:
//
//	ANTHROPIC_API_KEY=... make live-test
//
// It runs one tool round-trip (tool_use → tool_result → final answer), which
// also proves the API accepts assistant turns replayed byte for byte.
package anthropic

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/provider"
)

func TestLiveToolRoundTrip(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is not set")
	}
	model := os.Getenv("SDD_LIVE_MODEL")
	if model == "" {
		model = DefaultModel
	}
	c, err := New(key, WithModel(model), WithMaxTokens(4000), WithEffort(provider.EffortLow))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	req := provider.Request{
		System: "You are a test harness. Follow instructions exactly.",
		Tools: []provider.ToolDef{{
			Name:        "get_secret_word",
			Description: "Returns the secret word.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string"}},"required":["reason"]}`),
		}},
		Messages: []provider.Message{provider.UserText("Call get_secret_word once, then reply with only the secret word.")},
	}
	first, err := c.Complete(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("turn 1: model=%s stop=%s usage=%+v blocks=%d", first.Model, first.StopReason, first.Usage, len(first.Message.Content))
	uses := first.ToolUses()
	if first.StopReason != provider.StopToolUse || len(uses) == 0 {
		t.Fatalf("expected a tool call, got stop=%s text=%q", first.StopReason, first.Text())
	}
	if uses[0].InvalidInput {
		t.Fatalf("tool input did not parse: %s", uses[0].Input)
	}

	var results []provider.ContentBlock
	for _, u := range uses {
		results = append(results, provider.ToolResult(u.ToolUseID, "PELICAN", false))
	}
	req.Messages = append(req.Messages, first.Message, provider.Message{Role: provider.RoleUser, Content: results})
	second, err := c.Complete(ctx, req)
	if err != nil {
		t.Fatalf("replaying the assistant turn failed: %v", err)
	}
	t.Logf("turn 2: stop=%s usage=%+v text=%q", second.StopReason, second.Usage, second.Text())
	if !strings.Contains(strings.ToUpper(second.Text()), "PELICAN") {
		t.Errorf("final answer = %q", second.Text())
	}
}
