package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/scratch"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// scratchSweepInterval is how often the daemon sweeps closed tasks' scratch
// dirs (also swept once at start).
const scratchSweepInterval = 10 * time.Minute

// scratchWiring is the daemon's scratch-dir setup: the Manager every agent
// run gets its TMPDIR from, the liveness/busy hooks the RPCs and sweeps use,
// and the cleaner workspace.Manager calls when a task row is archived or
// deleted. The runs registry arrives only after the server is built, so busy
// reads it through an atomic pointer start installs.
type scratchWiring struct {
	mgr *scratch.Manager
	db  *store.Store
	// cleaner removes one closed task's dir (registered on the workspace
	// Manager by wireScratch; a field so tests can fire it directly).
	cleaner workspace.ScratchCleaner
	reg     atomic.Pointer[runs.Registry]
}

// live reports whether a task row still exists un-archived: that task's
// scratch dir must be kept. A store error is fail-safe (true) -- a broken
// read must never end in a deletion; unknown (deleted) and archived ids are
// not live.
func (w *scratchWiring) live(id int64) bool {
	lifecycle, err := w.db.TaskLifecycle()
	if err != nil {
		return true
	}
	archived, ok := lifecycle[id]
	return ok && !archived
}

// busy reports whether a task has a running or queued run; on a registry
// error it is conservative (true): a possibly-running run must not have its
// scratch dir removed from under it.
func (w *scratchWiring) busy(id int64) bool {
	r := w.reg.Load()
	if r == nil {
		return false
	}
	_, queued, active, err := r.ActiveRunForTask(id)
	return err != nil || active || queued
}

// scratchAPI is what start needs from the wsapi API.
type scratchAPI interface {
	AttachScratch(m *scratch.Manager, live func(taskID int64) bool)
}

// wireScratch builds the scratch Manager (under $SMIND_HOME/scratch),
// registers the closed-task cleaner on the workspace Manager, and returns
// the runner options that give every agent run the per-task TMPDIR and (per
// agents.sharedCaches, default on) the shared build caches. Call it before
// the Runner is built; start once the server and its runs registry exist.
func wireScratch(cfg config.Config, db *store.Store, wm *workspace.Manager) (opts []taskrunner.Option, w *scratchWiring) {
	w = &scratchWiring{db: db}
	w.mgr = scratch.New(filepath.Join(config.Dir(), "scratch"), scratch.WithBusy(w.busy))
	w.cleaner = func(id int64) {
		rem, err := w.mgr.Remove(id)
		if errors.Is(err, scratch.ErrBusy) {
			return // a still-running run: the next sweep retries
		}
		if err != nil {
			log.Printf("scratch: remove task %d: %v", id, err)
			return
		}
		if rem.Files > 0 {
			log.Printf("scratch: removed %d file(s), %d bytes of task %d", rem.Files, rem.Bytes, id)
		}
	}
	wm.SetScratchCleaner(w.cleaner)
	opts = []taskrunner.Option{taskrunner.WithScratch(w.mgr)}
	if cfg.Agents.SharedCachesEnabled() {
		home, _ := os.UserHomeDir()
		opts = append(opts, taskrunner.WithSharedCaches(scratch.ResolveSharedCaches(context.Background(), home, exec.LookPath,
			func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return exec.CommandContext(ctx, name, args...).Output()
			}, os.Getenv)))
	}
	return opts, w
}

// start attaches the scratch RPCs (before the server serves any connection)
// and sweeps closed tasks' scratch dirs once immediately, then every
// scratchSweepInterval, until ctx is done.
func (w *scratchWiring) start(ctx context.Context, api scratchAPI, reg *runs.Registry) {
	w.reg.Store(reg)
	api.AttachScratch(w.mgr, w.live)
	go func() {
		w.sweep()
		t := time.NewTicker(scratchSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.sweep()
			}
		}
	}()
}

// sweep removes the closed tasks' dirs; logs carry counts only.
func (w *scratchWiring) sweep() {
	rem, err := w.mgr.Sweep(w.live)
	if err != nil {
		log.Printf("scratch: sweep: %v", err)
		return
	}
	if rem.Files > 0 {
		log.Printf("scratch: swept %d file(s), %d bytes of closed tasks", rem.Files, rem.Bytes)
	}
}
