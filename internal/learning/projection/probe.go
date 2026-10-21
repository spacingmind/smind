package projection

import (
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// ProjectProbe materialises skills (the active set plus ONE proposed
// candidate, already rendered by the caller) for a probe arm. It is the only
// path by which a not-yet-approved skill reaches an agent (ADR-0037 §2): att
// must be a valid sandbox attestation at now, checked before anything is
// created. Production delivery uses Project and never this.
//
// root must be a probe-only root (e.g. <home>/learning/probe-projections), so
// production GC and Remove never touch it. The build is Project's: content
// addressed, read-only, the returned Hash is what the service records on the
// arm's run. Empty skills returns the zero Projection.
//
// Project's GC keeps only the newest two hash directories per workspace under
// root, so a caller running more than two distinct projections of one
// workspace at once must give each its own root or serialise them.
func ProjectProbe(att sandbox.Attestation, now time.Time, root string, workspaceID int64, skills []Skill) (Projection, error) {
	if err := att.Require(now); err != nil {
		return Projection{}, fmt.Errorf("projection: probe projection refused: %w", err)
	}
	return Project(root, workspaceID, skills)
}
