// Package mcpserver exposes sdd to coding agents as an MCP server over stdio
// (`sdd mcp`), built on the official Go SDK. Tools call the same use cases as
// the CLI (internal/app) and return the same text. Human-only operations
// (approvals, review decisions) are deliberately not exposed.
package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/darkcarard/sdd-framework/internal/app"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/version"
)

// Name is the server name agents see (tools appear as mcp__sdd__<tool> in Claude Code).
const Name = "sdd"

// Tools lists the tool names, in registration order.
var Tools = []string{"status", "guide", "spec_new", "plan_new", "lint", "sync_check", "trace"}

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
		Description: "Show the phase of every feature, or the detailed status of one feature (artifact statuses, lint counts, approvals) and the next step.",
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
		Name: "guide",
		Description: "Get the instructions, format rules and template for drafting an artifact. " +
			"Topics: workflow (how to work with sdd), functional, technical, plan. Pass a feature to include its current status (and, for plan, the checklist of IDs to cover). Call it before drafting.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in guideArgs) (*mcp.CallToolResult, any, error) {
		return text(a.Guide(in.Topic, in.Feature))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "spec_new",
		Description: "Create a feature and its functional spec from the template (text may be free text such as the user's own words; " +
			"a valid feature name is derived from it), or, with technical=true, the technical spec of an existing feature whose functional spec is approved.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in specNewArgs) (*mcp.CallToolResult, any, error) {
		if in.Text == "" && in.Name == "" {
			return nil, nil, errors.New("pass text (the feature name or its title) or name")
		}
		c, err := as(a, req).NewSpec(ctx, app.NewSpecInput{
			Text: in.Text, Name: in.Name, Title: in.Title, Description: in.Description, Technical: in.Technical,
		})
		return text(app.FormatCreated(c), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "plan_new",
		Description: "Create plan.md for a feature whose technical spec is approved, from the template, with the source hashes stamped.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in requiredFeatureArg) (*mcp.CallToolResult, any, error) {
		c, err := as(a, req).NewPlan(ctx, in.Feature)
		return text(app.FormatCreated(c), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "lint",
		Description: "Run the deterministic guardrail checks on a feature's specs and plan (or on every feature). " +
			"Errors block approval; fix them after every edit.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureArg) (*mcp.CallToolResult, any, error) {
		r, err := a.Lint(in.Feature)
		return text(app.FormatLint(r), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sync_check",
		Description: "Check consistency between a feature's artifacts (or every feature's): the technical spec maps every requirement and criterion, " +
			"the plan covers them, recorded hashes match, and nothing was edited after approval.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in featureArg) (*mcp.CallToolResult, any, error) {
		r, err := a.Sync(in.Feature)
		return text(app.FormatSync(r), err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "trace",
		Description: "Follow an ID (FR-001, AC-002, ADR-001, G1, T1.2; optionally <feature>/<ID>) through the specs and the plan. " +
			"With matrix=true, id is a feature and the whole traceability matrix is returned in markdown.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in traceArgs) (*mcp.CallToolResult, any, error) {
		if in.Matrix {
			return text(a.Matrix(in.ID))
		}
		entries, err := a.Trace(in.ID)
		return text(app.FormatTrace(entries), err)
	})
	return s
}

type featureArg struct {
	Feature string `json:"feature,omitempty" jsonschema:"a feature name, title or the text it was created from; omit for every feature"`
}

type requiredFeatureArg struct {
	Feature string `json:"feature" jsonschema:"a feature name, title or the text it was created from"`
}

type guideArgs struct {
	Topic   string `json:"topic" jsonschema:"workflow, functional, technical or plan"`
	Feature string `json:"feature,omitempty" jsonschema:"a feature whose current status to include"`
}

type specNewArgs struct {
	Text        string `json:"text,omitempty" jsonschema:"the feature name, or free text such as the user's description of it (a name is derived from it)"`
	Name        string `json:"name,omitempty" jsonschema:"an explicit feature name (kebab-case), overriding the derived one"`
	Title       string `json:"title,omitempty" jsonschema:"human-readable title (default: the free text)"`
	Description string `json:"description,omitempty" jsonschema:"one-line description of the feature"`
	Technical   bool   `json:"technical,omitempty" jsonschema:"create the technical spec of an existing feature instead"`
}

type traceArgs struct {
	ID     string `json:"id" jsonschema:"the ID to trace, optionally as <feature>/<ID>; with matrix, a feature"`
	Matrix bool   `json:"matrix,omitempty" jsonschema:"return the traceability matrix of the feature named by id"`
}

// text returns a successful text result, or the error as a tool error.
func text(s string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
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
