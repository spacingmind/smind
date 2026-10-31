package registry

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestGrantValidate_Table(t *testing.T) {
	providers := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("provider-%d", i)
		}
		return out
	}
	cases := []struct {
		name   string
		mutate func(*Grant)
		field  string // empty: the grant must be valid
	}{
		{"valid default forward", func(g *Grant) {}, ""},
		{"valid default reverse", func(g *Grant) { *g = DefaultReverseGrant() }, ""},
		{"empty ops", func(g *Grant) { g.Ops = nil }, "grant.ops"},
		{"unknown op", func(g *Grant) { g.Ops = []string{OpRead, "x"} }, "grant.ops"},
		{"duplicate op", func(g *Grant) { g.Ops = []string{OpRead, OpRead, OpSend} }, "grant.ops"},
		{"missing read", func(g *Grant) { g.Ops = []string{OpSend} }, "grant.ops"},
		{"create without read", func(g *Grant) { g.Ops = []string{OpCreate, OpSend, OpStop} }, "grant.ops"},
		{"all and ids", func(g *Grant) { g.WorkspacesRead = WorkspaceScope{All: true, IDs: []int64{1}} }, "grant.workspacesRead"},
		{"zero id", func(g *Grant) { g.WorkspacesRead = WorkspaceScope{IDs: []int64{0}} }, "grant.workspacesRead"},
		{"duplicate ids", func(g *Grant) { g.WorkspacesRead = WorkspaceScope{IDs: []int64{2, 2}} }, "grant.workspacesRead"},
		{"bad provider", func(g *Grant) { g.Providers = []string{"Bad Name"} }, "grant.providers"},
		{"too many providers", func(g *Grant) { g.Providers = providers(65) }, "grant.providers"},
		{"too many live agents", func(g *Grant) { g.MaxLiveAgents = 65 }, "grant.maxLiveAgents"},
		{"negative wall seconds", func(g *Grant) { g.MaxWallSeconds = -1 }, "grant.maxWallSeconds"},
		{"cost NaN", func(g *Grant) { g.MaxCostUSD = math.NaN() }, "grant.maxCostUsd"},
		{"cost over cap", func(g *Grant) { g.MaxCostUSD = 10001 }, "grant.maxCostUsd"},
		{"bad permission mode", func(g *Grant) { g.PermissionMode = "bypassPermissions" }, "grant.permissionMode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := DefaultForwardGrant()
			tc.mutate(&g)
			err := g.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error naming %s", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name %s", err, tc.field)
			}
		})
	}
}

func TestGrantValidateForApproval_RequiresWriteWorkspace(t *testing.T) {
	forward := DefaultForwardGrant()
	if err := forward.ValidateForApproval(); err == nil || !strings.Contains(err.Error(), "grant.workspacesWrite") {
		t.Fatalf("default forward grant must be refused for approval naming grant.workspacesWrite, got %v", err)
	}

	withWrite := DefaultForwardGrant()
	withWrite.WorkspacesWrite = []int64{3}
	if err := withWrite.ValidateForApproval(); err != nil {
		t.Fatalf("forward grant with a write workspace must pass, got %v", err)
	}

	// Read-only grants never write, so they need no write workspace.
	if err := DefaultReverseGrant().ValidateForApproval(); err != nil {
		t.Fatalf("default reverse grant must pass, got %v", err)
	}

	noAgents := DefaultForwardGrant()
	noAgents.WorkspacesWrite = []int64{3}
	noAgents.MaxLiveAgents = 0
	if err := noAgents.ValidateForApproval(); err == nil || !strings.Contains(err.Error(), "grant.maxLiveAgents") {
		t.Fatalf("create with MaxLiveAgents 0 must be refused naming grant.maxLiveAgents, got %v", err)
	}
}

func TestDefaultForwardGrant_S5(t *testing.T) {
	g := DefaultForwardGrant()
	wantOps := []string{OpRead, OpCreate, OpSend, OpStop}
	if !reflect.DeepEqual(g.Ops, wantOps) {
		t.Fatalf("ops = %v, want %v", g.Ops, wantOps)
	}
	if !g.WorkspacesRead.All || len(g.WorkspacesRead.IDs) != 0 {
		t.Fatalf("workspacesRead = %+v, want all", g.WorkspacesRead)
	}
	if g.WorkspacesWrite == nil || len(g.WorkspacesWrite) != 0 {
		t.Fatalf("workspacesWrite = %#v, want empty non-nil", g.WorkspacesWrite)
	}
	if g.Providers == nil || len(g.Providers) != 0 {
		t.Fatalf("providers = %#v, want empty non-nil", g.Providers)
	}
	if g.MaxLiveAgents != 2 {
		t.Fatalf("maxLiveAgents = %d, want 2", g.MaxLiveAgents)
	}
	if g.MaxWallSeconds != 7200 {
		t.Fatalf("maxWallSeconds = %d, want 7200", g.MaxWallSeconds)
	}
	if g.MaxCostUSD != 5.00 {
		t.Fatalf("maxCostUsd = %g, want 5.00", g.MaxCostUSD)
	}
	if g.PermissionMode != "" {
		t.Fatalf("permissionMode = %q, want empty", g.PermissionMode)
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("default forward grant must validate: %v", err)
	}
}

