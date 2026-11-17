// Package mcpserver exposes sdd to coding agents as an MCP server over stdio
// (`sdd mcp`), built on the official Go SDK. Tools call the same use cases as
// the CLI (internal/app) and return the same text. Human-only operations
// (approvals, review decisions) are deliberately not exposed.
package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/darkcarard/quipu-sdd/internal/app"
	"github.com/darkcarard/quipu-sdd/internal/feature"
	"github.com/darkcarard/quipu-sdd/internal/version"
)

// Name is the server name agents see (tools appear as mcp__sdd__<tool> in Claude Code).
const Name = "sdd"

// Tools lists the tool names, in registration order.
var Tools = []string{"status", "guide", "spec_new", "plan_new", "lint", "sync_check", "trace", "task_next", "task_done", "group_verify", "review_context", "adopt_context"}

// Run serves the project over stdin/stdout until ctx is cancelled or the client disconnects.
func Run(ctx context.Context, a *app.App) error {
	return New(a).Run(ctx, &mcp.StdioTransport{})
}

// New builds the server for a project.
func New(a *app.App) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: Name, Title: "Spec-Driven Development", Version: version.Version},
		&mcp.ServerOptions{Instructions: app.WorkflowGuide()})

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "status",
		Description: "Phase and next step of every feature, or detailed status of one.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureArg) (*mcp.CallToolResult, any, error) {
		if in.Feature == "" {
			list, err := a.Overview()
			return text(app.FormatOverview(list), err)
		}
		d, err := a.Status(in.Feature)
		return text(app.FormatDetail(d), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "guide",
		Description: "Drafting rules and template for a topic: workflow, functional, technical, plan (adds the coverage checklist), implement or adopt. Pass the feature for its status. Call before drafting.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in guideArgs) (*mcp.CallToolResult, any, error) {
		return text(a.Guide(in.Topic, in.Feature))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "spec_new",
		Description: "Create a feature and its functional spec (text: the feature name or the user's description of it), or with technical=true its technical spec.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in specNewArgs) (*mcp.CallToolResult, any, error) {
		if in.Text == "" && in.Name == "" {
			return nil, nil, errors.New("pass text (the feature name or its title) or name")
		}
		c, err := as(a, req).NewSpec(ctx, app.NewSpecInput{
			Text: in.Text, Name: in.Name, Title: in.Title, Description: in.Description, Technical: in.Technical, Baseline: in.Baseline,
		})
		return text(app.FormatCreated(c), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "plan_new",
		Description: "Create plan.md for a feature whose specs are approved.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in requiredFeatureArg) (*mcp.CallToolResult, any, error) {
		c, err := as(a, req).NewPlan(ctx, in.Feature)
		return text(app.FormatCreated(c), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "lint",
		Description: "Check a feature's specs and plan (or all features). Errors block approval: run after every edit.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureArg) (*mcp.CallToolResult, any, error) {
		r, err := a.Lint(in.Feature)
		return text(app.FormatLint(r), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "sync_check",
		Description: "Check consistency between a feature's specs, plan and code (or all features).",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureArg) (*mcp.CallToolResult, any, error) {
		r, err := a.Sync(ctx, in.Feature)
		return text(app.FormatSync(r), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "trace",
		Description: "Follow an ID (FR-001, AC-002, T1.2, optionally feature/ID) through specs, plan, commits and tests. With matrix=true, id is a feature.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in traceArgs) (*mcp.CallToolResult, any, error) {
		if in.Matrix {
			return text(a.Matrix(ctx, in.ID))
		}
		entries, err := a.Trace(ctx, in.ID)
		return text(app.FormatTrace(entries), err)
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "task_next",
		Description: "Implementation: next step for a feature. Returns a task to implement, the user's revision feedback, a request to call group_verify, or a reason to stop.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in requiredFeatureArg) (*mcp.CallToolResult, any, error) {
		n, err := as(a, req).TaskNext(ctx, in.Feature)
		return text(app.FormatNext(n), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "task_done",
		Description: "Implementation: finish the current task. sdd builds; on success it checkpoints, otherwise returns the build output.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in taskDoneArgs) (*mcp.CallToolResult, any, error) {
		d, err := as(a, req).TaskDone(ctx, in.Feature, in.Summary)
		if err == nil && !d.Passed {
			return failure(app.FormatDone(d))
		}
		return text(app.FormatDone(d), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "group_verify",
		Description: "Implementation: when the group's tasks are done, run build, lint and test. On success the group is committed and waits for the user's review: stop. On failure, fix and retry.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in requiredFeatureArg) (*mcp.CallToolResult, any, error) {
		v, err := as(a, req).GroupVerify(ctx, in.Feature)
		if err == nil && !v.Passed {
			return failure(app.FormatVerified(v))
		}
		return text(app.FormatVerified(v), err)
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "review_context",
		Description: "Validation: brief for a deep review of an implemented feature (criteria with their tasks, commits and tests). Informational only.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in requiredFeatureArg) (*mcp.CallToolResult, any, error) {
		return text(a.ReviewContext(ctx, in.Feature))
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "adopt_context",
		Description: "Adoption: inventory of an existing project (languages, build files, suggested commands, layout, tests, docs) to document it as baseline features.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return text(a.AdoptContext(ctx))
	})
	return s
}

type taskDoneArgs struct {
	Feature string `json:"feature" jsonschema:"feature name or title"`
	Summary string `json:"summary,omitempty" jsonschema:"one line on what changed (shown in the review)"`
}

type featureArg struct {
	Feature string `json:"feature,omitempty" jsonschema:"feature name or title; omit for all"`
}

type requiredFeatureArg struct {
	Feature string `json:"feature" jsonschema:"feature name or title"`
}

type guideArgs struct {
	Topic   string `json:"topic" jsonschema:"workflow, functional, technical, plan, implement or adopt"`
	Feature string `json:"feature,omitempty" jsonschema:"feature whose status to include"`
}

type specNewArgs struct {
	Text        string `json:"text,omitempty" jsonschema:"feature name, or free text to derive it from"`
	Name        string `json:"name,omitempty" jsonschema:"explicit kebab-case name"`
	Title       string `json:"title,omitempty" jsonschema:"title (default: the text)"`
	Description string `json:"description,omitempty" jsonschema:"one-line description"`
	Technical   bool   `json:"technical,omitempty" jsonschema:"create the technical spec instead"`
	Baseline    bool   `json:"baseline,omitempty" jsonschema:"a baseline feature documenting existing code"`
}

type traceArgs struct {
	ID     string `json:"id" jsonschema:"ID, feature/ID, or (with matrix) a feature"`
	Matrix bool   `json:"matrix,omitempty" jsonschema:"return the feature's matrix"`
}

// text returns a successful text result, or the error as a tool error.
func text(s string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

// failure returns a tool error carrying s (a verification that did not pass).
func failure(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

// as returns a copy of the app that journals state changes as the calling agent.
func as(a *app.App, req *mcp.CallToolRequest) *app.App {
	client := "mcp-client"
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
			client = p.ClientInfo.Name
		}
	}
	c := *a
	c.Who = func(context.Context) (feature.Identity, error) { return feature.Identity{Agent: client}, nil }
	return &c
}
