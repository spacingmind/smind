package runs

import (
	"context"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/history/native"
)

// ACP loads start a process, so they are limited beyond the shared slot pool
// (ADR-0053, "Implemented: ACP session/load"):
//
//   - one load per run at a time: a second request answers rebuild_busy without
//     starting anything;
//   - a load that failed (or found no turn) is not retried for ACPLoadCooldown:
//     the same reason is answered from memory;
//   - a load is cancelled, and its process killed, when a run starts on its chat
//     (two agents on one native session) and when the Registry closes.

// ACPLoadCooldown is how long a failed ACP load of a run answers the same reason
// without starting the agent again (memory only).
const ACPLoadCooldown = 60 * time.Second

// acpCooldown and acpNow are ACPLoadCooldown and the clock, so a test can shorten the wait.
var (
	acpCooldown = ACPLoadCooldown
	acpNow      = time.Now
)

type acpLoads struct {
	mu       sync.Mutex
	inflight map[string]*acpLoad // by run id
	failed   map[string]acpFailure
	wg       sync.WaitGroup
}

type acpLoad struct {
	chatID int64
	cancel context.CancelFunc
	// stoppedBy is "run" when a run started on the chat, "close" at shutdown; "" otherwise.
	stoppedBy string
}

type acpFailure struct {
	until  time.Time
	reason native.Reason
	detail string
}

// cooledReasons are the outcomes that are not retried inside the cooldown: they
// come from the agent or the session, and asking again now gives the same answer.
func cooledReason(r native.Reason) bool {
	switch r {
	case native.ReasonFailed, native.ReasonSliceNotFound, native.ReasonFormatDrift, native.ReasonSessionMissing:
		return true
	}
	return false
}

// beginACPLoad admits one load of runID (on chatID) or says why not. The returned
// context is cancelled when a run starts on the chat or the Registry closes; done
// must be called with the load's outcome.
func (reg *Registry) beginACPLoad(parent context.Context, runID string, chatID int64) (ctx context.Context, done func(Rebuilt) Rebuilt, refused *Rebuilt) {
	if reg.closing.Load() {
		r := degraded(native.ReasonCannotRebuild, "the daemon is shutting down")
		return nil, nil, &r
	}
	a := &reg.acp
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inflight == nil {
		a.inflight, a.failed = map[string]*acpLoad{}, map[string]acpFailure{}
	}
	if f, ok := a.failed[runID]; ok {
		if acpNow().Before(f.until) {
			r := degraded(f.reason, f.detail)
			return nil, nil, &r
		}
		delete(a.failed, runID)
	}
	if _, busy := a.inflight[runID]; busy {
		r := degraded(native.ReasonBusy, "")
		return nil, nil, &r
	}
	ctx, cancel := context.WithCancel(parent)
	l := &acpLoad{chatID: chatID, cancel: cancel}
	a.inflight[runID] = l
	a.wg.Add(1)
	return ctx, func(res Rebuilt) Rebuilt {
		a.mu.Lock()
		stopped := l.stoppedBy
		delete(a.inflight, runID)
		if stopped == "" && !res.OK() && cooledReason(res.Reason) {
			a.failed[runID] = acpFailure{until: acpNow().Add(acpCooldown), reason: res.Reason, detail: res.Detail}
		}
		a.mu.Unlock()
		cancel()
		a.wg.Done()
		switch stopped {
		case "run":
			return degraded(native.ReasonSessionInUse, "")
		case "close":
			return degraded(native.ReasonCannotRebuild, "the daemon is shutting down")
		}
		return res
	}, nil
}

// cancelACPLoads stops the loads of chatID: a run is starting on it.
func (reg *Registry) cancelACPLoads(chatID int64) {
	a := &reg.acp
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, l := range a.inflight {
		if l.chatID == chatID && l.stoppedBy == "" {
			l.stoppedBy = "run"
			l.cancel()
		}
	}
}

// closeACPLoads cancels every load and waits until their agents are gone.
func (reg *Registry) closeACPLoads() {
	a := &reg.acp
	a.mu.Lock()
	for _, l := range a.inflight {
		if l.stoppedBy == "" {
			l.stoppedBy = "close"
		}
		l.cancel()
	}
	a.mu.Unlock()
	a.wg.Wait()
}
