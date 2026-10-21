package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// HTTPError is a non-2xx response from a hosted API.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("status %d: %s", e.StatusCode, e.Body)
}

// keyFault says why a request failed when the fault lies with the API key
// itself, so another key could succeed.
type keyFault int

const (
	faultNone    keyFault = iota // not the key's fault: don't rotate
	faultLimited                 // throttled or overloaded
	faultBilling                 // out of credit
	faultAuth                    // rejected
)

// classifyKeyFault decides whether an error is the key's fault. HTTP status
// decides when known; otherwise a few unambiguous phrases do. Timeouts,
// network errors and malformed requests fail the same way on every key.
func classifyKeyFault(err error) keyFault {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return faultNone
	}
	var he *HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case 429, 529:
			return faultLimited
		case 402:
			return faultBilling
		case 401, 403:
			return faultAuth
		}
		return faultNone
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"), strings.Contains(msg, "overloaded"):
		return faultLimited
	case strings.Contains(msg, "credit balance"), strings.Contains(msg, "insufficient quota"):
		return faultBilling
	case strings.Contains(msg, "invalid api key"), strings.Contains(msg, "invalid x-api-key"):
		return faultAuth
	}
	return faultNone
}

// keyState is the health and pacing of one key.
type keyState struct {
	strikes  int       // consecutive faults; reset on success
	benched  time.Time // not tried before this time unless every key is benched
	nextSlot time.Time // earliest start of the next request under the RPM cap
}

// bench returns how long a key sits out after its nth consecutive fault:
// doubling from 30s (capped at 30m), or from 1h (capped at 12h) for billing.
func bench(f keyFault, strikes int) time.Duration {
	base, max := 30*time.Second, 30*time.Minute
	if f == faultBilling {
		base, max = time.Hour, 12*time.Hour
	}
	d := base
	for i := 1; i < strikes && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

// KeyPool spreads requests for one hosted provider across its API keys.
// A key that is throttled, out of credit, or rejected is benched for a
// while and the request moves to the next key. With rpm > 0, requests on
// each key are spaced evenly to stay under that many per minute.
//
// An empty key ("") stands for the provider SDK's own credential lookup
// (environment variables or a login profile).
type KeyPool struct {
	provider string
	keys     []string
	spacing  time.Duration // minimum gap between requests on one key; 0 = none
	now      func() time.Time

	mu     sync.Mutex
	states []keyState
	cursor int // round-robin start
}

// NewKeyPool builds a pool for provider. rpm <= 0 means no local rate limit.
func NewKeyPool(provider string, keys []string, rpm int) *KeyPool {
	if len(keys) == 0 {
		keys = []string{""}
	}
	p := &KeyPool{
		provider: provider,
		keys:     keys,
		states:   make([]keyState, len(keys)),
		now:      time.Now,
	}
	if rpm > 0 {
		p.spacing = time.Minute / time.Duration(rpm)
	}
	return p
}

// Len returns the number of keys in the pool.
func (p *KeyPool) Len() int { return len(p.keys) }

// candidates returns key indexes to try this round: keys in play first,
// round-robin from the cursor, then benched keys soonest-back first.
func (p *KeyPool) candidates() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	start := p.cursor
	p.cursor = (p.cursor + 1) % len(p.keys)

	var ready, benched []int
	for n := range p.keys {
		i := (start + n) % len(p.keys)
		if now.Before(p.states[i].benched) {
			benched = append(benched, i)
		} else {
			ready = append(ready, i)
		}
	}
	for a := 1; a < len(benched); a++ {
		for b := a; b > 0 && p.states[benched[b]].benched.Before(p.states[benched[b-1]].benched); b-- {
			benched[b], benched[b-1] = benched[b-1], benched[b]
		}
	}
	return append(ready, benched...)
}

// pace reserves key i's next request slot under the RPM cap and waits for
// it, or returns early if ctx ends.
func (p *KeyPool) pace(ctx context.Context, i int) error {
	if p.spacing == 0 {
		return nil
	}
	p.mu.Lock()
	now := p.now()
	slot := p.states[i].nextSlot
	if slot.Before(now) {
		slot = now
	}
	p.states[i].nextSlot = slot.Add(p.spacing)
	p.mu.Unlock()

	if wait := slot.Sub(now); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

func (p *KeyPool) record(i int, f keyFault) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := &p.states[i]
	if f == faultNone {
		s.strikes, s.benched = 0, time.Time{}
		return
	}
	s.strikes++
	s.benched = p.now().Add(bench(f, s.strikes))
}

// Do calls run with each key in turn until one succeeds or an error occurs
// that another key would not fix. run must not have produced any output for
// the caller before returning an error, or the retry would duplicate it.
func (p *KeyPool) Do(ctx context.Context, run func(key string) error) error {
	var lastErr error
	for _, i := range p.candidates() {
		if err := p.pace(ctx, i); err != nil {
			return err
		}
		err := run(p.keys[i])
		if err == nil {
			p.record(i, faultNone)
			return nil
		}
		f := classifyKeyFault(err)
		if f == faultNone {
			return err
		}
		p.record(i, f)
		lastErr = err
	}
	if len(p.keys) > 1 {
		return fmt.Errorf("all %d %s keys failed; last error: %w", len(p.keys), p.provider, lastErr)
	}
	return lastErr
}
