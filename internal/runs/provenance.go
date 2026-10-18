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
const provenanceTimeout = 3 * time.Second

// captureProvenance reads the run's worktree git state and persists it (O1).
// atStart selects which capture this is: base (before the agent is spawned,
// so the agent cannot edit before the base is read) or head (before the
// terminal status is persisted). Every failure -- no worktree, non-git dir,
// git missing, timeout -- logs one line and leaves that field nil; it never
// changes the run's status, emits a run event, or returns an error. Each
// field (sha, dirty) is captured independently, so a partial capture still
// records what it could read. UpdateRunProvenance writes only the non-nil
// fields, so a terminal capture with a failed sha read leaves the start
// values (and dirty semantics stay "latest successful capture").
func (reg *Registry) captureProvenance(r *run, atStart bool) {
	if r.worktree == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), provenanceTimeout)
	defer cancel()

	var p store.RunProvenance
	if sha, err := workspace.GitHead(ctx, r.worktree); err != nil {
		if atStart {
			log.Printf("runs: provenance: run %s: base sha: %v", r.id, err)
		} else {
			log.Printf("runs: provenance: run %s: head sha: %v", r.id, err)
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
			log.Printf("runs: provenance: run %s: base dirty: %v", r.id, err)
		} else {
			log.Printf("runs: provenance: run %s: head dirty: %v", r.id, err)
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
