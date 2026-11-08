package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Requester says who asked for a rebuild. Only a human request rebuilds: an
// agent, an orchestrator or an automation reads the stored skeleton (H7).
type Requester int

const (
	RequesterAgent Requester = iota // the zero value: not a human
	RequesterHuman
)

// Default bounds (ADR-0053 D4).
const (
	DefaultConcurrent = 2
	DefaultTimeout    = 10 * time.Second
	DefaultMaxOutput  = 64 << 20
	breakerFailures   = 3
	breakerOpenFor    = time.Hour
)

// Config wires a Rebuilder to the daemon.
type Config struct {
	// Roots returns the directories a ref of the given harness may be read from
	// NOW: the config roots the daemon would launch that provider with. A ref's
	// own ConfigDir/CodexHome must be one of them, so a stored or forged ref
	// cannot name another directory. nil or empty = nothing can be read.
	Roots func(provider, harness string) []string
	// Scrub, when set, is applied to every string a rebuilt event carries
	// (the provider's secret values and the like) before the elider runs.
	Scrub func(provider, s string) string

	MaxConcurrent int           // 0 = DefaultConcurrent
	Timeout       time.Duration // 0 = DefaultTimeout
	MaxOutput     int64         // 0 = DefaultMaxOutput
	Now           func() time.Time
}

// Request is one rebuild.
type Request struct {
	Ref       Ref
	Requester Requester
	// ExpectedToolCalls is the number of distinct tool calls the stored
	// skeleton has, for the drift self-check; negative = unknown, not checked.
	ExpectedToolCalls int
}

// Result is the outcome of a rebuild. A refusal is a Reason with no events,
// never an error: the caller serves the skeleton and shows the reason.
type Result struct {
	// Events are the turn's text, thinking and tool calls in client form: tool
	// inputs, titles and results pass the same ToolCallElider a live read does.
	Events []taskrunner.Event
	Reason Reason
	// Detail carries counts only (never text) for logs and the UI note.
	Detail string

	// Refs are the heredoc references the elider reported for each call's input
	// (by tool call id), for the layer that converts events to the wire: a live
	// tool call carries them next to its elided input.
	Refs map[string][]taskrunner.HeredocRef

	// FileSize and FileModTime (unix ns) identify the session file as it was when
	// this rebuild opened it; set on success. A cache of the result is valid only
	// while ProbeSession still reports the same pair.
	FileSize, FileModTime int64

	raw map[string][]string // tool call id -> raw heredoc bodies, in elision order
}

// OK reports whether the history was rebuilt.
func (r Result) OK() bool { return r.Reason == ReasonNone }

// HeredocBody returns body index of tool call id as the session recorded it
// (scrubbed, not elided), for the reveal path to verify against the stored
// descriptor. false when the call has no such body.
func (r Result) HeredocBody(id string, index int) (string, bool) {
	bodies := r.raw[id]
	if index < 0 || index >= len(bodies) {
		return "", false
	}
	return bodies[index], true
}

// Rebuilder rebuilds histories under fixed bounds.
type Rebuilder struct {
	cfg Config
	sem chan struct{}

	mu      sync.Mutex
	drift   map[string]*driftState // key harness|version
	nowFunc func() time.Time

	// beforeParse, when set (tests only), runs in the parse goroutine just before
	// the file is read, so a test can hold a slot or outlast the timeout.
	beforeParse atomic.Pointer[func()]

	parses atomic.Int64 // sessions actually read and parsed (not cache hits)
}

// Parses is the number of session files this Rebuilder has read so far.
func (rb *Rebuilder) Parses() int64 { return rb.parses.Load() }

// Probe is ProbeSession under the roots the daemon allows now: what a rebuild of
// ref would find, from a stat only (the file is not read).
func (rb *Rebuilder) Probe(ref Ref) Probe {
	var roots []string
	if rb.cfg.Roots != nil {
		roots = rb.cfg.Roots(ref.Provider, ref.Harness)
	}
	return ProbeSession(ref, roots)
}

type driftState struct {
	consecutive int
	openUntil   time.Time
}

