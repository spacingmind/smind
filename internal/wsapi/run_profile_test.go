package wsapi

import (
	"strconv"
	"testing"
	"time"
)

func TestRunStart_ProfileIDRecordedAndValidated(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)

	var prof struct {
		ID int64 `json:"ID"`
	}
	e.mustCall(t, "profile.create", map[string]any{"name": "p", "provider": "glm"}, &prof)

	runID := e.startOK(t, map[string]any{"profileId": prof.ID})
	waitRunDone := func(id string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if row, err := e.db.GetRun(id); err == nil && row.Status != "running" {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("run %s did not finish", id)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitRunDone(runID)
	if row, err := e.db.GetRun(runID); err != nil || row.ProfileID != prof.ID {
		t.Fatalf("run ProfileID = %d, %v, want %d", row.ProfileID, err, prof.ID)
	}

	env := e.start(t, map[string]any{"profileId": prof.ID + 1000})
	want := "run.start: profile " + strconv.FormatInt(prof.ID+1000, 10) + " not found"
	if env.Error == nil || env.Error.Message != want {
		t.Fatalf("unknown profile error = %+v, want %q", env.Error, want)
	}

	// ADR-0041 sticky seats: an omitted profileId would now inherit prof on
	// this chat; an explicit 0 is the plain run this check wants.
	plain := e.startOK(t, map[string]any{"profileId": 0})
	row, err := e.db.GetRun(plain)
	if err != nil || row.ProfileID != 0 {
		t.Fatalf("plain run ProfileID = %d, %v, want 0", row.ProfileID, err)
	}
}

// stickyEnv starts a first run under a fresh profile and returns the profile
// id, so the cases below can send follow-ups to the same chat.
func stickyEnv(t *testing.T) (*attachEnv, int64) {
	t.Helper()
	e := newAttachEnv(t, "", false)
	var prof struct {
		ID int64 `json:"ID"`
	}
	e.mustCall(t, "profile.create", map[string]any{"name": "seat", "provider": "glm"}, &prof)
	e.waitRunDone(t, e.startOK(t, map[string]any{"profileId": prof.ID}))
	return e, prof.ID
}

func (e *attachEnv) waitRunDone(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if row, err := e.db.GetRun(id); err == nil && row.Status != "running" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not finish", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *attachEnv) followUpProfile(t *testing.T, extra map[string]any) int64 {
	t.Helper()
	id := e.startOK(t, extra)
	e.waitRunDone(t, id)
	row, err := e.db.GetRun(id)
	if err != nil {
		t.Fatal(err)
	}
	return row.ProfileID
}

func TestRunStart_ProfileIdAbsentInherits(t *testing.T) {
	t.Parallel()
	e, prof := stickyEnv(t)
	if got := e.followUpProfile(t, nil); got != prof {
		t.Fatalf("follow-up ProfileID = %d, want %d (inherited)", got, prof)
	}
}

func TestRunStart_ProfileIdNullInherits(t *testing.T) {
	t.Parallel()
	e, prof := stickyEnv(t)
	if got := e.followUpProfile(t, map[string]any{"profileId": nil}); got != prof {
		t.Fatalf("follow-up ProfileID = %d, want %d (inherited)", got, prof)
	}
}

func TestRunStart_ProfileIdZeroMeansNone(t *testing.T) {
	t.Parallel()
	e, _ := stickyEnv(t)
	if got := e.followUpProfile(t, map[string]any{"profileId": 0}); got != 0 {
		t.Fatalf("explicit-none ProfileID = %d, want 0", got)
	}
	if got := e.followUpProfile(t, nil); got != 0 {
		t.Fatalf("next omitted ProfileID = %d, want 0 (stays plain)", got)
	}
}

func TestRunStart_ProfileIdNegativeRejected(t *testing.T) {
	t.Parallel()
	e, _ := stickyEnv(t)
	env := e.start(t, map[string]any{"profileId": -1})
	if env.Error == nil || env.Error.Message != "run.start: profile -1 not found" {
		t.Fatalf("negative profile error = %+v", env.Error)
	}
}
