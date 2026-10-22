package main

import (
	"log"
	"path/filepath"
	"time"

	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// attachmentSweepAge is how old an unreferenced attachment set must be before
// the startup sweep removes it (ADR-0038): young enough sets may belong to a
// request that is still being recorded.
const attachmentSweepAge = 24 * time.Hour

// wireAttachments turns prompt attachments on (ADR-0038): the store lives
// under $SMIND_HOME/attachments, deleting a task removes its sets, and one
// sweep drops sets orphaned by a crash. Call it before serving connections.
func wireAttachments(api *wsapi.API, wm *workspace.Manager, db *store.Store) {
	st := attach.NewStore(filepath.Join(config.Dir(), "attachments"))
	api.SetAttachmentStore(st)
	wm.SetAttachmentCleanup(func(ids []string) {
		for _, id := range ids {
			if err := st.Remove(id); err != nil {
				log.Printf("attachments: remove set: %v", err)
			}
		}
	})
	n, err := runs.SweepAttachments(st, db, attachmentSweepAge, time.Now())
	if err != nil {
		log.Printf("attachments: sweep: %v", err)
	} else if n > 0 {
		log.Printf("attachments: swept %d orphaned sets", n)
	}
}
