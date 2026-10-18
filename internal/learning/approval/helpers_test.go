package approval

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

type fixture struct {
	t   *testing.T
	st  *store.Store
	svc *Service
	ws  store.Workspace
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{t: t, st: st, ws: f0(t, st, "/ws1")}
	f.svc = New(Deps{Store: st, GateConfig: gate.DefaultConfig})
	return f
}

func f0(t *testing.T, st *store.Store, path string) store.Workspace {
	t.Helper()
	ws, err := st.CreateWorkspace(store.Workspace{Path: path, Title: path, RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return ws
}

func (f *fixture) skill(name, desc, body string) gate.Skill {
	return gate.Skill{Name: name, Category: "go", Description: desc, Body: body}
}

// propose stores a create proposal for the skill.
func (f *fixture) propose(sk gate.Skill) store.LearnedProposal {
	f.t.Helper()
	p, err := f.svc.CreateProposal(NewProposal{
		WorkspaceID: f.ws.ID, Source: "reflector", Kind: "create", Name: sk.Name,
		Items: []Item{{Intent: gate.Intent{Action: gate.ActionCreate, Name: sk.Name}, Result: sk}},
	})
	if err != nil {
		f.t.Fatalf("CreateProposal() error = %v", err)
	}
	return p
}

func (f *fixture) land(sk gate.Skill) store.LearnedSkill {
	f.t.Helper()
	k, err := f.st.LandSkill(store.LandSkillInput{
		Skill: store.LearnedSkill{WorkspaceID: f.ws.ID, Name: sk.Name, Category: sk.Category, Description: sk.Description, Body: sk.Body, Source: "reflector"},
		Actor: "human",
	})
	if err != nil {
		f.t.Fatalf("LandSkill() error = %v", err)
	}
	return k
}

func (f *fixture) ledgerActions(skillID int64) []string {
	f.t.Helper()
	es, err := f.st.ListLedger(f.ws.ID, skillID, 0)
	if err != nil {
		f.t.Fatalf("ListLedger() error = %v", err)
	}
	var out []string
	for i := len(es) - 1; i >= 0; i-- {
		out = append(out, es[i].Action)
	}
	return out
}

// Two clearly different skills so similarity stays below the thresholds.
var (
	skillA = gate.Skill{Name: "learned-flaky-tempdir", Category: "go", Description: "Use when a Go test fails on a shared TempDir.",
		Body: "Run the failing test alone with -race.\nCheck for a shared t.TempDir across parallel tests.\nAdd t.Parallel guards or separate dirs."}
	skillB = gate.Skill{Name: "learned-sqlite-busy", Category: "db", Description: "Use when sqlite returns SQLITE_BUSY under concurrent writers.",
		Body: "Open the database with a busy timeout.\nSerialise writers through one connection.\nRetry the transaction on lock errors."}
	skillC = gate.Skill{Name: "learned-docker-cache", Category: "ops", Description: "Use when a docker image build ignores layer cache.",
		Body: "Order COPY steps from least to most volatile.\nPin base image digests.\nUse BuildKit cache mounts for package managers."}
)
