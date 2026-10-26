package host

import (
	"reflect"
	"time"
)

// APIVersion is the plugin API major this host speaks (manifest `api`).
const APIVersion = 1

// Spec is everything needed to run one plugin process.
type Spec struct {
	ID      string
	Version string
	// Dir is the plugin's directory and the process's working directory.
	Dir string
	// DataDir is the plugin's persistent data/ directory (created on start).
	DataDir string
	// Command is the manifest's process.command: an argv, never a shell line.
	// A first element with a path separator is a program inside Dir (or an
	// absolute path); a bare name is looked up on the plugin's fixed PATH.
	Command []string
	// DaemonVersion is reported to the plugin in plugin.initialize.
	DaemonVersion string
	// Settings is the plugin's settings object for plugin.initialize. Setting
	// storage is PB.2: until then it is empty, but the field is always sent.
	Settings map[string]any
}

func (s Spec) equal(o Spec) bool { return reflect.DeepEqual(s, o) }

// Policy is every number the host enforces. DefaultPolicy documents them.
type Policy struct {
	Limits
	Log LogLimits

	// StartTimeout is how long a plugin has to answer plugin.initialize.
	StartTimeout time.Duration
	// RequestTimeout bounds a host-to-plugin request; WriteTimeout a write to
	// a plugin that is not reading its input.
	RequestTimeout time.Duration
	WriteTimeout   time.Duration
	// StopGrace is the time between SIGTERM and the process-group kill.
	StopGrace time.Duration

	// Restart backoff: BackoffBase, doubled per consecutive crash, up to
	// BackoffCap. A run of StableAfter resets the count.
	BackoffBase time.Duration
	BackoffCap  time.Duration
	StableAfter time.Duration
	// Breaker: BreakerCrashes crashes within BreakerWindow mark the plugin
	// failed; it is not restarted until reloaded or disabled and enabled.
	BreakerCrashes int
	BreakerWindow  time.Duration
}

// DefaultPolicy returns the defaults (documented in ADR-0042's PB.1 note).
func DefaultPolicy() Policy {
	return Policy{
		Limits: Limits{
			MaxHeaderLine: 1024, MaxHeaderLines: 8,
			MaxBody:        1 << 20,
			MaxOutstanding: 32, MaxInbound: 16,
		},
		Log:          LogLimits{MaxLines: 1000, MaxBytes: 256 << 10, MaxLineBytes: 2048, ReadLine: 8192, LinesPerSec: 200},
		StartTimeout: 10 * time.Second, RequestTimeout: 30 * time.Second, WriteTimeout: 10 * time.Second,
		StopGrace:   5 * time.Second,
		BackoffBase: time.Second, BackoffCap: 30 * time.Second, StableAfter: time.Minute,
		BreakerCrashes: 5, BreakerWindow: 10 * time.Minute,
	}
}

// State is where a plugin process is in its life.
type State string

const (
	StateStarting   State = "starting"
	StateRunning    State = "running"
	StateRestarting State = "restarting" // crashed, waiting out the backoff
	StateFailed     State = "failed"     // the breaker tripped; needs a reload
)

// Status is the snapshot shown by plugin list/show and the wsapi.
type Status struct {
	State     State     `json:"state"`
	PID       int       `json:"pid,omitempty"`
	Since     time.Time `json:"since"`
	Restarts  int       `json:"restarts"`
	LastError string    `json:"lastError,omitempty"`
	RetryAt   time.Time `json:"retryAt,omitzero"`
}

// Clock lets tests drive backoff without sleeping.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
