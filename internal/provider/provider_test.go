package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is a scripted provider: each call consumes the next step.
type fake struct {
	mu    sync.Mutex
	steps []step
	calls int
}

// step is the outcome of one call: an error before streaming (err), or a list
// of events (the last one should be Done or Error).
type step struct {
	err    error
	events []StreamEvent
}

func done(text string, usage Usage) StreamEvent {
	return StreamEvent{Type: EventDone, Response: &Response{
		Message: Message{Role: RoleAssistant, Content: []ContentBlock{Text(text)}}, StopReason: StopEndTurn, Usage: usage,
	}}
}

func fail(err error) StreamEvent { return StreamEvent{Type: EventError, Err: err} }

var (
	transient = &APIError{StatusCode: 529, Type: "overloaded_error", Message: "Overloaded", Retryable: true}
	permanent = &APIError{StatusCode: 400, Type: "invalid_request_error", Message: "bad"}
)

func (f *fake) Name() string { return "fake" }

func (f *fake) next() step {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.steps[f.calls]
	f.calls++
	return s
}

func (f *fake) Complete(ctx context.Context, req Request) (Response, error) {
	ch, err := f.Stream(ctx, req)
	if err != nil {
		return Response{}, err
	}
	return Collect(ch)
}

func (f *fake) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	s := f.next()
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan StreamEvent, len(s.events))
	for _, ev := range s.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func policy(retries int, delays *[]time.Duration) RetryPolicy {
	p := DefaultRetryPolicy(retries)
	p.Rand = func() float64 { return 0 }
	p.Sleep = func(ctx context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return nil
	}
	return p
}