// New returns a Rebuilder.
func New(cfg Config) *Rebuilder {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = DefaultConcurrent
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxOutput <= 0 {
		cfg.MaxOutput = DefaultMaxOutput
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Rebuilder{cfg: cfg, sem: make(chan struct{}, cfg.MaxConcurrent), drift: map[string]*driftState{}, nowFunc: now}
}

func refuse(r Reason, detail string) Result { return Result{Reason: r, Detail: detail} }

// Rebuild reads the provider's session for req.Ref and returns the events of the
// run's turn. It never writes, never returns an error, and never reads a file
// that the ref and the allowed roots do not name.
func (rb *Rebuilder) Rebuild(ctx context.Context, req Request) Result {
	if req.Requester != RequesterHuman {
		return refuse(ReasonHumanOnly, "")
	}
	ref := req.Ref
	var roots []string
	if rb.cfg.Roots != nil {
		roots = rb.cfg.Roots(ref.Provider, ref.Harness)
	}
	var parse func(context.Context, *readerCtx) ([]taskrunner.Event, sliceStats, int, error)
	var find func() (string, error)
	switch ref.Harness {
	case HarnessClaudeCode:
		find = func() (string, error) { return claudeSessionFile(ref, roots, MaxFileBytes) }
		parse = func(ctx context.Context, rc *readerCtx) ([]taskrunner.Event, sliceStats, int, error) {
			return parseClaude(ctx, rc.f, ref, &rc.out, rb.cfg.MaxOutput)
		}
	case HarnessCodex:
		find = func() (string, error) { return codexSessionFile(ref, roots, MaxFileBytes) }
		parse = func(ctx context.Context, rc *readerCtx) ([]taskrunner.Event, sliceStats, int, error) {
			return parseCodex(ctx, rc.f, ref, &rc.out, rb.cfg.MaxOutput)
		}
	default:
		return refuse(ReasonCannotRebuild, "")
	}
	if ref.SessionID == "" || ref.PromptSha256 == "" {
		return refuse(ReasonCannotRebuild, "no session or prompt hash recorded")
	}
	key := ref.Harness + "|" + ref.ProviderVersion
	if rb.breakerOpen(key) {
		return refuse(ReasonFormatDrift, "too many consecutive format failures for this version; retrying later")
	}
	select {
	case rb.sem <- struct{}{}:
	default:
		return refuse(ReasonBusy, "")
	}

	var f *os.File
	var fileSize, fileMod int64
	real, err := find()
	if err == nil {
		var f2 *os.File
		if f2, err = openRegular(real, MaxFileBytes); err == nil {
			f = f2
			if st, serr := f2.Stat(); serr == nil {
				fileSize, fileMod = st.Size(), st.ModTime().UnixNano()
			}
		}
	}
	if err != nil {
		<-rb.sem
		if errors.Is(err, errTooLarge) {
			return refuse(ReasonFailed, "the session file is over the size bound")
		}
		return refuse(ReasonSessionMissing, "")
	}

	ctx, cancel := context.WithTimeout(ctx, rb.cfg.Timeout)
	defer cancel()
	type parsed struct {
		events  []taskrunner.Event
		st      sliceStats
		matches int
		err     error
	}
	done := make(chan parsed, 1)
	go func() {
		// The slot and the file stay held until the parse really ends, so a timeout
		// cannot let more than the bound run at once.
		defer func() { f.Close(); <-rb.sem }()
		rc := &readerCtx{f: f}
		rb.parses.Add(1)
		if h := rb.beforeParse.Load(); h != nil {
			(*h)()
		}
		ev, st, m, err := parse(ctx, rc)
		done <- parsed{ev, st, m, err}
	}()
	var p parsed
	select {
	case p = <-done:
	case <-ctx.Done():
		return refuse(ReasonFailed, "timed out")
	}
	if p.err != nil {
		return refuse(ReasonFailed, p.err.Error())
	}
	if p.matches != 1 {
		return refuse(ReasonSliceNotFound, fmt.Sprintf("%d matching turns", p.matches))
	}
	if why := selfCheck(ref.Harness, p.st, len(p.events), req.ExpectedToolCalls); why != "" {
		rb.recordDrift(key, true)
		return refuse(ReasonFormatDrift, why)
	}
	rb.recordDrift(key, false)
	events, raw, refs := rb.deliver(ref.Provider, p.events)
	return Result{Events: events, raw: raw, Refs: refs, FileSize: fileSize, FileModTime: fileMod, Detail: fmt.Sprintf("%d events from %d records", len(events), p.st.decoded)}
}

type readerCtx struct {
	f   interface{ Read([]byte) (int, error) }
	out int64
}

// selfCheck is the format-drift test of ADR-0053 D7 on the matched turn: the
// file decoded, tool results pair with tool calls in at least 95% of calls, and
// the call count is within 10% (at least one) of the stored skeleton's. It
// returns "" when the session passes and a counts-only reason otherwise.
func selfCheck(harness string, st sliceStats, events, expectedCalls int) string {
	if st.decoded == 0 || st.undecodable*20 > st.decoded {
		return fmt.Sprintf("%d of %d records did not decode", st.undecodable, st.decoded+st.undecodable)
	}
	if harness != HarnessClaudeCode {
		return ""
	}
	denom := st.uses
	if st.results > denom {
		denom = st.results
	}
	if denom > 0 && float64(st.paired) < 0.95*float64(denom) {
		return fmt.Sprintf("%d of %d tool calls paired with a result", st.paired, denom)
	}
	if expectedCalls >= 0 {
		allow := expectedCalls / 10
		if allow < 1 {
			allow = 1
		}
		diff := st.uses - expectedCalls
		if diff < 0 {
			diff = -diff
		}
		if diff > allow {
			return fmt.Sprintf("%d tool calls in the session, %d in the stored run", st.uses, expectedCalls)
		}
	}
	return ""
}

func (rb *Rebuilder) breakerOpen(key string) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	d := rb.drift[key]
	return d != nil && rb.nowFunc().Before(d.openUntil)
}

