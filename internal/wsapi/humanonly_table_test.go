package wsapi

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/permpolicy"
)

// The policy floor refuses a command that names one of permpolicy.HumanOnlyRPCs.
// The table must stay true to the handler map: no named method may be
// run-scoped (an agent connection could call it), and the ones that exist today
// must still be handlers (a rename would silently drop the floor's coverage).
// The answer methods of the agent-questions work are listed as pending until
// their handlers land, and are never run-scoped.
func TestHumanOnlyRPCTable_MatchesHandlers(t *testing.T) {
	t.Parallel()
	src := handlerSources(t)
	for _, m := range permpolicy.HumanOnlyRPCs {
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is run-scoped but named human-only", m)
		}
		// Some handlers are registered only with their feature (fleet, relay),
		// so the check reads the sources for the quoted method name.
		registered := strings.Contains(src, `"`+m+`"`)
		pending := slices.Contains(permpolicy.PendingHumanOnlyRPCs, m)
		switch {
		case !registered && !pending:
			t.Errorf("%s is named human-only but no handler registers it: fix the name or list it in PendingHumanOnlyRPCs", m)
		case registered && pending:
			t.Errorf("%s has a handler now: drop it from permpolicy.PendingHumanOnlyRPCs", m)
		}
	}
	mustExist := []string{"run.respondPermission", "inbox.resolve", "permission.revealHeredoc", "run.revealToolHeredoc",
		"fleet.host.approve", "fleet.host.reject", "relay.device.approve", "relay.device.revoke", "chat.fork", "chat.forkSeed"}
	for _, m := range mustExist {
		if !slices.Contains(permpolicy.HumanOnlyRPCs, m) {
			t.Errorf("%s must stay in permpolicy.HumanOnlyRPCs", m)
		}
	}
}

// handlerSources is the text of the package's non-test Go files.
func handlerSources(t *testing.T) string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}
