package approval

import (
	"reflect"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
)

func TestPayload_EncodeDecode(t *testing.T) {
	items := []Item{
		{Intent: gate.Intent{Action: gate.ActionPatch, Name: "learned-a", OldString: "x", NewString: "y"}, Result: skillA, TargetSkillID: 7, BaseVersion: 3},
		{Intent: gate.Intent{Action: gate.ActionDelete, Name: "learned-b", AbsorbedInto: "learned-a"}, TargetSkillID: 8},
	}
	s, err := EncodeItems(items)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeItems(s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, items) {
		t.Errorf("round trip = %+v, want %+v", got, items)
	}
	if _, err := DecodeItems("not json"); err == nil {
		t.Error("DecodeItems(garbage) error = nil")
	}
	if s, _ := EncodeItems(nil); s != "[]" {
		t.Errorf("EncodeItems(nil) = %q, want []", s)
	}
}

func TestCreateProposal_RoundTripAndLedger(t *testing.T) {
	f := newFixture(t)
	p, err := f.svc.CreateProposal(NewProposal{
		WorkspaceID: f.ws.ID, JobID: 0, Source: "reflector", Kind: "create", Name: skillA.Name,
		Items:    []Item{{Result: skillA}},
		Evidence: `[{"kind":"user_message"}]`, PossibleDuplicateOf: "learned-x", Similarity: 0.7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "pending" || p.Kind != "create" || p.Source != "reflector" || p.Evidence != `[{"kind":"user_message"}]` ||
		p.PossibleDuplicateOf != "learned-x" || p.Similarity != 0.7 {
		t.Errorf("proposal = %+v", p)
	}
	items, err := DecodeItems(p.Intents)
	if err != nil || len(items) != 1 || items[0].Result != skillA {
		t.Errorf("items = %+v, err %v", items, err)
	}
	es, err := f.st.ListLedger(f.ws.ID, 0, 0)
	if err != nil || len(es) != 1 {
		t.Fatalf("ledger = %+v, err %v", es, err)
	}
	if es[0].Action != "propose" || es[0].Actor != "reflector" || es[0].SkillName != skillA.Name || es[0].Evidence != p.Evidence {
		t.Errorf("ledger row = %+v", es[0])
	}
}

func TestLiveSnapshot_IncludesPendingAndHuman(t *testing.T) {
	f := newFixture(t)
	f.land(skillA)
	f.propose(skillB)
	f.svc.d.Human = func(int64) []gate.LiveSkill { return []gate.LiveSkill{{Name: "h", Description: "d", Human: true}} }
	live, err := f.svc.liveSnapshot(f.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for _, l := range live {
		statuses = append(statuses, l.Name+":"+l.Status)
	}
	want := []string{skillA.Name + ":active", skillB.Name + ":proposed", "h:"}
	if !reflect.DeepEqual(statuses, want) {
		t.Errorf("snapshot = %v, want %v", statuses, want)
	}
}
