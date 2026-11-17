// Package orchestrator runs the per-phase agent loops. It depends only on the
// neutral interfaces (provider.Provider, tools.Hub, workspace.Workspace) so that
// orchestration stays independent of any vendor.
package orchestrator

import (
	"errors"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/provider"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/tools"
	"github.com/darkcarard/sdd-framework/internal/workspace"
)

// Engine wires the dependencies of the workflow.
type Engine struct {
	Config    config.Config
	Layout    state.Layout
	Provider  provider.Provider
	Tools     tools.Hub
	Workspace workspace.Workspace
}

// New validates and returns an Engine. Tools and Workspace may be nil for
// phases that do not need them (Specify, Plan).
func New(cfg config.Config, layout state.Layout, p provider.Provider, t tools.Hub, w workspace.Workspace) (*Engine, error) {
	if p == nil {
		return nil, errors.New("orchestrator: provider is required")
	}
	if layout.Root == "" {
		return nil, errors.New("orchestrator: project root is required")
	}
	return &Engine{Config: cfg, Layout: layout, Provider: p, Tools: t, Workspace: w}, nil
}