func (rb *Rebuilder) recordDrift(key string, failed bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	d := rb.drift[key]
	if d == nil {
		d = &driftState{}
		rb.drift[key] = d
	}
	if !failed {
		d.consecutive, d.openUntil = 0, time.Time{}
		return
	}
	d.consecutive++
	if d.consecutive >= breakerFailures {
		d.openUntil = rb.nowFunc().Add(breakerOpenFor)
		d.consecutive = 0
	}
}

// deliver scrubs and elides a turn's events the way a live read does, and keeps
// the raw heredoc bodies for the reveal path.
func (rb *Rebuilder) deliver(provider string, in []taskrunner.Event) ([]taskrunner.Event, map[string][]string, map[string][]taskrunner.HeredocRef) {
	el := taskrunner.NewToolCallElider()
	raw := map[string][]string{}
	allRefs := map[string][]taskrunner.HeredocRef{}
	out := make([]taskrunner.Event, 0, len(in))
	for _, e := range in {
		if rb.cfg.Scrub != nil {
			f := func(s string) string { return rb.cfg.Scrub(provider, s) }
			e.Text = f(e.Text)
			e.ToolTitle = f(e.ToolTitle)
			e.ToolInput = scrubJSON(e.ToolInput, f)
			e.ToolResult = scrubJSON(e.ToolResult, f)
		}
		if e.Type == taskrunner.EventTypeToolCall {
			if _, ok := raw[e.ToolCallID]; !ok && len(e.ToolInput) > 0 {
				if bodies := heredocBodies(e.ToolInput); len(bodies) > 0 {
					raw[e.ToolCallID] = bodies
				}
			}
			var refs []taskrunner.HeredocRef
			e.ToolInput, e.ToolTitle, e.ToolResult, refs = el.Elide(e.ToolCallID, e.ToolInput, e.ToolTitle, e.ToolResult)
			if len(refs) > 0 {
				allRefs[e.ToolCallID] = refs
			}
		}
		out = append(out, e)
	}
	return out, raw, allRefs
}

// heredocBodies lists the raw heredoc bodies of a command input in the order
// taskrunner.ElideToolCall numbers them.
func heredocBodies(input json.RawMessage) []string {
	for _, key := range []string{"command", "cmd"} {
		cmd, ok := taskrunner.StringField(input, key)
		if !ok {
			continue
		}
		if _, bodies := permpolicy.ElideHeredocs(cmd); len(bodies) > 0 {
			out := make([]string, len(bodies))
			for i, b := range bodies {
				out[i] = b.Text
			}
			return out
		}
	}
	return nil
}

// scrubJSON applies f to every string inside a raw JSON value (a bare string,
// or the strings of an object or array) and returns the re-encoded value;
// anything that is not JSON is returned unchanged.
func scrubJSON(raw json.RawMessage, f func(string) string) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	changed := false
	v = scrubValue(v, f, &changed)
	if !changed {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}

func scrubValue(v any, f func(string) string, changed *bool) any {
	switch t := v.(type) {
	case string:
		if s := f(t); s != t {
			*changed = true
			return s
		}
	case []any:
		for i := range t {
			t[i] = scrubValue(t[i], f, changed)
		}
	case map[string]any:
		for k, x := range t {
			t[k] = scrubValue(x, f, changed)
		}
	}
	return v
}
