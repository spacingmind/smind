package bridge

// password.go — the daemon password for relay devices (ADR-0031 D9 "Daemon
// password (P13c)", decision 14). With a password set (internal/server, P4),
// every device_hello must carry it, inside the encrypted channel and outside
// the signature, before anything about the device is recorded or revealed.

import (
	"crypto/subtle"
	"math"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/auth"
)

// Error codes of a refused password check (the response's error.code).
const (
	codePasswordRequired  = "password_required"
	codeIncorrectPassword = "incorrect_password"
	codeTooManyAttempts   = "too_many_attempts"
)

// maxHelloPassword bounds the password field: bcrypt only reads 72 bytes, so
// anything beyond a small multiple is garbage, not a candidate to hash.
const maxHelloPassword = 128

// PasswordGate is the daemon password as the hub sees it. A nil gate, or one
// that is not Required, means devices are admitted without a password.
type PasswordGate interface {
	// Required reports whether a password is configured.
	Required() bool
	// Check reports whether password matches it (constant time with respect to
	// the password).
	Check(password string) bool
}

// StaticPassword is a PasswordGate for a fixed clear password, with a
// constant-time comparison: for the test harnesses (the daemon itself uses its
// bcrypt hash through internal/server). "" means no password is required.
func StaticPassword(password string) PasswordGate { return staticPassword(password) }

type staticPassword string

func (p staticPassword) Required() bool { return p != "" }
func (p staticPassword) Check(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(p)) == 1
}

// passwordSlots bounds concurrent bcrypt checks: each costs hundreds of
// milliseconds of CPU and the limiter only learns of a failure afterwards.
const passwordSlots = 4

// passwordState is the hub's per-daemon password machinery.
type passwordState struct {
	limiter  *auth.LoginLimiter
	slots    chan struct{}
	warnOnce sync.Once
}

func newPasswordState(now func() time.Time) *passwordState {
	return &passwordState{limiter: auth.NewLoginLimiter(now), slots: make(chan struct{}, passwordSlots)}
}

func (h *Hub) passwordRequired() bool {
	g := h.opts.Password
	return g != nil && g.Required()
}

// checkPassword decides a hello's password. ok is true when the device may
// proceed; otherwise code/msg say why and retryAfter (seconds, 0 = none) tells
// a rate-limited client when to come back. Wrong attempts are counted per
// device id (and by the limiter's global window across all devices).
func (h *Hub) checkPassword(deviceID, password string) (ok bool, code, msg string, retryAfter int) {
	st := h.pw
	if allowed, wait := st.limiter.Allow(deviceID); !allowed {
		return false, codeTooManyAttempts, "too many wrong passwords; wait before trying again", ceilSeconds(wait)
	}
	if password == "" {
		return false, codePasswordRequired, "this daemon requires its password: enter it in the app", 0
	}
	if len(password) > maxHelloPassword {
		st.limiter.Fail(deviceID)
		return false, codeIncorrectPassword, "incorrect password", 0
	}
	select {
	case st.slots <- struct{}{}:
		defer func() { <-st.slots }()
	default:
		return false, codeTooManyAttempts, "the daemon is busy checking passwords; try again shortly", 1
	}
	if !h.opts.Password.Check(password) {
		st.limiter.Fail(deviceID)
		return false, codeIncorrectPassword, "incorrect password", 0
	}
	st.limiter.Success(deviceID)
	return true, "", "", 0
}

func ceilSeconds(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		return 1
	}
	return s
}

// warnNoPassword logs one line, once per hub, when relay devices can connect
// but the daemon has no password: approval is then the only gate.
func (h *Hub) warnNoPassword() {
	if h.passwordRequired() {
		return
	}
	h.pw.warnOnce.Do(func() {
		h.logf("relay: WARNING: no daemon password is set, so a relay device is admitted by approval alone; set one with `smind set-password` (and restart) to require it from every device")
	})
}
