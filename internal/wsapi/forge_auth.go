package wsapi

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/workspace"
)

// The GitHub sign-in surface (ADR-0039 section 8): forge.status,
// forge.setToken, forge.logout. Auth is delegated to gh; smind stores no
// token. The one secret on this surface is the pasted token, which this file
// reads from the request, hands to the provider (stdin only) and never
// logs, publishes, wraps in an error or returns. Handlers here read their
// params with typed structs and report parse failures with fixed sentences:
// a json error can quote input, a token must never ride in one.

const (
	forgeStatusTTL      = 60 * time.Second
	forgeRefreshFloor   = 5 * time.Second
	forgeRefreshPerHour = 12
	forgeStatusTimeout  = 15 * time.Second
	forgeLoginTimeout   = 30 * time.Second
	forgeGitTimeout     = 5 * time.Second

	// defaultLoginHost is the host forge.setToken signs in to when the
	// request names none.
	defaultLoginHost = "github.com"
)

// tokenOK is the accepted token alphabet: printable ASCII, no space, 1-255.
func tokenOK(t string) bool {
	if t == "" || len(t) > 255 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] < 0x21 || t[i] > 0x7e {
			return false
		}
	}
	return true
}

// forgeStatusBody is forge.status's result and (without canSetToken) the
// forge.status event's `status`.
type forgeStatusBody struct {
	Provider    string           `json:"provider"`
	CanSetToken *bool            `json:"canSetToken,omitempty"`
	CLI         forge.CLIInfo    `json:"cli"`
	Hosts       []forge.AuthHost `json:"hosts"`
}

// forgeStatusEvent is the forge.status topic payload.
type forgeStatusEvent struct {
	Status forgeStatusBody `json:"status"`
}

// forgeStatusResult wraps a status as forge.setToken/forge.logout's result.
type forgeStatusResult struct {
	Status forgeStatusBody `json:"status"`
}

type statusFlight struct {
	done chan struct{}
	st   forge.AuthStatus
	err  error
}

// forgeAuth reads and changes the daemon host's forge login. Status is cached
// for 60 s and single-flight; explicit refreshes are rate limited.
type forgeAuth struct {
	gh  *githubHolder
	bus *eventBus
	now func() time.Time

	// git is the git state service whose poller must hear about a login
	// change; set once after construction.
	git *gitstate.Service

	mu         sync.Mutex
	have       bool
	cached     forge.AuthStatus
	cachedAt   time.Time
	flight     *statusFlight
	pubKey     string // canonical key of the last status seen
	lastForced time.Time
	forced     []time.Time

	changeMu sync.Mutex // one login or logout at a time
}

func newForgeAuth(gh *githubHolder, bus *eventBus) *forgeAuth {
	return &forgeAuth{gh: gh, bus: bus, now: time.Now}
}

func (f *forgeAuth) provider() forge.Provider {
	if f.gh == nil || f.gh.c == nil {
		return nil
	}
	return f.gh.c
}

// status returns the cached status, or reads it. refresh asks to bypass the
// cache and is honoured only inside the floor and hourly cap; over them the
// cached answer is returned (a refresh button must not fail for pressing it
// twice).
func (f *forgeAuth) status(ctx context.Context, refresh bool) (forge.AuthStatus, error) {
	f.mu.Lock()
	now := f.now()
	if refresh && f.have && f.refreshAllowedLocked(now) {
		f.stampForcedLocked(now)
		f.have = false
	} else if !f.have || now.Sub(f.cachedAt) >= forgeStatusTTL {
		f.have = false
	}
	if f.have {
		st := f.cached
		f.mu.Unlock()
		return st, nil
	}
	return f.readLocked(ctx)
}

// readLocked joins or starts the one in-flight read. Called with f.mu held;
// it releases it.
func (f *forgeAuth) readLocked(ctx context.Context) (forge.AuthStatus, error) {
	fl := f.flight
	if fl == nil {
		fl = &statusFlight{done: make(chan struct{})}
		f.flight = fl
		go f.read(fl)
	}
	f.mu.Unlock()
	select {
	case <-fl.done:
		return fl.st, fl.err
	case <-ctx.Done():
		return forge.AuthStatus{}, forge.NewError(forge.CodeTimeout, "")
	}
}

