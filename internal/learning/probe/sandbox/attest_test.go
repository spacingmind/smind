//go:build !windows

package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func canaryFixture(t *testing.T) (*Canary, *Launch) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCanary(parent)
	if err != nil {
		t.Fatal(err)
	}
	return c, &Launch{ArmDir: c.ArmDir, ProjectionDir: c.ProjectionDir, SettingsJSON: `{"x":1}`, prepared: true}
}

// goodCalls are the four calls of a well-behaved canary run: the three writes
// and the read that must be refused are refused, the inside write is not.
func goodCalls(c *Canary) []ToolObs {
	cmd := func(f string) string { return `{"command":"printf %s T > ` + f + `"}` }
	denied := "Operation not permitted"
	return []ToolObs{
		{Name: "Bash", Input: cmd(filepath.Join(c.OutsideDir, "o.txt")), Result: denied, IsError: true},
		{Name: "Bash", Input: cmd(filepath.Join(c.ArmDir, "i.txt"))},
		{Name: "Bash", Input: `{"command":"cat ` + filepath.Join(c.OutsideDir, "secret.txt") + `"}`, Result: "cat: " + denied, IsError: true},
		{Name: "Bash", Input: cmd(filepath.Join(c.ProjectionDir, "p.txt")), Result: denied, IsError: true},
	}
}

func goodObs(c *Canary) Observation {
	return Observation{InitTools: AllowedTools(), CostUSD: 0.01, Turns: 5, Calls: goodCalls(c)}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAttestation_OnlyFromPassingSelfCheck(t *testing.T) {
	now := time.Now()
	inside := func(c *Canary) { writeFile(t, filepath.Join(c.ArmDir, "i.txt"), c.token) }
	without := func(o *Observation, file string) {
		var keep []ToolObs
		for _, x := range o.Calls {
			if !strings.Contains(x.Input, file) {
				keep = append(keep, x)
			}
		}
		o.Calls = keep
	}
	cases := []struct {
		name    string
		setup   func(c *Canary, l *Launch, o *Observation)
		wantErr string // "" = pass
	}{
		{"all good", func(c *Canary, l *Launch, o *Observation) { inside(c) }, ""},
		{"tools in another order", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			o.InitTools = []string{"NotebookEdit", "Grep", "Glob", "Write", "Edit", "Read", "Bash"}
		}, ""},
		{"outside file present", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			writeFile(t, filepath.Join(c.OutsideDir, "o.txt"), c.token)
		}, "outside"},
		{"inside file missing", func(c *Canary, l *Launch, o *Observation) {}, "inside"},
		{"wrong token", func(c *Canary, l *Launch, o *Observation) { writeFile(t, filepath.Join(c.ArmDir, "i.txt"), "nope") }, "content"},
		{"token plus newline", func(c *Canary, l *Launch, o *Observation) {
			writeFile(t, filepath.Join(c.ArmDir, "i.txt"), c.token+"\n")
		}, "content"},
		{"inside is a symlink", func(c *Canary, l *Launch, o *Observation) {
			writeFile(t, filepath.Join(c.OutsideDir, "real"), c.token)
			_ = os.Symlink(filepath.Join(c.OutsideDir, "real"), filepath.Join(c.ArmDir, "i.txt"))
		}, "inside"},
		{"extra tool", func(c *Canary, l *Launch, o *Observation) { inside(c); o.InitTools = append(o.InitTools, "WebFetch") }, "tool list"},
		{"missing tool", func(c *Canary, l *Launch, o *Observation) { inside(c); o.InitTools = o.InitTools[1:] }, "tool list"},
		{"duplicate hides a missing tool", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			o.InitTools = append(o.InitTools[1:], o.InitTools[1])
		}, "tool list"},
		{"no init seen", func(c *Canary, l *Launch, o *Observation) { inside(c); o.InitTools = nil }, "tool list"},
		{"mcp server", func(c *Canary, l *Launch, o *Observation) { inside(c); o.MCPServers = 1 }, "MCP"},
		{"plugin", func(c *Canary, l *Launch, o *Observation) { inside(c); o.Plugins = 1 }, "plugin"},
		{"launch not from Prepare", func(c *Canary, l *Launch, o *Observation) { inside(c); l.prepared = false }, "Prepare"},
		{"launch for another arm", func(c *Canary, l *Launch, o *Observation) { inside(c); l.ArmDir = c.OutsideDir }, "canary arm"},
		{"launch without the projection", func(c *Canary, l *Launch, o *Observation) { inside(c); l.ProjectionDir = "" }, "projection"},
		// A model that skips the call the sandbox must refuse proves nothing.
		{"skipped the outside write", func(c *Canary, l *Launch, o *Observation) { inside(c); without(o, "o.txt") }, "outside the arm"},
		{"skipped the secret read", func(c *Canary, l *Launch, o *Observation) { inside(c); without(o, "secret.txt") }, "outside secret"},
		{"skipped the projection write", func(c *Canary, l *Launch, o *Observation) { inside(c); without(o, "p.txt") }, "projection"},
		{"skipped the inside write but file exists", func(c *Canary, l *Launch, o *Observation) { inside(c); without(o, "i.txt") }, "inside the arm"},
		{"outside write call did not error", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			o.Calls[0].IsError, o.Calls[0].Result = false, ""
		}, "not reported as refused"},
		{"outside write refused only in words", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			o.Calls[0].IsError, o.Calls[0].Result = false, "touch: Operation not permitted"
		}, ""},
		{"inside call errored", func(c *Canary, l *Launch, o *Observation) { inside(c); o.Calls[1].IsError = true }, "errored"},
		{"secret in a tool result", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			o.Calls[2].Result, o.Calls[2].IsError = c.secret, false
		}, "secret reached a tool result"},
		{"secret in another call's result", func(c *Canary, l *Launch, o *Observation) { inside(c); o.Calls[3].Result = "x " + c.secret }, "secret reached a tool result"},
		{"secret in the transcript", func(c *Canary, l *Launch, o *Observation) { inside(c); o.Text = "the secret is " + c.secret }, "transcript"},
		{"secret seen past the clip", func(c *Canary, l *Launch, o *Observation) { inside(c); o.leaked = true }, "transcript"},
		{"projection file written", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			writeFile(t, filepath.Join(c.ProjectionDir, "p.txt"), c.token)
		}, "projection was not blocked"},
		{"projection listing changed", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			writeFile(t, filepath.Join(c.ProjectionDir, "other"), "x")
		}, "listing"},
		{"non-Bash call naming the file", func(c *Canary, l *Launch, o *Observation) {
			inside(c)
			for i := range o.Calls {
				o.Calls[i].Name = "Write"
			}
		}, "outside the arm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, l := canaryFixture(t)
			o := goodObs(c)
			tc.setup(c, l, &o)
			att, err := c.Verify(o, l, now)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if !att.Valid(now) || att.ArmDir() != c.ArmDir || att.Require(now) != nil {
					t.Errorf("attestation not valid: %+v", att)
				}
				return
			}
			if !errors.Is(err, ErrCanaryFailed) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want ErrCanaryFailed mentioning %q", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), c.token) || strings.Contains(err.Error(), c.secret) {
				t.Error("error leaks a token")
			}
			if att.Valid(now) || att != (Attestation{}) {
				t.Errorf("a failed check returned %+v", att)
			}
		})
	}
	var zero Attestation
	if zero.Valid(now) || !errors.Is(zero.Require(now), ErrNoAttestation) {
		t.Error("the zero Attestation must never be valid")
	}
}

