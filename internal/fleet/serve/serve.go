package serve

import (
	"time"

	"github.com/spacingmind/smind/internal/learning/redact"
)

// Server serves the fleet read methods for one host.
type Server struct {
	opts   Options
	replay *replayCache
	lim    *limiter
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
	return &Server{opts: o, replay: newReplayCache(), lim: newLimiter(o.RatePerSec, o.Burst)}
}