// read runs the provider call on a context of its own, so one caller giving
// up does not fail the callers sharing the flight.
func (f *forgeAuth) read(fl *statusFlight) {
	ctx, cancel := context.WithTimeout(context.Background(), forgeStatusTimeout)
	defer cancel()
	var st forge.AuthStatus
	var err error
	if p := f.provider(); p == nil {
		st = forge.AuthStatus{Hosts: []forge.AuthHost{}}
	} else {
		st, err = p.Status(ctx)
	}
	f.mu.Lock()
	var publish *forge.AuthStatus
	if err == nil {
		f.cached, f.cachedAt, f.have = st, f.now(), true
		key := statusKey(st)
		if f.pubKey != "" && f.pubKey != key {
			publish = &st
		}
		f.pubKey = key
	}
	f.flight = nil
	f.mu.Unlock()
	fl.st, fl.err = st, err
	close(fl.done)
	if publish != nil && f.bus != nil {
		f.bus.Publish(Event{Topic: TopicForgeStatus, Payload: forgeStatusEvent{Status: bodyOf(*publish, nil)}})
	}
}

func (f *forgeAuth) refreshAllowedLocked(now time.Time) bool {
	if !f.lastForced.IsZero() && now.Sub(f.lastForced) < forgeRefreshFloor {
		return false
	}
	n := 0
	for _, t := range f.forced {
		if now.Sub(t) < time.Hour {
			n++
		}
	}
	return n < forgeRefreshPerHour
}

func (f *forgeAuth) stampForcedLocked(now time.Time) {
	f.lastForced = now
	kept := f.forced[:0]
	for _, t := range f.forced {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	f.forced = append(kept, now)
}

// invalidate drops the cache so the next status call reads gh.
func (f *forgeAuth) invalidate() {
	f.mu.Lock()
	f.have = false
	f.mu.Unlock()
}

func statusKey(st forge.AuthStatus) string {
	b, _ := json.Marshal(st)
	return string(b)
}

func bodyOf(st forge.AuthStatus, canSet *bool) forgeStatusBody {
	hosts := st.Hosts
	if hosts == nil {
		hosts = []forge.AuthHost{}
	}
	return forgeStatusBody{Provider: "github", CanSetToken: canSet, CLI: st.CLI, Hosts: hosts}
}

// canSetToken is whether this connection may send or change a credential.
func canSetToken(rc *requestContext) bool {
	return rc != nil && !rc.runScoped && rc.channel.Secure()
}

// authenticatedFor reports whether the forge is signed in to host, from the
// cached status (one gh read at most per cache lifetime).
func (f *forgeAuth) authenticatedFor(ctx context.Context, host string) bool {
	st, err := f.status(ctx, false)
	return err == nil && st.Authenticated(host)
}

// networkGit is workspace.NetworkGitConfig / gitstate.Options.NetworkGit: the
// per-invocation credential helper for a network git call, only when
// remote's URL is https and the forge is signed in to its host. It reads the
// URL with `git remote get-url` (so url.insteadOf applies, as it does to the
// connection) and edits no configuration.
func (f *forgeAuth) networkGit(ctx context.Context, dir, remote string) []string {
	if remote == "" || strings.HasPrefix(remote, "-") {
		return nil
	}
	gctx, cancel := context.WithTimeout(ctx, forgeGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(gctx, "git", "-c", "core.fsmonitor=false", "remote", "get-url", "--", remote)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	host, ok := forge.HTTPSHost(string(out))
	if !ok || !f.authenticatedFor(gctx, host) {
		return nil
	}
	return forge.CredentialHelperArgs()
}

// NetworkGitConfig is the hook cmd/smind installs with
// workspace.SetNetworkGitConfig, so the daemon's own push and fetch calls use
// the forge login over https.
func (a *API) NetworkGitConfig() workspace.NetworkGitConfig { return a.forgeAuth.networkGit }

// handleForgeStatus serves forge.status {refresh?}. Readable by every
// caller; a run-scoped caller's refresh is ignored (it reads the cache).
func handleForgeStatus(f *forgeAuth) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			Refresh bool `json:"refresh"`
		}
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, &rpcCodeError{code: codeInvalid, msg: "forge.status: invalid params"}
			}
		}
		st, err := f.status(ctx, p.Refresh && rc != nil && !rc.runScoped)
		if err != nil {
			return nil, err
		}
		can := canSetToken(rc)
		return bodyOf(st, &can), nil
	}
}

