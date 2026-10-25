package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

// archivedHistoryEnv is a daemon with a workspace holding, via the CLI: live
// "live-root", and an archived "old-root" with a live-then-archived child
// "old-child". old-child has one persisted run (seeded straight into the
// store, so the registry does not hold it, as for an old archived task) that
// ran with profile "lead-profile".
type archivedHistoryEnv struct {
	client                        *wsclient.Client
	ws                            string
	liveID, oldRootID, oldChildID string
	profileID                     int64
	runID                         string
	daemonDB                      *store.Store
}

func newArchivedHistoryEnv(t *testing.T) *archivedHistoryEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	e := &archivedHistoryEnv{client: dialTestClient(t, srvURL)}
	wsID := createCLIHierarchyWorkspace(t, srvURL)
	e.ws = strconv.FormatInt(wsID, 10)
	e.daemonDB = daemonStoreFor(t, e.client)

	newTask := func(title string, extra ...string) string {
		code, out := runCaptured(t, append([]string{"task", "new", e.ws, title}, extra...))
		if code != 0 {
			t.Fatalf("task new %s = %d: %q", title, code, out)
		}
		return firstFieldOfOutputLine(t, out)
	}
	e.liveID = newTask("live-root")
	e.oldRootID = newTask("old-root")
	e.oldChildID = newTask("old-child", "--parent", e.oldRootID)

	var prof store.AgentProfile
	if err := e.client.Call(context.Background(), "profile.create", map[string]any{"name": "lead-profile", "provider": "glm"}, &prof); err != nil {
		t.Fatalf("profile.create: %v", err)
	}
	e.profileID = prof.ID

	childID, _ := strconv.ParseInt(e.oldChildID, 10, 64)
	now := time.Now().UTC().Add(-time.Hour)
	fin := now.Add(time.Minute)
	e.runID = "run-archived-1"
	if _, err := e.daemonDB.CreateRun(store.Run{
		ID: e.runID, TaskID: childID, Provider: "glm", Prompt: "old work", Status: "done",
		StartedAt: now, FinishedAt: &fin, ProfileID: prof.ID,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	for _, id := range []string{e.oldChildID, e.oldRootID} {
		if code, out := runCaptured(t, []string{"task", "archive", id, "--force"}); code != 0 {
			t.Fatalf("task archive %s = %d: %q", id, code, out)
		}
	}
	return e
}

func (e *archivedHistoryEnv) ls(t *testing.T, args ...string) string {
	t.Helper()
	code, out := runCaptured(t, append([]string{"task", "ls", e.ws}, args...))
	if code != 0 {
		t.Fatalf("task ls %v = %d: %q", args, code, out)
	}
	return out
}

// rowIDs is the first column of every table row after the header.
func rowIDs(out string) []string {
	var ids []string
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		ids = append(ids, strings.Fields(line)[0])
	}
	return ids
}

func sameIDs(got []string, want ...string) bool {
	return strings.Join(got, ",") == strings.Join(want, ",")
}

func TestTaskLs_DefaultHidesArchived(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	if got := rowIDs(e.ls(t)); !sameIDs(got, e.liveID) {
		t.Fatalf("default ls ids = %v, want only the live task %s", got, e.liveID)
	}
}

func TestTaskLs_ArchivedShowsOnlyArchived(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	out := e.ls(t, "--archived")
	if got := rowIDs(out); !sameIDs(got, e.oldRootID, e.oldChildID) {
		t.Fatalf("--archived ids = %v, want %s,%s:\n%s", got, e.oldRootID, e.oldChildID, out)
	}
	if !strings.Contains(strings.Fields(strings.Split(out, "\n")[0])[5], "ARCHIVED") {
		t.Fatalf("header = %q, want an ARCHIVED column", strings.Split(out, "\n")[0])
	}
}

