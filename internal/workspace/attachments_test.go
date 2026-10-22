package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/store"
)

func setWith(id string) string {
	return attach.EncodeSet(attach.Set{ID: id, Items: []attach.Item{{ID: "1", Kind: attach.KindFile, File: "01-a.txt"}}})
}

// TestDeleteTask_RemovesAttachmentSets: the sets of a deleted task's runs and
// queued prompts are handed to the cleanup hook after the rows are gone;
// another task's sets and a failed delete are untouched.
func TestDeleteTask_RemovesAttachmentSets(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	doomed, err := m.CreateTask(w.ID, nil, "doomed")
	if err != nil {
		t.Fatal(err)
	}
	keeper, err := m.CreateTask(w.ID, nil, "keeper")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "attachments")
	st := attach.NewStore(root)
	mk := func(id string) string {
		if err := os.MkdirAll(filepath.Join(root, id), 0o700); err != nil {
			t.Fatal(err)
		}
		return id
	}
	runSet, queueSet, keepSet := mk("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), mk("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), mk("cccccccccccccccccccccccccccccccc")
	if _, err := m.store.CreateRun(store.Run{ID: "r1", TaskID: doomed.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC(), Attachments: setWith(runSet)}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateRun(store.Run{ID: "r2", TaskID: keeper.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC(), Attachments: setWith(keepSet)}); err != nil {
		t.Fatal(err)
	}
	chat, err := m.store.CreateChat(doomed.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.EnqueueChatPromptAttach(chat.ID, "p", "{}", store.ChatQueueSourceHuman, 0, 0, 0, setWith(queueSet)); err != nil {
		t.Fatal(err)
	}
	var called int
	m.SetAttachmentCleanup(func(ids []string) {
		called++
		for _, id := range ids {
			_ = st.Remove(id)
		}
	})

	if _, err := m.DeleteTask(99999); err == nil {
		t.Fatal("deleting a missing task succeeded")
	}
	if called != 0 {
		t.Fatal("cleanup ran for a failed delete")
	}
	if _, err := m.DeleteTask(keeper.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, keepSet)); !os.IsNotExist(err) {
		t.Fatalf("deleted task's set still on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, runSet)); err != nil {
		t.Fatalf("another task's set removed: %v", err)
	}
	if _, err := m.DeleteTask(doomed.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{runSet, queueSet} {
		if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
			t.Fatalf("set %s still on disk: %v", id, err)
		}
	}
}

// TestDeleteSpaceAndWorkspace_RemoveAttachmentSets: the same cleanup runs when
// a task goes away through its space or its workspace (the cascade), and only
// for the tasks that cascade removes.
func TestDeleteSpaceAndWorkspace_RemoveAttachmentSets(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := m.CreateSpace(w.ID, "feature-x", "{}")
	if err != nil {
		t.Fatal(err)
	}
	inSpace, err := m.CreateTask(w.ID, &sp.ID, "in space")
	if err != nil {
		t.Fatal(err)
	}
	outside, err := m.CreateTask(w.ID, nil, "outside")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "attachments")
	st := attach.NewStore(root)
	mk := func(id string) string {
		if err := os.MkdirAll(filepath.Join(root, id), 0o700); err != nil {
			t.Fatal(err)
		}
		return id
	}
	spaceSet, outsideSet := mk("dddddddddddddddddddddddddddddddd"), mk("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	for _, r := range []store.Run{
		{ID: "rs", TaskID: inSpace.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC(), Attachments: setWith(spaceSet)},
		{ID: "ro", TaskID: outside.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC(), Attachments: setWith(outsideSet)},
	} {
		if _, err := m.store.CreateRun(r); err != nil {
			t.Fatal(err)
		}
	}
	m.SetAttachmentCleanup(func(ids []string) {
		for _, id := range ids {
			_ = st.Remove(id)
		}
	})
	exists := func(id string) bool {
		_, err := os.Stat(filepath.Join(root, id))
		return err == nil
	}

	if _, err := m.DeleteSpace(sp.ID); err != nil {
		t.Fatal(err)
	}
	if exists(spaceSet) {
		t.Error("the deleted space's task set is still on disk")
	}
	if !exists(outsideSet) {
		t.Error("a task outside the space lost its set")
	}
	if _, err := m.DeleteWorkspace(w.ID); err != nil {
		t.Fatal(err)
	}
	if exists(outsideSet) {
		t.Error("the deleted workspace's task set is still on disk")
	}
}
