// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// backendLifetime prevents revocation from closing the transport while an HTTP
// request or background task is still finishing (including multipart abort).
// Cleanup runs outside Server.mu and exactly once, after the final lease ends.
type backendLifetime struct {
	mu      sync.Mutex
	refs    int
	retired bool
	cleanup func()
}

func (r sessionRuntime) retain() bool {
	if r.lifetime == nil {
		return true
	}
	l := r.lifetime
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.retired {
		return false
	}
	l.refs++
	return true
}

func (r sessionRuntime) release() {
	if r.lifetime == nil {
		return
	}
	l := r.lifetime
	l.mu.Lock()
	l.refs--
	cleanup := l.takeCleanupLocked()
	l.mu.Unlock()
	if cleanup != nil {
		go cleanup()
	}
}

func (r sessionRuntime) retire() {
	if r.lifetime == nil {
		return
	}
	l := r.lifetime
	l.mu.Lock()
	l.retired = true
	cleanup := l.takeCleanupLocked()
	l.mu.Unlock()
	if cleanup != nil {
		go cleanup()
	}
}

func (l *backendLifetime) takeCleanupLocked() func() {
	if !l.retired || l.refs != 0 {
		return nil
	}
	cleanup := l.cleanup
	l.cleanup = nil
	return cleanup
}

func (s *Server) newSessionRuntime(ctx context.Context, credentials NativeCredentials) (sessionRuntime, error) {
	runtime := sessionRuntime{backend: s.backend, writer: s.writer, settings: s.settings, preferences: s.preferences}
	if s.backendFactory == nil && s.nativeLogin == nil {
		return runtime, nil
	}
	if err := tryAcquire(ctx, s.loginSlots); err != nil {
		return sessionRuntime{}, err
	}
	defer func() { <-s.loginSlots }()
	// Register pending creation before Close can start draining owned clients.
	s.mu.Lock()
	if s.closed || s.rotating || ctx.Err() != nil {
		s.mu.Unlock()
		return sessionRuntime{}, errors.New("session creation unavailable")
	}
	s.clients.Add(1)
	s.mu.Unlock()
	var backend consoleapi.Backend
	var cleanup func()
	var err error
	if s.nativeLogin != nil {
		var connection NativeConnection
		authCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		connection, err = s.nativeLogin(authCtx, credentials)
		credentials.SecretKey = ""
		if err == nil && authCtx.Err() != nil {
			err = authCtx.Err()
		}
		backend, cleanup, runtime.identity = connection.Backend, connection.Cleanup, connection.Identity
	} else {
		backend, cleanup, err = s.backendFactory(ctx)
	}
	accepted := false
	defer func() {
		if !accepted {
			if cleanup != nil {
				cleanup()
			}
			s.clients.Done()
		}
	}()
	if err != nil {
		return sessionRuntime{}, err
	}
	if backend == nil || cleanup == nil {
		return sessionRuntime{}, errors.New("session factory returned an incomplete connection")
	}
	if err := ctx.Err(); err != nil {
		return sessionRuntime{}, err
	}
	if s.nativeLogin != nil {
		_, identityErr := hex.DecodeString(runtime.identity)
		if len(runtime.identity) != 64 || identityErr != nil {
			return sessionRuntime{}, errors.New("authentication returned an invalid principal identity")
		}
	}
	runtime.backend = backend
	runtime.writer = nil
	if s.allowWrites {
		var ok bool
		runtime.writer, ok = backend.(consoleapi.MutationBackend)
		if !ok {
			return sessionRuntime{}, errors.New("session connection does not support writes")
		}
	}
	runtime.settings, _ = backend.(consoleapi.SettingsBackend)
	runtime.lifetime = &backendLifetime{cleanup: func() { defer s.clients.Done(); cleanup() }}
	accepted = true
	return runtime, nil
}
