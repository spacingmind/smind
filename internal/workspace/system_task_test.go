package workspace

import (
	"os"
	"testing"
)

// failNotifier panics on any call (the embedded Notifier is nil), proving
// CreateSystemTask never notifies.
type failNotifier struct{ Notifier }

func TestManager_CreateSystemTask_NoGitNoNotifierAndHidden(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	user, err := m.CreateTask(w.ID, nil, "user")
	if err != nil {
		t.Fatal(err)
	}
	m.SetNotifier(failNotifier{})

	sys, chat, err := m.CreateSystemTask(w.ID, "learning", repo)
	if err != nil {
		t.Fatalf("CreateSystemTask() error = %v", err)
	}
	if sys.Kind != "system" || sys.Branch != nil || sys.Port != nil || sys.WorktreePath == nil || *sys.WorktreePath != repo || chat.TaskID != sys.ID {
		t.Fatalf("system task = %+v chat = %+v", sys, chat)
	}
	if _, err := os.Stat(repo + "/.git"); err != nil {
		t.Fatal(err)
	}
	if tasks, err := m.ListTasks(w.ID); err != nil || len(tasks) != 1 || tasks[0].ID != user.ID {
		t.Fatalf("ListTasks = %+v, %v", tasks, err)
	}
	if got, err := m.GetTask(sys.ID); err != nil || got.ID != sys.ID {
		t.Fatalf("GetTask(system) = %+v, %v", got, err)
	}
}
