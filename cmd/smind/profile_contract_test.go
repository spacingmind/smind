package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

func TestProfileAdd_ContractFlags(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	child, err := reg.Create(store.AgentProfile{Name: "child", Provider: "glm"})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	var code int
	captureStdout(t, func() {
		code = run([]string{"profile", "add", "sup", "claude-native", "--orchestration-tools=true", "--instructions=coordinate",
			"--skills=smind-leader", "--child-policy=profiles", "--child-profiles=" + strconv.FormatInt(child.ID, 10)})
	})
	if code != 0 {
		t.Fatalf("run(profile add) = %d, want 0", code)
	}
	list, _ := reg.List()
	got := list[len(list)-1]
	if got.OrchestrationTools == nil || !*got.OrchestrationTools || got.Instructions != "coordinate" ||
		!reflect.DeepEqual(got.Skills, []string{"smind-leader"}) || got.ChildPolicy != "profiles" ||
		!reflect.DeepEqual(got.ChildProfileIDs, []int64{child.ID}) {
		t.Fatalf("stored = %+v", got)
	}

	captureStdout(t, func() {
		code = run([]string{"profile", "add", "peer", "claude-native", "--orchestration-tools=false"})
	})
	list, _ = reg.List()
	peer := list[len(list)-1]
	if code != 0 || peer.OrchestrationTools == nil || *peer.OrchestrationTools || peer.ChildPolicy != "none" {
		t.Fatalf("peer code=%d stored=%+v", code, peer)
	}

	stderr := captureStderr(t, func() {
		code = run([]string{"profile", "add", "bad", "claude-native", "--orchestration-tools=maybe"})
	})
	if code != 2 {
		t.Fatalf("bad --orchestration-tools = %d, want 2; stderr %q", code, stderr)
	}
}

func TestProfileAdd_InstructionsFileAndFlagConflict(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	file := filepath.Join(t.TempDir(), "role.md")
	if err := os.WriteFile(file, []byte("from a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var code int
	captureStdout(t, func() {
		code = run([]string{"profile", "add", "filed", "claude-native", "--instructions-file", file})
	})
	if code != 0 {
		t.Fatalf("run(profile add --instructions-file) = %d, want 0", code)
	}
	list, _ := reg.List()
	if len(list) != 1 || list[0].Instructions != "from a file\n" {
		t.Fatalf("stored = %+v", list)
	}

	stderr := captureStderr(t, func() {
		code = run([]string{"profile", "add", "both", "claude-native", "--instructions=x", "--instructions-file=" + file})
	})
	if code == 0 || !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("both flags = %d, stderr %q; want an error", code, stderr)
	}
	if list, _ = reg.List(); len(list) != 1 {
		t.Fatalf("conflicting add created a profile: %+v", list)
	}
}

func TestProfileLs_ShowsRole(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	yes, no := true, false
	for _, p := range []store.AgentProfile{
		{Name: "legacy", Provider: "glm"},
		{Name: "pee", Provider: "glm", OrchestrationTools: &no},
		{Name: "led", Provider: "glm", OrchestrationTools: &yes, ChildPolicy: "leaf"},
	} {
		if _, err := reg.Create(p); err != nil {
			t.Fatalf("create %s: %v", p.Name, err)
		}
	}
	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "ls"}) })
	if code != 0 {
		t.Fatalf("profile ls = %d", code)
	}
	if !strings.Contains(out, "ROLE") {
		t.Errorf("missing ROLE header:\n%s", out)
	}
	for name, role := range map[string]string{"legacy": "-", "pee": "peer", "led": "lead"} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[1] == name {
				found = true
				if f[len(f)-1] != role {
					t.Errorf("%s role = %q, want %q (line %q)", name, f[len(f)-1], role, line)
				}
			}
		}
		if !found {
			t.Errorf("no row for %s:\n%s", name, out)
		}
	}
}

func TestProfileRole_Table(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		name string
		p    store.AgentProfile
		want string
	}{
		{"unmanaged", store.AgentProfile{}, "-"},
		{"unmanaged with policy", store.AgentProfile{ChildPolicy: "leaf"}, "-"},
		{"peer", store.AgentProfile{OrchestrationTools: &no, ChildPolicy: "none"}, "peer"},
		{"lead", store.AgentProfile{OrchestrationTools: &yes, ChildPolicy: "leaf"}, "lead"},
		{"supervisor", store.AgentProfile{OrchestrationTools: &yes, ChildPolicy: "profiles"}, "supervisor"},
		{"orchestrator", store.AgentProfile{OrchestrationTools: &yes, ChildPolicy: "none"}, "orchestrator"},
		{"orchestrator unset policy", store.AgentProfile{OrchestrationTools: &yes}, "orchestrator"},
	} {
		if got := profileRole(c.p); got != c.want {
			t.Errorf("%s: profileRole = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMCPProfileList_ContractFieldsNoInstructions(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	var child struct {
		ID int64 `json:"ID"`
	}
	if err := client.Call(context.Background(), "profile.create", map[string]any{"name": "child", "provider": "glm"}, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := client.Call(context.Background(), "profile.create", map[string]any{
		"name": "sup", "provider": "claude-native", "orchestrationTools": true, "instructions": "SECRET ROLE TEXT",
		"skills": []string{"smind-leader"}, "childPolicy": "profiles", "childProfileIds": []int64{child.ID},
	}, nil); err != nil {
		t.Fatalf("create sup: %v", err)
	}

	var out struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if isErr := callMCPTool(t, env.cs, "profile_list", map[string]any{}, &out); isErr {
		t.Fatal("profile_list returned an error result")
	}
	if len(out.Profiles) != 2 {
		t.Fatalf("profiles = %+v", out.Profiles)
	}
	plain, sup := out.Profiles[0], out.Profiles[1]
	if _, ok := plain["orchestrationTools"]; ok {
		t.Errorf("unmanaged profile has orchestrationTools: %+v", plain)
	}
	if _, ok := plain["skills"]; ok {
		t.Errorf("profile without skills has skills: %+v", plain)
	}
	if _, ok := plain["hasInstructions"]; ok {
		t.Errorf("profile without instructions has hasInstructions: %+v", plain)
	}
	if plain["role"] != "-" {
		t.Errorf("plain role = %v, want -", plain["role"])
	}
	if sup["orchestrationTools"] != true || sup["role"] != "supervisor" || sup["childPolicy"] != "profiles" ||
		!reflect.DeepEqual(sup["childProfileIds"], []any{float64(child.ID)}) ||
		!reflect.DeepEqual(sup["skills"], []any{"smind-leader"}) || sup["hasInstructions"] != true {
		t.Errorf("sup = %+v", sup)
	}
	for _, p := range out.Profiles {
		if _, ok := p["instructions"]; ok {
			t.Errorf("instructions key leaked: %+v", p)
		}
		if _, ok := p["Instructions"]; ok {
			t.Errorf("Instructions key leaked: %+v", p)
		}
	}
	rawB, _ := json.Marshal(out)
	raw := string(rawB)
	if strings.Contains(raw, "SECRET ROLE TEXT") {
		t.Errorf("instruction text leaked: %s", raw)
	}
}

func TestMCPServer_NoProfileWriteTools(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range res.Tools {
		if strings.HasPrefix(tool.Name, "profile_") && tool.Name != "profile_list" {
			t.Errorf("MCP tool %q exposes profile writes; profiles are human-authored", tool.Name)
		}
	}
}
