package console

import (
	"context"
	"errors"
	"sync"
	"time"
)

// oidcGeneration is immutable after publication. Its context bounds every
// retained request and task to the credentials that created its transport.
type oidcGeneration struct {
	runtime sessionRuntime
	ctx     context.Context
	cancel  context.CancelFunc
	expires time.Time
	number  uint64
}

type oidcGenerations struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	deadline time.Time
	current  *oidcGeneration
	all      []*oidcGeneration
	flight   chan struct{}
	closed   bool
}

var errOIDCSession = errors.New("OIDC session unavailable")

func newOIDCGenerations(parent context.Context, runtime sessionRuntime, expires, deadline time.Time) *oidcGenerations {
	ctx, cancel := context.WithDeadline(parent, deadline)
	m := &oidcGenerations{ctx: ctx, cancel: cancel, deadline: deadline}
	if expires.After(deadline) {
		expires = deadline
	}
	genctx, gencancel := context.WithDeadline(ctx, expires)
	m.current = &oidcGeneration{runtime: runtime, ctx: genctx, cancel: gencancel, expires: expires, number: 1}
	m.all = []*oidcGeneration{m.current}
	return m
}

func (m *oidcGenerations) acquire() (*oidcGeneration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.current
	if m.closed || m.ctx.Err() != nil || g.ctx.Err() != nil || !time.Now().Before(g.expires) || !g.runtime.retain() {
		return nil, errOIDCSession
	}
	return g, nil
}

// refresh coalesces concurrent callers. build never runs under a lock. close can
// therefore cancel a slow exchange, and no late candidate can resurrect a login.
func (m *oidcGenerations) refresh(ctx context.Context, build func(context.Context) (sessionRuntime, time.Time, error)) error {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil || m.current.ctx.Err() != nil {
		m.mu.Unlock()
		return errOIDCSession
	}
	if flight := m.flight; flight != nil {
		m.mu.Unlock()
		select {
		case <-flight:
		case <-ctx.Done():
			return ctx.Err()
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed || m.ctx.Err() != nil {
			return errOIDCSession
		}
		return nil
	}
	old := m.current
	flight := make(chan struct{})
	m.flight = flight
	m.mu.Unlock()
	exchange, cancel := context.WithDeadline(m.ctx, old.expires)
	stop := context.AfterFunc(ctx, cancel)
	candidate, expires, err := build(exchange)
	stop()
	cancel()
	m.mu.Lock()
	if err == nil && (m.closed || m.ctx.Err() != nil || ctx.Err() != nil || m.current != old || !time.Now().Before(old.expires) || !time.Now().Before(expires)) {
		err = errOIDCSession
	}
	if err == nil {
		if expires.After(m.deadline) {
			expires = m.deadline
		}
		genctx, gencancel := context.WithDeadline(m.ctx, expires)
		next := &oidcGeneration{runtime: candidate, ctx: genctx, cancel: gencancel, expires: expires, number: old.number + 1}
		m.current = next
		m.all = append(m.all, next)
	}
	// Publish failure atomically before waking waiters. A failed exchange must
	// never leave a window where a waiter observes success on the old login.
	var failedGenerations []*oidcGeneration
	if err != nil {
		m.closed = true
		m.cancel()
		failedGenerations = append([]*oidcGeneration(nil), m.all...)
	}
	m.flight = nil
	close(flight)
	m.mu.Unlock()
	if err != nil {
		for _, g := range failedGenerations {
			g.cancel()
			g.runtime.retire()
		}
		candidate.retire()
		return err
	}
	old.runtime.retire()
	return nil
}

func (m *oidcGenerations) close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	generations := append([]*oidcGeneration(nil), m.all...)
	m.mu.Unlock()
	for _, g := range generations {
		g.cancel()
		g.runtime.retire()
	}
}