func TestTaskLs_AllShowsBothWithMarker(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	out := e.ls(t, "--all")
	if got := rowIDs(out); !sameIDs(got, e.liveID, e.oldRootID, e.oldChildID) {
		t.Fatalf("--all ids = %v:\n%s", got, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n")[1:] {
		f := strings.Fields(line)
		marker := f[5] // ID TITLE STATUS PARENT SETUP ARCHIVED
		if (f[0] == e.liveID) != (marker == "-") {
			t.Errorf("row %q: ARCHIVED = %q, live task must be \"-\" and archived ones a date", line, marker)
		}
	}
	if code, _ := runCapturedStderr(t, []string{"task", "ls", e.ws, "--all", "--archived"}); code != 2 {
		t.Errorf("--all with --archived = %d, want usage error 2", code)
	}
}

func TestTaskLs_JSONCarriesHistoryFields(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	var rows []struct {
		ID           int64      `json:"id"`
		Title        string     `json:"title"`
		ParentTaskID *int64     `json:"parentTaskId"`
		ArchivedAt   *time.Time `json:"archivedAt"`
		Branch       *string    `json:"branch"`
		Status       string     `json:"status"`
		ProfileID    *int64     `json:"profileId"`
		ProfileName  *string    `json:"profileName"`
	}
	if err := json.Unmarshal([]byte(e.ls(t, "--all", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	byTitle := map[string]int{}
	for i, r := range rows {
		byTitle[r.Title] = i
	}
	child := rows[byTitle["old-child"]]
	if child.ArchivedAt == nil || child.ParentTaskID == nil || strconv.FormatInt(*child.ParentTaskID, 10) != e.oldRootID ||
		child.Branch == nil || child.Status == "" ||
		child.ProfileID == nil || *child.ProfileID != e.profileID || child.ProfileName == nil || *child.ProfileName != "lead-profile" {
		t.Fatalf("old-child = %+v, want archivedAt, parent %s, branch, status, profile lead-profile", child, e.oldRootID)
	}
	live := rows[byTitle["live-root"]]
	if live.ArchivedAt != nil || live.ProfileID != nil || live.ProfileName != nil {
		t.Fatalf("live-root = %+v, want null archivedAt/profile (no run)", live)
	}
}

func TestTaskRuns_ArchivedTaskShowsProfileAndParent(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	code, out := runCaptured(t, []string{"task", "runs", e.oldChildID})
	if code != 0 {
		t.Fatalf("task runs on archived task = %d: %q", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	hdr := strings.Fields(lines[0])
	if len(hdr) < 2 || hdr[len(hdr)-2] != "PROFILE" || hdr[len(hdr)-1] != "PARENT" || hdr[9] != "STATUS" {
		t.Fatalf("header = %v, want ...STATUS PROFILE PARENT appended", hdr)
	}
	if len(lines) != 2 {
		t.Fatalf("want one run row, got %q", out)
	}
	f := strings.Fields(lines[1])
	if f[0] != e.runID || f[len(f)-2] != "lead-profile" || f[len(f)-1] != e.oldRootID {
		t.Fatalf("row = %v, want run %s, profile lead-profile, parent %s", f, e.runID, e.oldRootID)
	}

	_, js := runCaptured(t, []string{"task", "runs", e.oldChildID, "--json"})
	var rows []map[string]any
	if err := json.Unmarshal([]byte(js), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("--json = %q (%v)", js, err)
	}
	r := rows[0]
	if r["profileName"] != "lead-profile" || r["profileId"] != float64(e.profileID) || r["parentTaskId"] != float64(mustInt(t, e.oldRootID)) || r["ID"] != e.runID || r["Status"] != "done" {
		t.Fatalf("--json row = %v, want profileName/profileId/parentTaskId plus the existing fields", r)
	}

	// The run's log is readable too (the registry does not hold it).
	if code, out := runCaptured(t, []string{"task", "logs", e.runID}); code != 0 {
		t.Fatalf("task logs of an archived task's run = %d: %q", code, out)
	}
}

func TestTaskRuns_DeletedProfilePrintsDeleted(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	if err := e.daemonDB.DeleteAgentProfile(e.profileID); err != nil {
		t.Fatalf("DeleteAgentProfile: %v", err)
	}
	code, out := runCaptured(t, []string{"task", "runs", e.oldChildID})
	if code != 0 {
		t.Fatalf("task runs = %d: %q", code, out)
	}
	f := strings.Fields(strings.Split(strings.TrimSpace(out), "\n")[1])
	if f[len(f)-2] != "(deleted)" {
		t.Fatalf("row = %v, want PROFILE (deleted)", f)
	}
}

func TestRunProfileCell(t *testing.T) {
	name := "n"
	for _, tc := range []struct {
		r    runSummaryRow
		want string
	}{
		{runSummaryRow{}, "-"},
		{runSummaryRow{ProfileID: 3}, "(deleted)"},
		{runSummaryRow{ProfileID: 3, ProfileName: &name}, "n"},
	} {
		if got := runProfileCell(tc.r); got != tc.want {
			t.Errorf("runProfileCell(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestTaskTree_ArchivedTaskAndFlag(t *testing.T) {
	e := newArchivedHistoryEnv(t)
	type node struct {
		ID         int64  `json:"id"`
		ArchivedAt string `json:"archivedAt"`
		Usage      struct {
			Own struct {
				Runs int64 `json:"runs"`
			} `json:"own"`
		} `json:"usage"`
	}
	tree := func(args ...string) []node {
		t.Helper()
		code, out := runCaptured(t, append([]string{"task", "tree"}, append(args, "--json")...))
		if code != 0 {
			t.Fatalf("task tree %v = %d: %q", args, code, out)
		}
		var res struct {
			Nodes []node `json:"nodes"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatal(err)
		}
		return res.Nodes
	}

	// Default stays live-only.
	if ns := tree(e.ws); len(ns) != 1 || ns[0].ID != mustInt(t, e.liveID) || ns[0].ArchivedAt != "" {
		t.Fatalf("default workspace tree = %+v, want only the live task", ns)
	}
	// --task <archived> shows it and its archived child, marked, with usage.
	ns := tree("--task", e.oldRootID)
	if len(ns) != 2 || ns[0].ID != mustInt(t, e.oldRootID) || ns[1].ID != mustInt(t, e.oldChildID) || ns[0].ArchivedAt == "" || ns[1].ArchivedAt == "" {
		t.Fatalf("--task archived tree = %+v, want the archived root and child, both with archivedAt", ns)
	}
	// Workspace --archived adds archived nodes alongside the live one.
	if ns := tree(e.ws, "--archived"); len(ns) != 3 {
		t.Fatalf("workspace --archived tree = %d nodes, want 3: %+v", len(ns), ns)
	}
	code, out := runCaptured(t, []string{"task", "tree", e.ws, "--archived"})
	if code != 0 || strings.Count(out, "archived") != 2 {
		t.Fatalf("table output = %d %q, want two lines marked archived", code, out)
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
