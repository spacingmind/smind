// Package service is the learning collector (ADR-0026 section 2/3): it
// observes finished runs, counts tool calls, decides when to reflect, runs the
// isolated reflector through an Executor, gate-checks what comes back and
// stores the survivors as pending proposals. Budget, cool-down, single-flight
// and the kill switch are enforced here, in code.
package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Typed refusals of an explicit trigger.
var (
	ErrDisabled         = errors.New("learning is disabled")
	ErrModeOff          = errors.New("learning is not enabled for this workspace")
	ErrBusy             = errors.New("a learning job is already running for this workspace")
	ErrNothingToReflect = errors.New("nothing to reflect on since the last reflection")
	ErrNotEligible      = errors.New("chat is not eligible for learning")
	ErrClosed           = errors.New("learning service is closed")
	ErrNotImplemented   = errors.New("not implemented")
)

// Executor runs one isolated reflector/curator request (executor.Executor).
type Executor interface {
	Run(ctx context.Context, r executor.Request) (executor.Result, error)
}

// Notifier hears about learning progress. Implementations must not block.
type Notifier interface {
	NotifyProposed(workspaceID, proposalID int64, name, kind string)
	NotifyJob(workspaceID, jobID int64, status string)
	NotifyChanged(workspaceID int64, reason string)
}

type noopNotifier struct{}

func (noopNotifier) NotifyProposed(int64, int64, string, string) {}
func (noopNotifier) NotifyJob(int64, int64, string)              {}
func (noopNotifier) NotifyChanged(int64, string)                 {}

// Deps are the service's collaborators. Only Store, Exec and Approval are
// required; every other field has a default.
type Deps struct {
	Store    *store.Store
	Exec     Executor
	Approval *approval.Service
	// Human lists the workspace's human/third-party skills (name +
	// description only); nil = none.
	Human func(workspaceID int64) []gate.LiveSkill
	// Config is read at every decision so a kill switch takes effect
	// instantly; nil = config.DefaultLearning().
	Config func() config.LearningConfig
	Notify Notifier
	// Home is the smind home dir; its path is redacted from windows.
	Home string
	Now  func() time.Time
	// DecodeEvent decodes run_events.event_data; default runs.DecodeStoredEvent.
	DecodeEvent func(string) (taskrunner.Event, error)
	// TailEvery is the tail-flush period; default 1 minute.
	TailEvery time.Duration
}

const (
	eventQueue       = 256
	defaultTailEvery = time.Minute
	jobKindReflect   = "reflect"
)

// Trigger kinds recorded on learning_jobs.trigger_kind.
const (
	triggerCadence  = "cadence"
	triggerExplicit = "explicit"
	triggerTail     = "tail"
)

// Service is the collector. It satisfies runs.Observer.
type Service struct {
	d Deps

	events chan runs.RunFinished

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // jobs
	loops  sync.WaitGroup // the event loop

	// onJobStart, if set (tests), runs at the top of every job goroutine.
	onJobStart func(jobID int64)

	mu      sync.Mutex
	active  map[int64]bool // workspaces with a job between create and terminal state
	closing bool
	started bool
}

var _ runs.Observer = (*Service)(nil)

// New builds a Service; call Start to begin consuming events.
func New(d Deps) *Service {
	if d.Notify == nil {
		d.Notify = noopNotifier{}
	}
	if d.Config == nil {
		d.Config = config.DefaultLearning
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.DecodeEvent == nil {
		d.DecodeEvent = runs.DecodeStoredEvent
	}
	if d.TailEvery <= 0 {
		d.TailEvery = defaultTailEvery
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{d: d, events: make(chan runs.RunFinished, eventQueue), ctx: ctx, cancel: cancel, active: map[int64]bool{}}
}

func (s *Service) now() time.Time { return s.d.Now().UTC() }

// Start fails jobs a previous process left queued or running (without
// touching any chat watermark, so the same window is retried at the next
// trigger) and starts the event loop and the tail ticker. ctx cancellation
// stops the service like Close. Start is idempotent.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started || s.closing {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.loops.Add(1)
	s.mu.Unlock()

	if n, err := s.d.Store.FailInterruptedLearningJobs("interrupted by restart"); err != nil {
		slog.Error("learning: fail interrupted jobs", "err", err)
	} else if n > 0 {
		slog.Info("learning: marked interrupted jobs failed", "jobs", n)
	}
	go func() {
		select {
		case <-ctx.Done():
			s.cancel()
		case <-s.ctx.Done():
		}
	}()
	go s.loop()
}

// Close stops the loops, cancels in-flight jobs (they end failed, watermark
// untouched) and waits for them.
func (s *Service) Close() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.cancel()
	s.loops.Wait()
	s.wg.Wait()
}

// OnRunFinished implements runs.Observer. It never blocks: when the queue is
// full the event is dropped and logged (the next finished run or the tail
// flush catches the chat up, since the counter is persisted).
func (s *Service) OnRunFinished(f runs.RunFinished) {
	select {
	case s.events <- f:
	default:
		slog.Warn("learning: observer queue full, dropping run-finished", "run", f.RunID, "chat", f.ChatID)
	}
}

func (s *Service) loop() {
	defer s.loops.Done()
	tick := time.NewTicker(s.d.TailEvery)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case f := <-s.events:
			s.handleFinished(f)
		case <-tick.C:
			s.tailFlush()
		}
	}
}

// Curate is not implemented yet (curator job, ADR-0026 section 7).
func (s *Service) Curate(ctx context.Context, workspaceID int64) error {
	return ErrNotImplemented
}