func TestDefaultReverseGrant_S5(t *testing.T) {
	g := DefaultReverseGrant()
	wantOps := []string{OpRead}
	if !reflect.DeepEqual(g.Ops, wantOps) {
		t.Fatalf("ops = %v, want %v", g.Ops, wantOps)
	}
	if !g.WorkspacesRead.All || len(g.WorkspacesRead.IDs) != 0 {
		t.Fatalf("workspacesRead = %+v, want all", g.WorkspacesRead)
	}
	if g.WorkspacesWrite == nil || len(g.WorkspacesWrite) != 0 {
		t.Fatalf("workspacesWrite = %#v, want empty non-nil", g.WorkspacesWrite)
	}
	if g.Providers == nil || len(g.Providers) != 0 {
		t.Fatalf("providers = %#v, want empty non-nil", g.Providers)
	}
	if g.MaxLiveAgents != 0 || g.MaxWallSeconds != 0 || g.MaxCostUSD != 0 {
		t.Fatalf("budget limits = %d/%d/%g, want 0/0/0", g.MaxLiveAgents, g.MaxWallSeconds, g.MaxCostUSD)
	}
	if g.PermissionMode != "" {
		t.Fatalf("permissionMode = %q, want empty", g.PermissionMode)
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("default reverse grant must validate: %v", err)
	}
}

func TestGrantParse_RoundTripAndUnknownField(t *testing.T) {
	for _, g := range []Grant{DefaultForwardGrant(), DefaultReverseGrant()} {
		parsed, err := ParseGrant(g.JSON())
		if err != nil {
			t.Fatalf("parse %s: %v", g.JSON(), err)
		}
		if !reflect.DeepEqual(parsed, g) {
			t.Fatalf("round trip changed the grant: %+v != %+v", parsed, g)
		}
	}

	raw := strings.Replace(DefaultForwardGrant().JSON(), "{", `{"evil":1,`, 1)
	if _, err := ParseGrant(raw); err == nil {
		t.Fatal("an unknown field must be refused")
	}

	// Nil slices marshal as [] so a stored grant never carries a JSON null.
	for _, want := range []string{`"ops":[]`, `"workspacesWrite":[]`, `"providers":[]`} {
		if out := (Grant{}).JSON(); !strings.Contains(out, want) {
			t.Fatalf("zero grant JSON %s lacks %s", out, want)
		}
	}
	if _, err := ParseGrant(""); err == nil {
		t.Fatal("an empty grant string must be refused")
	}
}

func TestGrantAllowsAndScopes(t *testing.T) {
	g := DefaultForwardGrant()
	g.WorkspacesWrite = []int64{5}
	for _, op := range []string{OpRead, OpCreate, OpSend, OpStop} {
		if !g.Allows(op) {
			t.Fatalf("default forward grant must allow %s", op)
		}
	}
	if g.Allows("fly") {
		t.Fatal("grant must not allow an unknown op")
	}

	if !g.CanRead(99) {
		t.Fatal("an all scope must read every workspace")
	}
	if !g.CanWrite(5) {
		t.Fatal("workspace 5 must be writable")
	}
	if g.CanWrite(6) {
		t.Fatal("workspace 6 must not be writable")
	}

	scoped := DefaultReverseGrant()
	scoped.WorkspacesRead = WorkspaceScope{IDs: []int64{1, 2}}
	if !scoped.CanRead(2) {
		t.Fatal("a listed workspace must be readable")
	}
	if scoped.CanRead(3) {
		t.Fatal("an unlisted workspace must not be readable")
	}
	if scoped.CanRead(0) {
		t.Fatal("workspace 0 must not be readable by a listed scope")
	}
	if scoped.CanWrite(1) {
		t.Fatal("a read-only grant must write nowhere")
	}
}

func TestAliasValidation(t *testing.T) {
	for _, alias := range []string{"a", "mac-1", "a" + strings.Repeat("b", 31)} {
		if err := ValidateAlias(alias); err != nil {
			t.Fatalf("alias %q must be valid, got %v", alias, err)
		}
	}
	for _, alias := range []string{"", "1abc", "Abc", "a_b", "-a", strings.Repeat("a", 33), "a b"} {
		err := ValidateAlias(alias)
		if err == nil {
			t.Fatalf("alias %q must be refused", alias)
		}
		if !strings.Contains(err.Error(), "alias:") {
			t.Fatalf("error %q does not name alias", err)
		}
	}
}

func TestLabelValidation_RejectsControl(t *testing.T) {
	if err := ValidateLabel("Lou's laptop"); err != nil {
		t.Fatalf("a plain label must pass, got %v", err)
	}
	err := ValidateLabel("bad\x00label")
	if err == nil {
		t.Fatal("a control character must be refused")
	}
	if !strings.Contains(err.Error(), "label:") {
		t.Fatalf("error %q does not name label", err)
	}
}
