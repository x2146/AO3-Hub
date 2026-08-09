package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxConcurrentPasswordChecks = 2
	maxFailedLoginAttempts      = 8
	loginAttemptWindow          = 5 * time.Minute
	maxTrackedLoginKeys         = 4096
)

var (
	errLoginRateLimited  = errors.New("login rate limited")
	errPasswordCheckBusy = errors.New("password check capacity exhausted")
)

type loginAttemptState struct {
	failures int
	since    time.Time
	lastSeen time.Time
}

type loginAttemptGuard struct {
	slots    chan struct{}
	mu       sync.Mutex
	attempts map[string]loginAttemptState
}

func newLoginAttemptGuard() *loginAttemptGuard {
	return &loginAttemptGuard{
		slots:    make(chan struct{}, maxConcurrentPasswordChecks),
		attempts: map[string]loginAttemptState{},
	}
}

func (a *App) authLoginGuard() *loginAttemptGuard {
	a.loginOnce.Do(func() {
		a.loginGuard = newLoginAttemptGuard()
	})
	return a.loginGuard
}

func loginAttemptKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.ToLower(strings.TrimSpace(host)) + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

func (g *loginAttemptGuard) Begin(ctx context.Context, key string) (func(bool), error) {
	if err := g.checkRateLimit(key, time.Now()); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	select {
	case g.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, errPasswordCheckBusy
	}

	now := time.Now()
	if err := g.checkRateLimit(key, now); err != nil {
		<-g.slots
		return nil, err
	}

	var once sync.Once
	return func(success bool) {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			now := time.Now()
			if success {
				delete(g.attempts, key)
			} else {
				state := g.attempts[key]
				if state.since.IsZero() || now.Sub(state.since) >= loginAttemptWindow {
					state = loginAttemptState{since: now}
				}
				state.failures++
				state.lastSeen = now
				g.attempts[key] = state
			}
			<-g.slots
		})
	}, nil
}

func (g *loginAttemptGuard) checkRateLimit(key string, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked(now)
	state := g.attempts[key]
	if !state.since.IsZero() && now.Sub(state.since) < loginAttemptWindow && state.failures >= maxFailedLoginAttempts {
		return errLoginRateLimited
	}
	return nil
}

func (g *loginAttemptGuard) pruneLocked(now time.Time) {
	for key, state := range g.attempts {
		if now.Sub(state.lastSeen) >= loginAttemptWindow {
			delete(g.attempts, key)
		}
	}
	if len(g.attempts) < maxTrackedLoginKeys {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, state := range g.attempts {
		if oldestKey == "" || state.lastSeen.Before(oldest) {
			oldestKey = key
			oldest = state.lastSeen
		}
	}
	delete(g.attempts, oldestKey)
}
