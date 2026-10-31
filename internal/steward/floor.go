package steward

import (
	"fmt"

	"github.com/spacingmind/smind/internal/permpolicy"
)

// SeatRequest is one permission request of the seat: the tool and every
// path it names, judged against the scratch tree.
type SeatRequest struct {
	Tool       string
	Paths      []string
	ScratchDir string
}

// SeatFloor decides one permission request of the seat (ADR-0047 §1):
// allow ONLY Read, Grep or Glob whose every named path resolves INSIDE the
// scratch tree; everything else is deny (a judge has no reason to ask).
// Resolving first makes a symlink or `..` that escapes the tree visible.
func SeatFloor(req SeatRequest) (allow bool, reason string) {
	known := false
	for _, t := range SeatBuiltinTools {
		if req.Tool == t {
			known = true
			break
		}
	}
	if !known {
		return false, fmt.Sprintf("tool %s is not available to the steward seat", req.Tool)
	}
	if req.ScratchDir == "" {
		return false, "no scratch tree: the steward seat has nothing to read"
	}
	if len(req.Paths) == 0 {
		return false, "no path named; the seat floor cannot verify it stays inside the scratch tree"
	}
	for _, p := range req.Paths {
		resolved, err := permpolicy.ResolvePath(p, req.ScratchDir)
		if err != nil {
			return false, fmt.Sprintf("path %s cannot be resolved: %v", p, err)
		}
		if !permpolicy.Within(resolved, []string{req.ScratchDir}) {
			return false, fmt.Sprintf("path %s escapes the scratch tree", p)
		}
	}
	return true, "inside the scratch tree"
}
