package console

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestOIDCGenerationLeaseAndExpiry(t *testing.T) {
	var cleanup atomic.Int32
	old := sessionRuntime{lifetime: &backendLifetime{cleanup: func() { cleanup.Add(1) }}}
	expiry := time.Now().Add(80 * time.Millisecond)
	m := newOIDCGenerations(context.Background(), old, expiry, time.Now().Add(time.Second))
	lease, err := m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.refresh(context.Background(), func(context.Context) (sessionRuntime, time.Time, error) {
		return sessionRuntime{}, time.Now().Add(time.Second), nil
	}); err != nil {
		t.Fatal(err)
	}
	if cleanup.Load() != 0 {
		t.Fatal("retired transport closed during retained request")
	}
	next, err := m.acquire()
	if err != nil || next.number != 2 {
		t.Fatal("replacement unavailable", err)
	}
	next.runtime.release()
	select {
	case <-lease.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("old credential request crossed expiry")
	}
	if next.ctx.Err() != nil {
		t.Fatal("old expiry cancelled replacement")
	}
	lease.runtime.release()
	m.close()
	deadline := time.After(time.Second)
	for cleanup.Load() != 1 {
		select {
		case <-deadline:
			t.Fatal("cleanup not exactly once")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestOIDCGenerationRefreshCloseRejectsLateCandidate(t *testing.T) {
	m := newOIDCGenerations(context.Background(), sessionRuntime{}, time.Now().Add(time.Second), time.Now().Add(time.Second))
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan error)
	var cleanup atomic.Int32
	go func() {
		done <- m.refresh(context.Background(), func(context.Context) (sessionRuntime, time.Time, error) {
			close(started)
			<-finish
			return sessionRuntime{lifetime: &backendLifetime{cleanup: func() { cleanup.Add(1) }}}, time.Now().Add(time.Second), nil
		})
	}()
	<-started
	m.close()
	close(finish)
	if err := <-done; err == nil {
		t.Fatal("late refresh committed after logout")
	}
	if _, err := m.acquire(); err == nil {
		t.Fatal("closed login resurrected")
	}
	deadline := time.After(time.Second)
	for cleanup.Load() != 1 {
		select {
		case <-deadline:
			t.Fatal("candidate cleanup missing")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestOIDCGenerationRefreshSingleFlight(t *testing.T) {
	m := newOIDCGenerations(context.Background(), sessionRuntime{}, time.Now().Add(time.Second), time.Now().Add(time.Second))
	defer m.close()
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	build := func(context.Context) (sessionRuntime, time.Time, error) {
		calls.Add(1)
		close(started)
		<-finish
		return sessionRuntime{}, time.Now().Add(time.Second), nil
	}
	done := make(chan error, 2)
	go func() { done <- m.refresh(context.Background(), build) }()
	<-started
	// Wait until the second caller has observed the active exchange, without
	// relying on a scheduler sleep: its context cancellation releases the wait.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- m.refresh(ctx, build) }()
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("cancelled waiter did not stop")
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate refresh exchange")
	}
}

func TestOIDCGenerationRefreshCannotExtendFixedDeadline(t *testing.T) {
	deadline := time.Now().Add(100 * time.Millisecond)
	m := newOIDCGenerations(context.Background(), sessionRuntime{}, time.Now().Add(60*time.Millisecond), deadline)
	defer m.close()
	if err := m.refresh(context.Background(), func(context.Context) (sessionRuntime, time.Time, error) {
		return sessionRuntime{}, time.Now().Add(time.Hour), nil
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.runtime.release()
	if !lease.expires.Equal(deadline) {
		t.Fatal("fixed deadline extended")
	}
	select {
	case <-lease.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("session failed to expire")
	}
	if _, err := m.acquire(); err == nil {
		t.Fatal("expired session accepted operation")
	}
}
