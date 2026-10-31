package serve

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/store"
)

// Server serves the fleet read methods for one host.
type Server struct {
	opts   Options
	replay *replayCache
	lim    *limiter
	// writeMu spans the idempotency lookup, the budget check and the start
	// of a write effect, so two concurrent creates cannot both pass
	// maxLiveAgents.
	writeMu sync.Mutex
}

// New builds a Server; it panics when a required option is nil.
func New(o Options) *Server {
	if o.HostID == "" || o.Authorizer == nil || o.Audit == nil || o.Reader == nil {
		panic("serve.New: HostID, Authorizer, Audit and Reader are required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Redact == nil {
		o.Redact = redact.New(nil, nil).String
	}
	if o.PollInterval <= 0 {
		o.PollInterval = defaultPollInterval
	}
	return &Server{opts: o, replay: newReplayCache(), lim: newLimiter(o.RatePerSec, o.Burst)}
}

// Handle serves one read call from an authenticated link principal. It
// refuses or answers, and writes exactly one audit row either way.
func (s *Server) Handle(ctx context.Context, principal, method string, raw json.RawMessage) (any, error) {
	start := s.opts.Now()
	var env struct {
		Prov wire.Prov `json:"prov"`
	}
	row := store.FleetAuditRow{TS: start.UTC(), Direction: "in", HostID: principal, Method: method}

	res, err := s.serve(principal, method, raw, start, &env.Prov, &row)

	row.Trace, row.ReqID = env.Prov.Trace, env.Prov.ReqID
	row.Outcome = "ok"
	var rf *Refusal
	switch {
	case errors.As(err, &rf):
		row.Outcome, row.Code = "refused", rf.Code
	case err != nil:
		row.Outcome = "error"
	default:
		if b, merr := json.Marshal(res); merr == nil {
			row.Bytes = int64(len(b))
		}
	}
	row.LatencyMs = s.opts.Now().Sub(start).Milliseconds()
	row.Summary = Summary(raw, s.opts.Redact)
	if _, aerr := s.opts.Audit.AppendFleetAudit(row); aerr != nil {
		return nil, aerr
	}
	if err != nil && rf == nil {
		// A backend, ledger or store error can carry the callee's local
		// paths or remote URLs; the peer gets a fixed refusal and the
		// detail stays in this host's own log.
		log.Printf("fleet: %s from %s failed: %v", method, principal, err)
		return nil, refuse(wire.CodeInternal, "the host could not complete the request")
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) serve(principal, method string, raw json.RawMessage, now time.Time, prov *wire.Prov, row *store.FleetAuditRow) (any, error) {
	var env struct {
		Prov wire.Prov `json:"prov"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, refuse(wire.CodeInvalid, "malformed params")
	}
	*prov = env.Prov
	if !s.lim.allow(principal, now) {
		return nil, refuse(wire.CodeRateLimited, "rate limit exceeded")
	}
	if r := s.checkProv(principal, env.Prov, now); r != nil {
		return nil, r
	}

	// Write methods exist only on a host whose write effects are wired;
	// without them the read switch below answers "unknown method".
	if slices.Contains(wire.WriteMethods, method) && s.opts.Backend != nil && s.opts.Ledger != nil && s.opts.Tasks != nil {
		return s.write(principal, method, raw, now, row)
	}

	var ws int64
	var call func(g registry.Grant) (any, error)
	rd := s.opts.Reader
	decode := func(v any) error {
		if err := json.Unmarshal(raw, v); err != nil {
			return refuse(wire.CodeInvalid, "malformed params")
		}
		return nil
	}
	id := func(n int64) string {
		if n == 0 {
			return ""
		}
		return strconv.FormatInt(n, 10)
	}
	switch method {
	case wire.MethodInfo:
		call = func(g registry.Grant) (any, error) { return rd.Info(g) }
	case wire.MethodWorkspaceList:
		call = func(g registry.Grant) (any, error) { return rd.Workspaces(g) }
	case wire.MethodTaskList:
		var p wire.TaskListParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		ws = p.WorkspaceID
		call = func(g registry.Grant) (any, error) { return rd.TaskList(g, p) }
	case wire.MethodTaskGet:
		var p wire.TaskGetParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		row.TaskID = id(p.ID)
		call = func(g registry.Grant) (any, error) { return rd.TaskGet(g, p) }
	case wire.MethodRunStatus:
		var p wire.RunStatusParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		row.TaskID, row.RunID = id(p.TaskID), p.RunID
		call = func(g registry.Grant) (any, error) {
			st, err := rd.RunStatus(g, p)
			if err != nil {
				return nil, err
			}
			// Only a live run can wait on a permission, and the flag is all
			// the caller ever learns of it (AC19).
			if s.opts.Backend != nil && st.State == "running" {
				st.WaitingPermission = s.opts.Backend.WaitingPermission(st.RunID)
			}
			return st, nil
		}
	case wire.MethodTaskActivity:
		var p wire.TaskActivityParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		row.TaskID, row.RunID = id(p.TaskID), p.RunID
		call = func(g registry.Grant) (any, error) { return rd.Activity(g, p) }
	default:
		return nil, refuse(wire.CodeInvalid, "unknown method")
	}

	g, err := s.opts.Authorizer.Authorize(principal, registry.OpRead, ws)
	if err != nil {
		return nil, refuse(wire.CodeNotGranted, "not granted")
	}
	res, err := call(g)
	if err != nil {
		var rf *Refusal
		if errors.As(err, &rf) {
			return nil, rf
		}
		if errors.Is(err, ErrNotFound) {
			return nil, refuse(wire.CodeNotFound, "not found")
		}
		return nil, err
	}
	return res, nil
}
