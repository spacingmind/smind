package wire

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWireReadTypesHaveNoSecretFields(t *testing.T) {
	forbidden := []string{"path", "env", "token", "secret", "password", "key", "account", "cwd", "worktree", "url"}
	types := []any{
		Prov{}, InfoParams{}, WorkspaceListParams{}, TaskListParams{}, TaskGetParams{},
		RunStatusParams{}, TaskActivityParams{}, Limits{}, Info{}, Workspace{},
		WorkspaceList{}, Origin{}, Task{}, TaskList{}, RunStatus{}, Event{}, Activity{},
	}
	seen := map[reflect.Type]bool{}
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || rt == reflect.TypeOf(time.Time{}) || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := strings.ToLower(strings.Split(f.Tag.Get("json"), ",")[0])
			if name == "" {
				t.Errorf("%s.%s has no json name", rt.Name(), f.Name)
			}
			if name != "repourl" {
				for _, bad := range forbidden {
					if strings.Contains(name, bad) {
						t.Errorf("%s.%s json name %q contains %q", rt.Name(), f.Name, name, bad)
					}
				}
			}
			walk(f.Type)
		}
	}
	for _, v := range types {
		walk(reflect.TypeOf(v))
	}
}

func TestWireProvRoundTrip(t *testing.T) {
	in := Prov{Origin: "h1", Trace: "t1", Hops: []string{"a"}, ReqID: "r1", TS: 1700000000}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"reqId"`, `"ts"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json %s missing %s", b, want)
		}
	}
	var out Prov
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip: got %+v want %+v", out, in)
	}
}

// TestFleetWire_ActionRoundTrip_UnknownKindTolerated: a run status carries the
// failure action, and an action kind this daemon does not know survives a
// decode/encode untouched (a newer peer).
func TestFleetWire_ActionRoundTrip_UnknownKindTolerated(t *testing.T) {
	in := `{"runId":"r","taskId":1,"state":"error","provider":"glm","startedAt":"2026-11-01T00:00:00Z","failureKind":"martian_kind","action":{"kind":"martian","label":"Do the thing"},"costUsd":null}`
	var st RunStatus
	if err := json.Unmarshal([]byte(in), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Action == nil || st.Action.Kind != "martian" || st.Action.Label != "Do the thing" {
		t.Fatalf("action = %+v", st.Action)
	}
	out, err := json.Marshal(st)
	if err != nil || !strings.Contains(string(out), `"action":{"kind":"martian","label":"Do the thing"}`) {
		t.Fatalf("re-encoded = %s (%v)", out, err)
	}
	b, _ := json.Marshal(RunStatus{RunID: "r"})
	if strings.Contains(string(b), "action") {
		t.Fatalf("no failure, no action key: %s", b)
	}
}
