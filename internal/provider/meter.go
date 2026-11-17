package provider

import (
	"context"
	"fmt"
	"sync"
)

// Meter accumulates the token usage of a run and enforces an optional budget
// (limits.token_budget). It is safe for concurrent use.
type Meter struct {
	mu       sync.Mutex
	usage    Usage
	requests int
	// Budget is the maximum Usage.Total for the run; 0 means unlimited.
	Budget int
}

// Add records the usage of one response.
func (m *Meter) Add(u Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = m.usage.Add(u)
	m.requests++
}

// Usage returns the accumulated usage and the number of responses counted.
func (m *Meter) Usage() (Usage, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage, m.requests
}

// Check returns ErrBudgetExceeded once the budget is spent.
func (m *Meter) Check() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Budget > 0 && m.usage.Total() >= m.Budget {
		return fmt.Errorf("%w: %d of %d tokens used", ErrBudgetExceeded, m.usage.Total(), m.Budget)
	}
	return nil
}

type metered struct {
	Provider
	m *Meter
}

// WithMeter counts the usage of every response in m and refuses new requests
// once m's budget is spent. Usage is counted per attempt, so place it inside
// WithRetry only if failed attempts should be counted (they report no usage).
func WithMeter(p Provider, m *Meter) Provider { return &metered{Provider: p, m: m} }

func (p *metered) Complete(ctx context.Context, req Request) (Response, error) {
	if err := p.m.Check(); err != nil {
		return Response{}, err
	}
	resp, err := p.Provider.Complete(ctx, req)
	if err == nil {
		p.m.Add(resp.Usage)
	}
	return resp, err
}

func (p *metered) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	if err := p.m.Check(); err != nil {
		return nil, err
	}
	in, err := p.Provider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		for ev := range in {
			if ev.Type == EventDone {
				p.m.Add(ev.Response.Usage)
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
