// Package registry builds the configured provider (sdd.yaml `provider`) with
// the standard decorators: transcript recording, retries with backoff, and
// usage accounting against the run's token budget.
package registry

import (
	"fmt"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/provider"
	"github.com/darkcarard/sdd-framework/internal/provider/anthropic"
	"github.com/darkcarard/sdd-framework/internal/transcript"
)

// Options are the run-scoped collaborators of a provider.
type Options struct {
	// Transcript, if set, records every attempt and retry.
	Transcript *transcript.Writer
	// Meter, if set, accumulates usage and enforces its Budget.
	Meter *provider.Meter
	// Retry overrides the retry policy (tests); nil uses
	// provider.DefaultRetryPolicy(cfg.Limits.MaxRetries).
	Retry *provider.RetryPolicy
	// AnthropicOptions are appended to the options derived from cfg (tests).
	AnthropicOptions []anthropic.Option
}

// New returns the provider named in cfg, wrapped as
// meter(retry(transcript(adapter))): each attempt is recorded, transient
// failures are retried, and only completed turns count toward the budget.
func New(cfg config.Config, opts Options) (provider.Provider, error) {
	var p provider.Provider
	switch cfg.Provider.Name {
	case "anthropic":
		key, err := cfg.APIKey()
		if err != nil {
			return nil, err
		}
		aopts := []anthropic.Option{
			anthropic.WithModel(cfg.Provider.Model),
			anthropic.WithMaxTokens(cfg.Provider.MaxTokens),
			anthropic.WithEffort(cfg.Provider.Effort),
			anthropic.WithThinkingDisplay(cfg.Provider.ThinkingDisplay),
			anthropic.WithFallbacks(cfg.Provider.Fallbacks),
			anthropic.WithIdleTimeout(cfg.Limits.RequestTimeout),
		}
		if cfg.Provider.BaseURL != "" {
			aopts = append(aopts, anthropic.WithBaseURL(cfg.Provider.BaseURL))
		}
		c, err := anthropic.New(key, append(aopts, opts.AnthropicOptions...)...)
		if err != nil {
			return nil, err
		}
		p = c
	case "gemini", "openai":
		return nil, fmt.Errorf("provider %q is not implemented yet (planned for M7, see docs/ROADMAP.md)", cfg.Provider.Name)
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider.Name)
	}

	if opts.Transcript != nil {
		p = transcript.Recording(p, opts.Transcript)
	}
	policy := provider.DefaultRetryPolicy(cfg.Limits.MaxRetries)
	if opts.Retry != nil {
		policy = *opts.Retry
	}
	if opts.Transcript != nil && policy.OnRetry == nil {
		policy.OnRetry = opts.Transcript.RecordRetry(cfg.Provider.Name)
	}
	p = provider.WithRetry(p, policy)
	if opts.Meter != nil {
		p = provider.WithMeter(p, opts.Meter)
	}
	return p, nil
}

// Redactor builds the transcript redactor for cfg: its secrets plus the
// built-in and configured patterns.
func Redactor(cfg config.Config) (*transcript.Redactor, error) {
	return transcript.NewRedactor(cfg.Secrets(), cfg.Audit.RedactPatterns)
}