func TestRetryComplete(t *testing.T) {
	tests := []struct {
		name    string
		steps   []step
		retries int
		wantErr error
		calls   int
		delays  []time.Duration
	}{
		{"succeeds after transient errors",
			[]step{{err: transient}, {events: []StreamEvent{fail(transient)}}, {events: []StreamEvent{done("ok", Usage{})}}},
			5, nil, 3, []time.Duration{500 * time.Millisecond, time.Second}},
		{"gives up after max retries",
			[]step{{err: transient}, {err: transient}, {err: transient}},
			2, transient, 3, []time.Duration{500 * time.Millisecond, time.Second}},
		{"does not retry permanent errors",
			[]step{{err: permanent}}, 5, permanent, 1, nil},
		{"honours retry-after",
			[]step{{err: &APIError{StatusCode: 429, Retryable: true, RetryAfter: 7 * time.Second}}, {events: []StreamEvent{done("ok", Usage{})}}},
			5, nil, 2, []time.Duration{7 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fake{steps: tt.steps}
			var delays []time.Duration
			p := policy(tt.retries, &delays)
			var attempts []int
			p.OnRetry = func(attempt int, err error, d time.Duration) { attempts = append(attempts, attempt) }
			_, err := WithRetry(f, p).Complete(context.Background(), Request{})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if f.calls != tt.calls {
				t.Errorf("calls = %d, want %d", f.calls, tt.calls)
			}
			if len(delays) != len(tt.delays) || (len(delays) > 0 && delays[0] != tt.delays[0]) {
				t.Errorf("delays = %v, want %v", delays, tt.delays)
			}
			if len(attempts) != len(tt.delays) {
				t.Errorf("OnRetry calls = %v", attempts)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: 10 * time.Second, Rand: func() float64 { return 0.999 }}
	for attempt, max := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 5: 10 * time.Second, 40: 10 * time.Second} {
		d := p.Delay(attempt, transient)
		if d < max/2 || d > max {
			t.Errorf("attempt %d: delay %v outside [%v, %v]", attempt, d, max/2, max)
		}
	}
	if d := p.Delay(1, &APIError{RetryAfter: time.Hour}); d != 10*time.Second {
		t.Errorf("retry-after must be capped: %v", d)
	}
}

func TestRetryStream(t *testing.T) {
	delta := StreamEvent{Type: EventTextDelta, Text: "Hel"}
	t.Run("retries until something is delivered", func(t *testing.T) {
		f := &fake{steps: []step{
			{events: []StreamEvent{fail(transient)}},
			{err: transient},
			{events: []StreamEvent{delta, done("Hello", Usage{})}},
		}}
		var delays []time.Duration
		ch, err := WithRetry(f, policy(5, &delays)).Stream(context.Background(), Request{})
		if err != nil {
			t.Fatal(err)
		}
		var types []string
		for ev := range ch {
			types = append(types, string(ev.Type))
		}
		if strings.Join(types, ",") != "text_delta,done" || f.calls != 3 || len(delays) != 2 {
			t.Errorf("events = %v, calls = %d, delays = %v", types, f.calls, delays)
		}
	})
	t.Run("does not retry after delivering", func(t *testing.T) {
		f := &fake{steps: []step{{events: []StreamEvent{delta, fail(transient)}}, {events: []StreamEvent{done("x", Usage{})}}}}
		var delays []time.Duration
		ch, _ := WithRetry(f, policy(5, &delays)).Stream(context.Background(), Request{})
		var last StreamEvent
		for ev := range ch {
			last = ev
		}
		if last.Type != EventError || f.calls != 1 {
			t.Errorf("last = %+v, calls = %d", last, f.calls)
		}
	})
	t.Run("budget of retries is shared", func(t *testing.T) {
		f := &fake{steps: []step{{events: []StreamEvent{fail(transient)}}, {err: transient}, {events: []StreamEvent{fail(transient)}}}}
		var delays []time.Duration
		ch, _ := WithRetry(f, policy(2, &delays)).Stream(context.Background(), Request{})
		var last StreamEvent
		for ev := range ch {
			last = ev
		}
		if last.Type != EventError || f.calls != 3 || len(delays) != 2 {
			t.Errorf("last = %+v, calls = %d, delays = %v", last, f.calls, delays)
		}
	})
	t.Run("stream closed without a result", func(t *testing.T) {
		f := &fake{steps: []step{{events: nil}}}
		var delays []time.Duration
		ch, _ := WithRetry(f, policy(2, &delays)).Stream(context.Background(), Request{})
		var last StreamEvent
		for ev := range ch {
			last = ev
		}
		if last.Type != EventError || f.calls != 1 {
			t.Errorf("last = %+v, calls = %d", last, f.calls)
		}
	})
}

func TestMeter(t *testing.T) {
	u := Usage{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 400, CacheCreationTokens: 10}
	f := &fake{steps: []step{{events: []StreamEvent{done("a", u)}}, {events: []StreamEvent{done("b", u)}}, {events: []StreamEvent{done("c", u)}}}}
	m := &Meter{Budget: 1000}
	p := WithMeter(f, m)
	if _, err := p.Complete(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	got, n := m.Usage()
	if n != 2 || got != u.Add(u) || got.Total() != 1120 {
		t.Errorf("usage = %+v (%d responses)", got, n)
	}
	if _, err := p.Complete(context.Background(), Request{}); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("err = %v", err)
	}
	if f.calls != 2 {
		t.Error("no request may be sent once the budget is spent")
	}
	if err := (&Meter{}).Check(); err != nil {
		t.Errorf("a zero budget is unlimited: %v", err)
	}
}

func TestHelpers(t *testing.T) {
	closed := make(chan StreamEvent)
	close(closed)
	if _, err := Collect(closed); err == nil || !IsRetryable(err) {
		t.Errorf("an empty stream must be a retryable error: %v", err)
	}
	r := InvalidInputResult(ContentBlock{ToolUseID: "t1", Input: json.RawMessage(`"{\"a\": "`)})
	if r.ToolUseID != "t1" || !r.IsError || r.Output != `{"INVALID_JSON":"{\"a\": "}` {
		t.Errorf("result = %+v", r)
	}
	for code, want := range map[int]bool{400: false, 401: false, 408: true, 409: true, 429: true, 500: true, 529: true} {
		if RetryableStatus(code) != want {
			t.Errorf("RetryableStatus(%d) = %v", code, !want)
		}
	}
	e := &APIError{Provider: "anthropic", StatusCode: 429, Type: "rate_limit_error", Message: "slow", RequestID: "req_1"}
	if e.Error() != "anthropic: 429 rate_limit_error: slow (request req_1)" {
		t.Errorf("Error() = %q", e.Error())
	}
}
