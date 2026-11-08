package wsapi

import (
	"slices"
	"testing"

	"github.com/spacingmind/smind/internal/permpolicy"
)

// The policy floor refuses a command that names one of permpolicy.HumanOnlyRPCs.
// The table must stay true to the handler map: no named method may be
// run-scoped (an agent connection could call it), and the ones that exist today
// must still be handlers (a rename would silently drop the floor's coverage).
// The two answer methods of the agent-questions work are named ahead of their
// handlers, so they are allowed to be absent but never run-scoped.
func TestHumanOnlyRPCTable_MatchesHandlers(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	mustExist := []string{"run.respondPermission", "inbox.resolve", "permission.revealHeredoc", "run.revealToolHeredoc"}
	for _, m := range permpolicy.HumanOnlyRPCs {
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is run-scoped but named human-only", m)
		}
		if _, ok := e.api.hs[m]; !ok && slices.Contains(mustExist, m) {
			t.Errorf("%s is named human-only but is no handler any more: update permpolicy.HumanOnlyRPCs", m)
		}
	}
	for _, m := range mustExist {
		if !slices.Contains(permpolicy.HumanOnlyRPCs, m) {
			t.Errorf("%s must stay in permpolicy.HumanOnlyRPCs", m)
		}
	}
}
