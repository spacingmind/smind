package workspace

import "testing"

func TestManager_Spaces(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "My Workspace", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	empty, err := m.ListSpaces(w.ID)
	if err != nil {
		t.Fatalf("ListSpaces() error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListSpaces() = %v, want empty", empty)
	}

	sp, err := m.CreateSpace(w.ID, "feature-x", `{"FOO":"bar"}`)
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	if sp.ID == 0 {
		t.Fatalf("CreateSpace() returned zero id")
	}
	if sp.WorkspaceID != w.ID || sp.Title != "feature-x" || sp.EnvData != `{"FOO":"bar"}` {
		t.Fatalf("CreateSpace() = %+v, unexpected fields", sp)
	}

	got, err := m.GetSpace(sp.ID)
	if err != nil {
		t.Fatalf("GetSpace() error = %v", err)
	}
	if got.ID != sp.ID || got.Title != sp.Title {
		t.Fatalf("GetSpace() = %+v, want %+v", got, sp)
	}

	list, err := m.ListSpaces(w.ID)
	if err != nil {
		t.Fatalf("ListSpaces() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != sp.ID {
		t.Fatalf("ListSpaces() = %+v, want [%+v]", list, sp)
	}
}

func TestManager_GetSpaceMissing(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)

	if _, err := m.GetSpace(999); err == nil {
		t.Fatal("GetSpace() error = nil, want error for missing space")
	}
}