// guardForgeChange is the checks both mutating RPCs share, run before any
// param is parsed and before anything is spawned: agents are refused, and so
// is every channel a credential must not cross.
func guardForgeChange(method string, rc *requestContext) error {
	if rc == nil || rc.runScoped {
		return &rpcCodeError{code: codeForbidden, msg: method + " is not available to agents"}
	}
	if !rc.channel.Secure() {
		return forge.NewError(forge.CodeInsecureChannel, "")
	}
	return nil
}

// handleForgeSetToken serves forge.setToken {host?, token}: gh auth login
// --with-token, token on stdin only.
func handleForgeSetToken(f *forgeAuth) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := guardForgeChange("forge.setToken", rc); err != nil {
			return nil, err
		}
		var p struct {
			Host  string `json:"host"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			// Fixed sentence: a json error could quote part of the input.
			return nil, &rpcCodeError{code: codeInvalid, msg: "forge.setToken: invalid params"}
		}
		if p.Host == "" {
			p.Host = defaultLoginHost
		}
		if !forge.ValidHost(p.Host) {
			return nil, &rpcCodeError{code: codeInvalid, msg: "forge.setToken: invalid host"}
		}
		if !tokenOK(p.Token) {
			// Not sent to gh: it cannot be a token. No detail, no echo.
			return nil, forge.NewError(forge.CodeTokenInvalid, "")
		}
		prov := f.provider()
		if prov == nil {
			return nil, forge.NewError(forge.CodeCLIMissing, "")
		}
		f.changeMu.Lock()
		defer f.changeMu.Unlock()
		f.baseline(ctx)
		lctx, cancel := context.WithTimeout(ctx, forgeLoginTimeout)
		defer cancel()
		if err := prov.Login(lctx, p.Host, p.Token); err != nil {
			return nil, scrubToken(err, p.Token)
		}
		return f.afterChange(ctx, rc, p.Host)
	}
}

// handleForgeLogout serves forge.logout {host}.
func handleForgeLogout(f *forgeAuth) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := guardForgeChange("forge.logout", rc); err != nil {
			return nil, err
		}
		var p struct {
			Host string `json:"host"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || !forge.ValidHost(p.Host) {
			return nil, &rpcCodeError{code: codeInvalid, msg: "forge.logout: invalid params"}
		}
		prov := f.provider()
		if prov == nil {
			return nil, forge.NewError(forge.CodeCLIMissing, "")
		}
		f.changeMu.Lock()
		defer f.changeMu.Unlock()
		f.baseline(ctx)
		lctx, cancel := context.WithTimeout(ctx, forgeLoginTimeout)
		defer cancel()
		if err := prov.Logout(lctx, p.Host); err != nil {
			return nil, err
		}
		return f.afterChange(ctx, rc, p.Host)
	}
}

// baseline makes sure a status has been seen before a change, so the change
// is a difference worth publishing even when nobody asked for the status yet.
func (f *forgeAuth) baseline(ctx context.Context) {
	f.mu.Lock()
	seen := f.pubKey != ""
	f.mu.Unlock()
	if !seen {
		_, _ = f.status(ctx, false)
	}
}

// afterChange re-reads the status after a login or logout, tells the poller
// (so a host suspension and a forge_unauthenticated blocker lift at once) and
// returns the new status.
func (f *forgeAuth) afterChange(ctx context.Context, rc *requestContext, host string) (any, error) {
	f.invalidate()
	f.git.ForgeAuthChanged(host)
	st, err := f.status(ctx, false)
	if err != nil {
		return nil, err
	}
	can := canSetToken(rc)
	return forgeStatusResult{Status: bodyOf(st, &can)}, nil
}

// scrubToken is the last line of defence on an error that is about to cross
// the wire: a typed forge error keeps its fixed message and a detail with the
// token cut out; anything else collapses to a fixed forge_error, so no
// untyped text from below can carry the value out.
func scrubToken(err error, token string) error {
	if fe, ok := err.(*forge.Error); ok {
		cp := *fe
		cp.Detail = forge.ClipDetail(strings.ReplaceAll(cp.Detail, token, "[redacted]"))
		return &cp
	}
	return forge.NewError(forge.CodeError, "")
}
