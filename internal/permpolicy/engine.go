package permpolicy

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/store"
)

// Caps on what the engine puts in one audit row; the store bounds them too,
// the engine keeps the redacted text from being cut mid-secret by that cap.
const (
	auditMaxTarget = 512
	auditMaxPaths  = 8
	// auditMaxReasons bounds the joined reasons; they can quote a segment's
	// raw text, so they are redacted like the target.
	auditMaxReasons = 1024
)

// Audit outcomes an escalated ask can resolve to (ADR-0047 §2.3).
const (
	OutcomeHumanAllow  = "ask_human_allow"
	OutcomeHumanDeny   = "ask_human_deny"
	OutcomeTimeoutDeny = "ask_timeout_deny"
)

// AuditSink is where every evaluation is recorded; *store.Store satisfies it.
type AuditSink interface {
	AppendPolicyAudit(store.PolicyAuditRow) (int64, error)
}

// Options wires an Engine. Enabled is read per decision, so flipping the
// config takes effect without a restart; nil means disabled.
type Options struct {
	Path      string
	Enabled   func() bool
	Audit     AuditSink
	Now       func() time.Time
	Home      string
	SmindHome string
}

// Status is what `policy.get` reports: whether the engine is on and whether
// the file on disk is usable. An invalid file still means floor + ask.
type Status struct {
	Enabled bool
	Valid   bool
	Exists  bool
	Error   string
	Path    string
	Sha     string
	Rules   int
}

// Explanation is the dry-run answer of Explain.
type Explanation struct {
	Decision Decision
	Status   Status
}

// Engine evaluates permission requests against the policy file, re-reading
// it only when its mtime or size changes. Safe for concurrent use.
type Engine struct {
	o Options

	mu    sync.Mutex
	state fileState
	have  bool
}

// fileState is the cached result of reading the policy file, keyed by what
// stat reported. pol is nil for an absent or invalid file (floor + ask).
type fileState struct {
	exists bool
	mtime  time.Time
	size   int64
	pol    *Policy
	err    string
}

// NewEngine builds an Engine; the policy file is read lazily.
func NewEngine(o Options) *Engine {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Engine{o: o}
}

func (e *Engine) enabled() bool {
	return e.o.Enabled != nil && e.o.Enabled()
}

// current stats the policy file and reloads it when it changed. A stat or
// read failure, an oversize file or a Load error is an invalid state (nil
// policy), never a partial one.
func (e *Engine) current() fileState {
	e.mu.Lock()
	defer e.mu.Unlock()
	fi, err := os.Stat(e.o.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		e.state, e.have = fileState{}, true
		return e.state
	case err != nil:
		e.state, e.have = fileState{exists: true, err: err.Error()}, true
		return e.state
	}
	if e.have && e.state.exists && e.state.mtime.Equal(fi.ModTime()) && e.state.size == fi.Size() {
		return e.state
	}
	st := fileState{exists: true, mtime: fi.ModTime(), size: fi.Size()}
	if fi.Size() > MaxPolicyBytes {
		st.err = le("policy", "oversize: %d bytes, the cap is %d", fi.Size(), MaxPolicyBytes).Error()
	} else if data, err := os.ReadFile(e.o.Path); err != nil {
		st.err = err.Error()
	} else if pol, err := Load(data); err != nil {
		st.err = err.Error()
	} else {
		st.pol = pol
	}
	e.state, e.have = st, true
	return st
}

func (e *Engine) statusOf(st fileState) Status {
	s := Status{Enabled: e.enabled(), Valid: st.err == "", Exists: st.exists, Error: st.err, Path: e.o.Path}
	if st.pol != nil {
		s.Sha = st.pol.Sha
		s.Rules = len(st.pol.Rules)
	}
	return s
}

// Status reports the engine switch and the state of the policy file.
func (e *Engine) Status() Status { return e.statusOf(e.current()) }

// Rules returns the current valid policy's rules; nil when the file is
// invalid or absent.
func (e *Engine) Rules() []Rule {
	st := e.current()
	if st.pol == nil {
		return nil
	}
	return append([]Rule(nil), st.pol.Rules...)
}

