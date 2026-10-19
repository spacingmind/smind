package main

import (
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/delivery"
	"github.com/spacingmind/smind/internal/store"
)

// newDeliveryService builds the learned-skill delivery service (ADR-0026
// section 5/6), or nil when the learning kill switch is off -- in which case
// nothing is wired and any projection already on disk is simply never used
// (never deleted). home is the smind home directory.
func newDeliveryService(cfg config.LearningConfig, db *store.Store, home string) *delivery.Service {
	if !cfg.Enabled {
		return nil
	}
	return delivery.New(delivery.Deps{
		Store:  db,
		Config: func() config.LearningConfig { return cfg },
		Root:   filepath.Join(home, "learning", "projection"),
	})
}
