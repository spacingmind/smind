package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/plugins/host"
)

// plugin.call routing (ADR-0042 section 4, PB.4): a client asks a plugin
// process to run one of the RPCs its manifest declares in contributes.rpc. The
// call is `rpc.<name>` on the plugin's stdio connection and nothing else is
// ever forwarded: an undeclared name is answered here, typed, without touching
// the process.

// Typed codes of a failed call. They cross the wsapi unchanged.
const (
	CodeUnknownRPC        = "unknown_rpc"        // the name is not declared in contributes.rpc
	CodePluginUnavailable = "plugin_unavailable" // not enabled, switch off, not running, failed or gone mid-call
	CodeCallTimeout       = "call_timeout"       // no answer within the timeout
	CodeCallTooLarge      = "call_too_large"     // params or result over the cap (a result is dropped)
	CodePluginBusy        = "plugin_busy"        // the per-plugin concurrency cap is reached
	CodePluginError       = "plugin_error"       // the plugin answered with an error
	CodeBadCall           = "bad_call"           // params are not JSON
)

// Call limits. MaxCallTimeout equals the host's own request timeout, so a
// manifest can lower the timeout and never raise it.
const (
	MaxCallTimeout    = 30 * time.Second
	MaxCallParams     = 64 << 10
	MaxCallResult     = 256 << 10
	MaxCallsPerPlugin = 4
	maxCallErrChars   = 300
	maxLogPreview     = 160
)

// callGate bounds the calls in flight per plugin. A full gate refuses at once
// (typed plugin_busy) instead of queueing behind a slow plugin.
type callGate struct {
	mu sync.Mutex
	n  map[string]int
}

func (g *callGate) enter(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.n == nil {
		g.n = map[string]int{}
	}
	if g.n[id] >= MaxCallsPerPlugin {
		return false
	}
	g.n[id]++
	return true
}

func (g *callGate) leave(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.n[id]--; g.n[id] <= 0 {
		delete(g.n, id)
	}
}

// Call runs the declared RPC `rpc` of plugin id with params (JSON, an empty
// value means {}) and returns the plugin's result, verbatim JSON within the
// size cap. Every failure is a typed *Error whose message names the plugin.
func (m *Manager) Call(ctx context.Context, id, rpc string, params json.RawMessage) (json.RawMessage, error) {
	fail := func(code, format string, a ...any) error {
		return newErr(code, "plugin %s: "+format, append([]any{id}, a...)...)
	}
	if m.proc == nil || !m.GlobalEnabled() {
		return nil, fail(CodePluginUnavailable, "plugins are not running")
	}
	var info *Info
	for _, in := range m.Active() {
		if in.ID == id {
			in := in
			info = &in
			break
		}
	}
	if info == nil {
		return nil, fail(CodePluginUnavailable, "it is not installed and enabled")
	}
	man := m.loadedManifest(*info)
	if man == nil || man.Process == nil {
		return nil, fail(CodePluginUnavailable, "it has no process")
	}
	var decl *RPCContribution
	for i := range man.Contributes.RPC {
		if man.Contributes.RPC[i].Name == rpc {
			decl = &man.Contributes.RPC[i]
		}
	}
	if decl == nil {
		return nil, fail(CodeUnknownRPC, "it does not declare rpc %q in contributes.rpc", clipText(rpc, 64))
	}
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}
	if len(params) > MaxCallParams {
		return nil, fail(CodeCallTooLarge, "params are %d bytes, over the %d byte cap", len(params), MaxCallParams)
	}
	if !json.Valid(params) {
		return nil, fail(CodeBadCall, "params are not valid JSON")
	}
	if !m.calls.enter(id) {
		return nil, fail(CodePluginBusy, "%d calls are already running, try again", MaxCallsPerPlugin)
	}
	defer m.calls.leave(id)

	timeout := MaxCallTimeout
	if decl.TimeoutMs > 0 && time.Duration(decl.TimeoutMs)*time.Millisecond < timeout {
		timeout = time.Duration(decl.TimeoutMs) * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	raw, err := m.proc.h.Call(cctx, id, "rpc."+rpc, params)
	took := time.Since(started).Round(time.Millisecond)
	if err != nil {
		cerr := m.mapCallErr(id, rpc, timeout, ctx, err)
		m.proc.h.Logf(id, "rpc %s failed after %s: %s", rpc, took, cerr.Error())
		return nil, cerr
	}
	if len(raw) > MaxCallResult {
		m.proc.h.Logf(id, "rpc %s: result of %d bytes dropped (cap %d)", rpc, len(raw), MaxCallResult)
		return nil, fail(CodeCallTooLarge, "the result of rpc %q is %d bytes, over the %d byte cap; it was dropped", rpc, len(raw), MaxCallResult)
	}
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	m.proc.h.Logf(id, "rpc %s ok in %s, %d bytes: %s", rpc, took, len(raw), clipText(string(raw), maxLogPreview))
	return raw, nil
}

// mapCallErr turns what the host returned into a typed error naming the
// plugin. A plugin's own error text is untrusted: control characters are
// removed, it is clipped and passed through the plugin redactor.
func (m *Manager) mapCallErr(id, rpc string, timeout time.Duration, parent context.Context, err error) error {
	var re *host.RPCError
	switch {
	case errors.As(err, &re):
		msg := m.proc.red.Load().String(clipText(re.Message, maxCallErrChars))
		if re.Code == host.ErrCodeMethodNotFound {
			msg = "the process does not implement rpc." + rpc
		}
		return newErr(CodePluginError, "plugin %s: rpc %s failed: %s", id, rpc, msg)
	case errors.Is(err, host.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return newErr(CodeCallTimeout, "plugin %s: rpc %s did not answer within %s", id, rpc, timeout)
	case errors.Is(err, host.ErrBusy):
		return newErr(CodePluginBusy, "plugin %s: too many requests are outstanding, try again", id)
	case parent.Err() != nil:
		return parent.Err()
	default:
		return newErr(CodePluginUnavailable, "plugin %s: the process is not available (%s)", id, clipText(err.Error(), 120))
	}
}

// clip removes control characters and cuts s to at most n runes.
func clipText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "..."
}