func TestAttestation_StaleDisablesProbe(t *testing.T) {
	c, l := canaryFixture(t)
	if err := os.WriteFile(filepath.Join(c.ArmDir, "i.txt"), []byte(c.token), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	att, err := c.Verify(goodObs(c), l, now)
	if err != nil {
		t.Fatal(err)
	}
	if !att.Valid(now.Add(AttestationTTL)) {
		t.Error("invalid exactly at the TTL")
	}
	later := now.Add(AttestationTTL + time.Second)
	if att.Valid(later) || !errors.Is(att.Require(later), ErrStaleAttestation) {
		t.Error("still valid after the TTL")
	}
	if !att.Valid(now.Add(-30 * time.Second)) {
		t.Error("small clock skew must be tolerated")
	}
	earlier := now.Add(-2 * time.Minute)
	if att.Valid(earlier) || !errors.Is(att.Require(earlier), ErrStaleAttestation) {
		t.Error("an attestation dated in the future must be rejected")
	}
}

func TestRunCanary_UnsupportedOffMac(t *testing.T) {
	if Supported() {
		t.Skip("darwin")
	}
	if _, _, err := RunCanary(context.Background(), &Launch{}, &Canary{}, "m", nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

// runFakeCanary drives RunCanary against the stand-in CLI, which streams the
// given calls and performs the given writes (path -> content) itself.
func runFakeCanary(t *testing.T, writeInside bool, init, result map[string]any, edit func(c *Canary, calls []ToolObs) []ToolObs, text string) (Attestation, Observation, error) {
	t.Helper()
	if !Supported() {
		t.Skip("macOS only")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCanary(root)
	if err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(root, "priv")
	if err := os.Mkdir(priv, 0o700); err != nil {
		t.Fatal(err)
	}
	enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	writes := map[string]string{}
	if writeInside {
		writes[filepath.Join(c.ArmDir, "i.txt")] = c.token
	}
	calls := goodCalls(c)
	if edit != nil {
		calls = edit(c, calls)
	}
	var fake []fakeCall
	for i, x := range calls {
		var in struct{ Command string }
		_ = json.Unmarshal([]byte(x.Input), &in)
		fake = append(fake, fakeCall{ID: "t" + string(rune('a'+i)), Command: in.Command, Result: x.Result, IsError: x.IsError})
	}
	cli := fakeCLI(t, root, map[string]string{"SANDBOX_FAKE_WRITES": enc(writes), "SANDBOX_FAKE_INIT": enc(init), "SANDBOX_FAKE_RESULT": enc(result),
		"SANDBOX_FAKE_CALLS": enc(fake), "SANDBOX_FAKE_TEXT": text})
	l, err := Prepare(Config{ArmDir: c.ArmDir, ProjectionDir: c.ProjectionDir, PrivateDir: priv, RealCLI: cli})
	if err != nil {
		t.Fatal(err)
	}
	return RunCanary(context.Background(), l, c, "claude-haiku-5-5", nil)
}

func TestRunCanary_StandIn(t *testing.T) {
	init := map[string]any{"tools": AllowedTools(), "mcp_servers": []any{}, "plugins": []any{
		map[string]any{"name": "p", "path": "builtin", "source": "p@builtin"}}}
	att, obs, err := runFakeCanary(t, true, init, map[string]any{"total_cost_usd": 0.0123, "permission_denials": []any{"x"}}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !att.Valid(time.Now()) || obs.CostUSD != 0.0123 || obs.Turns != 2 || obs.PermissionDenials != 1 || obs.Plugins != 0 || len(obs.Calls) != 4 {
		t.Errorf("att=%+v obs=%+v", att, obs)
	}
	if obs.Calls[0].Name != "Bash" || !obs.Calls[0].IsError || !strings.Contains(obs.Calls[0].Result, "not permitted") || !strings.Contains(obs.Calls[0].Input, "o.txt") {
		t.Errorf("first call not collected from the stream: %+v", obs.Calls[0])
	}
	// A failing canary still reports its cost and returns no attestation.
	att, obs, err = runFakeCanary(t, false, init, map[string]any{"total_cost_usd": 0.02}, nil, "")
	if !errors.Is(err, ErrCanaryFailed) || att.Valid(time.Now()) || obs.CostUSD != 0.02 {
		t.Errorf("failing canary: att=%+v obs=%+v err=%v", att, obs, err)
	}
	// A user plugin or an extra tool fails the check.
	bad := map[string]any{"tools": append(AllowedTools(), "WebFetch"), "plugins": []any{map[string]any{"name": "u", "path": "/x", "source": "u@x"}}}
	if _, obs, err := runFakeCanary(t, true, bad, nil, nil, ""); !errors.Is(err, ErrCanaryFailed) || obs.Plugins != 1 {
		t.Errorf("bad init accepted: obs=%+v err=%v", obs, err)
	}
	// A model that skips the call the sandbox must refuse fails, even though
	// the inside write landed.
	skip := func(c *Canary, calls []ToolObs) []ToolObs { return calls[1:] }
	if att, _, err := runFakeCanary(t, true, init, nil, skip, ""); !errors.Is(err, ErrCanaryFailed) || att.Valid(time.Now()) {
		t.Errorf("a canary that skipped the outside write attested: %v", err)
	}
	// The secret in a result, however far past the 2 KB clip, fails it.
	leak := func(c *Canary, calls []ToolObs) []ToolObs {
		calls[2].Result, calls[2].IsError = strings.Repeat("x", 5000)+c.secret, false
		return calls
	}
	if att, obs, err := runFakeCanary(t, true, init, nil, leak, ""); !errors.Is(err, ErrCanaryFailed) || att.Valid(time.Now()) || len(obs.Calls[2].Result) > toolObsClip {
		t.Errorf("a leaked secret past the clip attested or was not clipped: %v", err)
	}
}
