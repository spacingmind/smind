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

	plain := e.startOK(t, nil)
	row, err := e.db.GetRun(plain)
	if err != nil || row.ProfileID != 0 {
		t.Fatalf("plain run ProfileID = %d, %v, want 0", row.ProfileID, err)
	}
}
