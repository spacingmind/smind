package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func TestRenderTaskList_ParentAndSetupColumns(t *testing.T) {
	seven := int64(7)
	b1, b2 := "smind/child-1", "smind/child-2"
	tasks := []store.Task{
		{ID: 1, Title: "root", Status: "open"},
		{ID: 2, Title: "kid-failed", Status: "open", ParentTaskID: &seven, SetupStatus: "failed", Branch: &b1},
		{ID: 3, Title: "kid-ok", Status: "open", ParentTaskID: &seven, SetupStatus: "ok", Branch: &b2},
	}
	var buf bytes.Buffer
	renderTaskList(&buf, tasks)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d:\n%s", len(lines), buf.String())
	}
	if got, want := strings.Fields(lines[0]), []string{"ID", "TITLE", "STATUS", "PARENT", "SETUP", "BRANCH"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("header = %v, want %v", got, want)
	}
	want := [][]string{
		{"1", "root", "open", "-", "-"},
		{"2", "kid-failed", "open", "7", "failed", "smind/child-1"},
		{"3", "kid-ok", "open", "7", "ok", "smind/child-2"},
	}
	for i, w := range want {
		if got := strings.Fields(lines[i+1]); strings.Join(got, " ") != strings.Join(w, " ") {
			t.Errorf("row %d = %v, want %v", i+1, got, w)
		}
	}
}

func TestRenderTaskList_NilBranch(t *testing.T) {
	var buf bytes.Buffer
	renderTaskList(&buf, []store.Task{{ID: 5, Title: "t", Status: "open"}})
	if !strings.Contains(buf.String(), "5") {
		t.Fatalf("missing row: %q", buf.String())
	}
}
