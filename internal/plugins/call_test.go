package plugins

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRPCContribution_ManifestValidation(t *testing.T) {
	proc := `"process":{"command":["x"]},`
	cases := map[string]struct{ extra, want string }{
		"no process":         {`"contributes":{"rpc":[{"name":"a"}]}`, "needs a plugin process"},
		"duplicate":          {proc + `"contributes":{"rpc":[{"name":"a"},{"name":"a"}]}`, "duplicate rpc"},
		"bad name":           {proc + `"contributes":{"rpc":[{"name":"a b"}]}`, "not a valid name"},
		"timeout too high":   {proc + `"contributes":{"rpc":[{"name":"a","timeoutMs":30001}]}`, "out of range"},
		"timeout negative":   {proc + `"contributes":{"rpc":[{"name":"a","timeoutMs":-1}]}`, "out of range"},
		"command undeclared": {proc + `"contributes":{"commands":[{"name":"c","rpc":"nope"}]}`, "not declared in contributes.rpc"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseManifest([]byte(manifestWith("demo", "1.0.0", c.extra)), "0.15.0")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	ok := proc + `"contributes":{"rpc":[{"name":"a","timeoutMs":30000}],"commands":[{"name":"c","rpc":"a"}]}`
	if _, err := ParseManifest([]byte(manifestWith("demo", "1.0.0", ok)), "0.15.0"); err != nil {
		t.Fatalf("valid rpc manifest rejected: %v", err)
	}
}

func TestSlashRpc_ListedWithoutPrompt_BuiltinAndCollisionRules(t *testing.T) {
	e := newContribEnv(t, nil)
	rpcCmd := `"process":{"command":["x"]},"contributes":{"rpc":[{"name":"gen"}],"commands":[{"name":"review","description":"d","rpc":"gen"}]}`
	enableDemo(t, e, "zeta", rpcCmd, map[string]string{})
	enableDemo(t, e, "alpha", `"contributes":{"commands":[{"name":"review","prompt":"c.md"}]}`, map[string]string{"c.md": "text"})
	byQ := map[string]Command{}
	for _, c := range e.m.Commands() {
		byQ[c.Qualified] = c
	}
	z := byQ["zeta:review"]
	if z.RPC != "gen" || z.Prompt != "" || z.Alias != "" {
		t.Fatalf("rpc command: %+v (no prompt, rpc named, bare name lost to alpha by id)", z)
	}
	if a := byQ["alpha:review"]; a.RPC != "" || a.Prompt != "text" || a.Alias != "review" {
		t.Fatalf("prompt command: %+v", a)
	}
	b, _ := json.Marshal(z)
	if strings.Contains(string(b), `"prompt"`) {
		t.Fatalf("an rpc command carries no prompt key: %s", b)
	}
	got, _ := e.m.Get("zeta")
	var found bool
	for _, c := range got.Conflicts {
		found = found || (c.Kind == "command" && c.Name == "review")
	}
	if !found {
		t.Fatalf("the rpc command that lost the bare name must see the collision: %+v", got.Conflicts)
	}
}
