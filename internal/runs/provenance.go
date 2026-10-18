package runs

import (
	"context"
	"log"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// provenanceTimeout bounds each git capture (O1): git runs against a local
// worktree, so 3s is generous, and a hung git must never delay the run's
// own lifecycle.
var provenanceTimeout = 3 * time.Second

// captureProvenance reads the run's worktree git state and persists it (O1).
// parent bounds the capture: the run's own ctx for the start capture (a Stop
// before the agent spawns skips it), a fresh Background for the terminal one
// (a stopped run still records its head). atStart selects which capture this is: base (before the agent is spawned,
// so the agent cannot edit before the base is read) or head (before the
// terminal status is persisted). Every failure -- no worktree, non-git dir,
// git missing, timeout -- logs one line and leaves that field nil; it never
// changes the run's status, emits a run event, or returns an error. Each
// field (sha, dirty) is captured independently, so a partial capture still
// records what it could read. UpdateRunProvenance writes only the non-nil
// fields, so a terminal capture with a failed sha read leaves the start
// values (and dirty semantics stay "latest successful capture").
func (reg *Registry) captureProvenance(parent context.Context, r *run, atStart bool) {
	if r.worktree == "" {
		return
	}
	timeout := provenanceTimeout
	if !atStart && reg.closing.Load() {
		// Daemon shutdown waits on every run's closedCh: keep the terminal
		// capture short rather than add the full timeout per run.
		timeout = time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// A Stop before the agent spawns cancels parent; that is not a git
	// failure worth a log line.
	logf := func(format string, args ...any) {
		if parent.Err() == nil {
			log.Printf(format, args...)
		}
	}
	var p store.RunProvenance
	if sha, err := workspace.GitHead(ctx, r.worktree); err != nil {
		if atStart {
			logf("runs: provenance: run %s: base sha: %v", r.id, err)
		} else {
			logf("runs: provenance: run %s: head sha: %v", r.id, err)
		}
	} else {
		p.BaseSHA = &sha
		if !atStart {
			p.HeadSHA = &sha
			p.BaseSHA = nil
		}
	}
	if dirty, err := workspace.GitDirty(ctx, r.worktree); err != nil {
		if atStart {
			logf("runs: provenance: run %s: base dirty: %v", r.id, err)
		} else {
			logf("runs: provenance: run %s: head dirty: %v", r.id, err)
		}
	} else {
		p.Dirty = &dirty
	}

	if err := reg.st.UpdateRunProvenance(r.id, p); err != nil {
		log.Printf("runs: provenance: run %s: persist: %v", r.id, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if p.BaseSHA != nil {
		r.baseSHA = p.BaseSHA
	}
	if p.HeadSHA != nil {
		r.headSHA = p.HeadSHA
	}
	if p.Dirty != nil {
		r.dirty = p.Dirty
	}
}
