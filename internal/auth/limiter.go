package auth

import (
	"sync"
	"time"
)

// LoginLimiter throttles password guessing (ADR-0031 D8): per client IP, the
// first freeAttempts failures are free; each further failure locks that IP out
// for baseDelay doubled per extra failure, capped at maxDelay. A success
// clears the IP. A second, global window stops a guesser who rotates source
// addresses: more than globalMax failures within globalWindow locks every
// login for globalLock.
//
// The key is the TCP peer address. Forwarding headers are never trusted (they
// are client-controlled); behind a same-host reverse proxy every client shares
// the proxy's address and therefore one bucket, which is the safe direction.
type LoginLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	ips     map[string]*ipState
	globals []time.Time // failure times inside the global window
	gLocked time.Time   // global lockout end
}

type ipState struct {
	fails int
	until time.Time // locked out until
	last  time.Time // last failure, for expiry
}

const (
	freeAttempts = 5
	baseDelay    = 2 * time.Second
	maxDelay     = 15 * time.Minute
	forgetAfter  = time.Hour
	maxTracked   = 4096
	globalMax    = 40
	globalWindow = time.Minute
	globalLock   = time.Minute
)

// NewLoginLimiter returns a limiter. now == nil means time.Now.
func NewLoginLimiter(now func() time.Time) *LoginLimiter {
	if now == nil {
		now = time.Now
	}
	return &LoginLimiter{now: now, ips: map[string]*ipState{}}
}

// Allow reports whether ip may attempt a login now; when not, retryAfter is
// how long to wait.
func (l *LoginLimiter) Allow(ip string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Before(l.gLocked) {
		return false, l.gLocked.Sub(now)
	}
	if st := l.ips[ip]; st != nil && now.Before(st.until) {
		return false, st.until.Sub(now)
	}
	return true, 0
}

// Fail records a wrong password from ip.
func (l *LoginLimiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	st := l.ips[ip]
	if st == nil {
		l.evictLocked(now)
		st = &ipState{}
		l.ips[ip] = st
	}
	if now.Sub(st.last) > forgetAfter {
		st.fails = 0
	}
	st.fails++
	st.last = now
	if st.fails > freeAttempts {
		delay := baseDelay
		for i := freeAttempts + 1; i < st.fails && delay < maxDelay; i++ {
			delay *= 2
		}
		if delay > maxDelay {
			delay = maxDelay
		}
		st.until = now.Add(delay)
	}

	cut := now.Add(-globalWindow)
	kept := l.globals[:0]
	for _, t := range l.globals {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.globals = append(kept, now)
	if len(l.globals) > globalMax {
		l.gLocked = now.Add(globalLock)
		l.globals = l.globals[:0]
	}
}

// Success clears ip's failure history.
func (l *LoginLimiter) Success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.ips, ip)
}

// evictLocked makes room for one more tracked address: expired entries first,
// then the one whose lockout ends soonest.
func (l *LoginLimiter) evictLocked(now time.Time) {
	if len(l.ips) < maxTracked {
		return
	}
	var victim string
	var victimUntil time.Time
	first := true
	for ip, st := range l.ips {
		if now.Sub(st.last) > forgetAfter {
			delete(l.ips, ip)
			continue
		}
		if first || st.until.Before(victimUntil) {
			victim, victimUntil, first = ip, st.until, false
		}
	}
	if len(l.ips) >= maxTracked && !first {
		delete(l.ips, victim)
	}
}
