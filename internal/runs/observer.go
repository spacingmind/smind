package runs

import (
	"log/slog"

	"github.com/spacingmind/smind/internal/taskrunner"

	"github.com/spacingmind/smind/internal/store"
)

// PermissionResolvedBySystem tags a permission request a system run
// (WithPurpose) auto-denied without waiting on anyone (ADR-0026).
const PermissionResolvedBySystem taskrunner.PermissionResolution = "system"

// RunFinished is what an Observer learns when a run reaches a terminal
// state. Purpose is "user" for ordinary runs.
type RunFinished struct {
	RunID      string
	TaskID     int64
	ChatID     int64
	Provider   string
	Purpose    string
	Status     string
	StopReason string
	// ToolCalls is the number of distinct non-empty ToolCallIDs the run
	// emitted, including runs that finished with error or cancel.
	ToolCalls int
}

// Observer is notified once per finished run, synchronously from finish,
// after notifyRunStatus and outside every registry lock (so it may call
// back into the Registry). OnRunFinished must not block -- hand slow work
// to its own goroutine. A panic is recovered and logged.
type Observer interface {
	OnRunFinished(RunFinished)
}

// AddObserver registers o. Unlike the single-slot Notifier, any number of
// observers may be registered.
func (reg *Registry) AddObserver(o Observer) {
	if o == nil {
		return
	}
	reg.observersMu.Lock()
	reg.observers = append(reg.observers, o)
	reg.observersMu.Unlock()
}

func observerPurpose(p string) string {
	if p == "" {
		return store.RunPurposeUser
	}
	return p
}

func (reg *Registry) notifyObservers(f RunFinished) {
	reg.observersMu.Lock()
	obs := append([]Observer(nil), reg.observers...)
	reg.observersMu.Unlock()
	for _, o := range obs {
		func() {
			defer func() {
				if v := recover(); v != nil {
					slog.Error("runs: observer panicked", "run", f.RunID, "panic", v)
				}
			}()
			o.OnRunFinished(f)
		}()
	}
}