func (e *Engine) fill(req Request) Request {
	if req.Home == "" {
		req.Home = e.o.Home
	}
	if req.SmindHome == "" {
		req.SmindHome = e.o.SmindHome
	}
	return req
}

// Explain is the dry run: the same Decide, no audit row, and it works while
// the engine is switched off.
func (e *Engine) Explain(req Request) Explanation {
	st := e.current()
	return Explanation{Decision: Decide(e.fill(req), st.pol), Status: e.statusOf(st)}
}

// Evaluate decides a request and records the decision. ok is false when the
// engine is off: nothing is decided and nothing is written. When the audit
// cannot be written an allow is downgraded to ask, because an allow must
// never go unrecorded.
func (e *Engine) Evaluate(req Request) (Decision, bool) {
	if !e.enabled() {
		return Decision{}, false
	}
	st := e.current()
	req = e.fill(req)
	start := e.o.Now()
	d := Decide(req, st.pol)
	latency := e.o.Now().Sub(start).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	row := e.auditRow(req, d, latency)
	if e.o.Audit == nil {
		return e.downgrade(d, errors.New("no audit sink")), true
	}
	if _, err := e.o.Audit.AppendPolicyAudit(row); err != nil {
		return e.downgrade(d, err), true
	}
	return d, true
}

func (e *Engine) downgrade(d Decision, err error) Decision {
	log.Printf("permpolicy: audit append failed: %v", err)
	if d.Action == ActionAllow {
		d.Action = ActionAsk
		d.Reasons = append(append([]string(nil), d.Reasons...), "audit unavailable")
	}
	return d
}

// RecordOutcome appends the later human or timeout resolution of an
// escalated ask as a second row linked by RequestID. An unknown outcome is
// ignored.
func (e *Engine) RecordOutcome(req Request, outcome string) {
	switch outcome {
	case OutcomeHumanAllow, OutcomeHumanDeny, OutcomeTimeoutDeny:
	default:
		return
	}
	if e.o.Audit == nil {
		return
	}
	row := store.PolicyAuditRow{
		CreatedAt: e.o.Now().UTC(), RunID: req.RunID, TaskID: req.TaskID, RequestID: req.RequestID,
		Seat: req.Seat, WorkspaceID: req.WorkspaceID, Tool: req.Tool, Decision: outcome,
		PolicySha: e.current().sha(),
	}
	if _, err := e.o.Audit.AppendPolicyAudit(row); err != nil {
		log.Printf("permpolicy: audit append failed: %v", err)
	}
}

func (s fileState) sha() string {
	if s.pol == nil {
		return ""
	}
	return s.pol.Sha
}

func (e *Engine) redactor(req Request) *redact.Redactor {
	home := req.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	var homes []string
	if home != "" {
		homes = []string{home}
	}
	return redact.New(nil, homes)
}

func (e *Engine) auditRow(req Request, d Decision, latency int64) store.PolicyAuditRow {
	rd := e.redactor(req)
	target := d.Normalized
	if req.Tool != "Bash" && !(req.Tool == "" && req.Command != "") {
		target = req.Tool + " " + strings.Join(d.Paths, " ")
	}
	target = capRunes(rd.String(target), auditMaxTarget)
	paths := d.Paths
	if len(paths) > auditMaxPaths {
		paths = paths[:auditMaxPaths]
	}
	var rp []string
	for _, p := range paths {
		rp = append(rp, rd.String(p))
	}
	rule := d.RuleID
	if rule == "" {
		rule = d.FloorID
	}
	return store.PolicyAuditRow{
		CreatedAt: e.o.Now().UTC(), RunID: req.RunID, TaskID: req.TaskID, RequestID: req.RequestID,
		Seat: req.Seat, WorkspaceID: req.WorkspaceID, Tool: req.Tool, Target: target, Paths: rp,
		Decision: string(d.Action), RuleID: rule, PolicySha: d.PolicySha,
		Reasons: capRunes(rd.String(strings.Join(d.Reasons, "; ")), auditMaxReasons), LatencyMs: latency,
	}
}

// capRunes cuts s to at most max bytes on a rune boundary.
func capRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
