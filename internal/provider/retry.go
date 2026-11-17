package provider

import (
	"context"
	"math/rand/v2"
	"time"
)

// RetryPolicy configures WithRetry. Delays grow exponentially from BaseDelay
// with jitter (each delay is a random value in [d/2, d)), are capped at
// MaxDelay, and a provider-requested RetryAfter takes precedence.
type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	// OnRetry is called before each retry (for the journal and the transcript).
	OnRetry func(attempt int, err error, delay time.Duration)
	// Sleep and Rand are injectable for tests.
	Sleep func(ctx context.Context, d time.Duration) error
	Rand  func() float64
}

// DefaultRetryPolicy returns the policy used unless configured otherwise.
func DefaultRetryPolicy(maxRetries int) RetryPolicy {
	return RetryPolicy{MaxRetries: maxRetries, BaseDelay: time.Second, MaxDelay: time.Minute}
}

// Delay returns the wait before retry number attempt (1-based).
func (p RetryPolicy) Delay(attempt int, err error) time.Duration {
	if e, ok := err.(*APIError); ok && e.RetryAfter > 0 {
		return min(e.RetryAfter, p.MaxDelay)
	}
	d := p.BaseDelay << (attempt - 1)
	if d <= 0 || d > p.MaxDelay {
		d = p.MaxDelay
	}
	r := rand.Float64
	if p.Rand != nil {
		r = p.Rand
	}
	return d/2 + time.Duration(r()*float64(d/2))
}

func (p RetryPolicy) sleep(ctx context.Context, d time.Duration) error {
	if p.Sleep != nil {
		return p.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type retrying struct {
	Provider
	policy RetryPolicy
}

// WithRetry retries transient failures (IsRetryable) with exponential backoff.
// A stream is retried only while nothing has been delivered from it; a failure
// after the first event is passed on, and the caller resumes from its checkpoint.
func WithRetry(p Provider, policy RetryPolicy) Provider {
	return &retrying{Provider: p, policy: policy}
}

func (r *retrying) wait(ctx context.Context, attempt int, err error) bool {
	if attempt > r.policy.MaxRetries || !IsRetryable(err) || ctx.Err() != nil {
		return false
	}
	d := r.policy.Delay(attempt, unwrapAPI(err))
	if r.policy.OnRetry != nil {
		r.policy.OnRetry(attempt, err, d)
	}
	return r.policy.sleep(ctx, d) == nil
}

func unwrapAPI(err error) error {
	for e := err; e != nil; {
		if a, ok := e.(*APIError); ok {
			return a
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	return err
}

func (r *retrying) Complete(ctx context.Context, req Request) (Response, error) {
	for attempt := 1; ; attempt++ {
		resp, err := r.Provider.Complete(ctx, req)
		if err == nil || !r.wait(ctx, attempt, err) {
			return resp, err
		}
	}
}

func (r *retrying) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	first, attempt, err := r.open(ctx, req, 1)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		ch := first
		for {
			delivered, retry := false, false
			for ev := range ch {
				if ev.Type == EventError && !delivered && r.wait(ctx, attempt, ev.Err) {
					retry = true
					break // nothing reached the caller yet: retry transparently
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
				delivered = true
				if ev.Type == EventDone || ev.Type == EventError {
					return
				}
			}
			if !retry {
				if !delivered { // closed without a result
					select {
					case out <- StreamEvent{Type: EventError, Err: &APIError{Message: "stream ended without a result"}}:
					case <-ctx.Done():
					}
				}
				return
			}
			next, used, err := r.open(ctx, req, attempt+1)
			attempt = used
			if err != nil {
				select {
				case out <- StreamEvent{Type: EventError, Err: err}:
				case <-ctx.Done():
				}
				return
			}
			ch = next
		}
	}()
	return out, nil
}

// open starts a stream, retrying failures to connect. It returns the attempt
// number that succeeded (or failed for good).
func (r *retrying) open(ctx context.Context, req Request, attempt int) (<-chan StreamEvent, int, error) {
	for ; ; attempt++ {
		ch, err := r.Provider.Stream(ctx, req)
		if err == nil || !r.wait(ctx, attempt, err) {
			return ch, attempt, err
		}
	}
}
