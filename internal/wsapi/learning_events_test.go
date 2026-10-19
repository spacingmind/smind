package wsapi

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func learnEvents(f *learnFx) *eventConn {
	ec := newEventConn(f.t, dialWS(f.t, f.srv, "tok"))
	ec.subscribe("sub", TopicLearnedSkillProposed, TopicLearnedSkillChanged, TopicLearningJob)
	return ec
}

func TestLearning_EventsPublished(t *testing.T) {
	t.Parallel()
	f := newLearnFx(t)
	ec := learnEvents(f)
	other := newEventConn(t, dialWS(t, f.srv, "tok"))
	other.subscribe("sub", TopicWorkspaceCreated, TopicTaskStatus)

	want := func(reason string, skillID, proposalID int64) {
		t.Helper()
		ev := ec.nextEvent(5 * time.Second)
		if ev.Topic != TopicLearnedSkillChanged {
			t.Fatalf("%s: topic = %q, want %q", reason, ev.Topic, TopicLearnedSkillChanged)
		}
		var p learnedSkillChangedPayload
		decodePayload(t, ev, &p)
		if p.WorkspaceID != f.w.ID || p.Reason != reason || p.SkillID != skillID || p.ProposalID != proposalID {
			t.Fatalf("%s: payload = %+v, want skill %d proposal %d", reason, p, skillID, proposalID)
		}
	}

	f.ok("learning.setMode", map[string]any{"workspaceId": f.w.ID, "mode": "propose"}, nil)
	want("modeChanged", 0, 0)

	p1 := f.propose(lSkillA)
	p2 := f.propose(lSkillB)
	var skill LearnedSkillView
	f.ok("learning.proposals.approve", map[string]any{"id": p1.ID}, &skill)
	want("approved", skill.ID, p1.ID)
	f.ok("learning.proposals.reject", map[string]any{"id": p2.ID}, nil)
	want("rejected", 0, p2.ID)

	id := map[string]any{"id": skill.ID}
	for _, step := range []struct{ method, reason string }{
		{"learning.skills.pin", "pinned"}, {"learning.skills.unpin", "unpinned"},
		{"learning.skills.disable", "disabled"}, {"learning.skills.enable", "enabled"},
		{"learning.skills.archive", "archived"}, {"learning.skills.restore", "restored"},
	} {
		f.ok(step.method, id, nil)
		want(step.reason, skill.ID, 0)
	}
	if _, err := f.db.PatchSkill(store.PatchSkillInput{ID: skill.ID, ExpectVersion: 1, Description: lSkillA.Description, Body: lSkillA.Body + "\nMore.", Actor: "human", Action: "edit"}); err != nil {
		t.Fatal(err)
	}
	f.ok("learning.skills.undo", map[string]any{"id": skill.ID, "version": 1}, nil)
	want("undone", skill.ID, 0)
	f.ok("learning.skills.delete", id, nil)
	want("deleted", skill.ID, 0)
	f.ok("learning.forget", map[string]any{"workspaceId": f.w.ID}, nil)
	want("forgotten", 0, 0)

	// A failed mutation publishes nothing; neither does a read.
	f.fail("learning.skills.pin", map[string]any{"id": 99999}, "not_found")
	f.ok("learning.status", nil, nil)
	ec.expectNoEvent(300 * time.Millisecond)

	// Subscribers of other topics saw none of it.
	other.expectNoEvent(100 * time.Millisecond)
}

func TestLearning_ProposedAndJobEvents(t *testing.T) {
	t.Parallel()
	f := newLearnFx(t)
	ec := learnEvents(f)
	other := newEventConn(t, dialWS(t, f.srv, "tok"))
	other.subscribe("sub", TopicLearnedSkillChanged)

	n := f.api.LearningNotifier()
	n.NotifyProposed(f.w.ID, 7, "learned-x", "create")
	ev := ec.nextEvent(5 * time.Second)
	var p learnedSkillProposedPayload
	decodePayload(t, ev, &p)
	if ev.Topic != TopicLearnedSkillProposed || p != (learnedSkillProposedPayload{WorkspaceID: f.w.ID, ProposalID: 7, Name: "learned-x", Kind: "create"}) {
		t.Fatalf("proposed event = %q %+v", ev.Topic, p)
	}

	_, chat := f.userChat()
	j, err := f.db.CreateLearningJob(store.LearningJob{WorkspaceID: f.w.ID, ChatID: &chat.ID, Kind: "reflect", TriggerKind: "explicit", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	n.NotifyJob(f.w.ID, j.ID, "running")
	ev = ec.nextEvent(5 * time.Second)
	var jp learningJobPayload
	decodePayload(t, ev, &jp)
	if ev.Topic != TopicLearningJob || jp != (learningJobPayload{WorkspaceID: f.w.ID, JobID: j.ID, Status: "running"}) {
		t.Fatalf("job event = %q %+v", ev.Topic, jp)
	}

	// Subscribing to only learnedSkill.changed does not deliver proposed/job.
	other.expectNoEvent(200 * time.Millisecond)
}

func TestLearning_NoEventForSystemTask(t *testing.T) {
	t.Parallel()
	f := newLearnFx(t)
	ec := learnEvents(f)
	_, sysChat, err := f.db.CreateSystemTask(f.w.ID, "learning", f.w.Path)
	if err != nil {
		t.Fatal(err)
	}
	j, err := f.db.CreateLearningJob(store.LearningJob{WorkspaceID: f.w.ID, ChatID: &sysChat.ID, Kind: "reflect", TriggerKind: "explicit", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	f.api.LearningNotifier().NotifyJob(f.w.ID, j.ID, "running")
	ec.expectNoEvent(300 * time.Millisecond)
}
