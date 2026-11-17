// Package anthropic adapts the neutral provider contract to the Anthropic
// Messages API using plain net/http (no vendor SDK). Implemented in milestone M2.
package anthropic

import (
	"context"
	"errors"
	"net/http"

	"github.com/darkcarard/sdd-framework/internal/provider"
)

// DefaultBaseURL is the Anthropic API endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// APIVersion is sent as the anthropic-version header.
const APIVersion = "2023-06-01"

// Client is the Anthropic adapter.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// Option customizes a Client.
type Option func(*Client)

// WithBaseURL overrides the API endpoint (used by tests with recorded fixtures).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a client. The API key is held in memory only and never logged.
func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic: API key is required")
	}
	c := &Client{apiKey: apiKey, baseURL: DefaultBaseURL, http: http.DefaultClient}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

var _ provider.Provider = (*Client)(nil)

// Name implements provider.Provider.
func (c *Client) Name() string { return "anthropic" }

// Complete implements provider.Provider.
func (c *Client) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{}, provider.ErrNotImplemented // M2
}

// Stream implements provider.Provider.
func (c *Client) Stream(ctx context.Context, req provider.Request) (<-chan provider.StreamEvent, error) {
	return nil, provider.ErrNotImplemented // M2
}
