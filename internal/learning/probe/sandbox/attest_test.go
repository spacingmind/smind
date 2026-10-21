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

func goodObs() Observation { return Observation{InitTools: AllowedTools(), CostUSD: 0.01, Turns: 2} }

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
	return c, &Launch{ArmDir: c.ArmDir, SettingsJSON: `{"x":1}`, prepared: true}
}

func TestAttestation_OnlyFromPassingSelfCheck(t *testing.T) {
	now := time.Now()
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name    string
		setup   func(c *Canary, l *Launch, o *Observation)
		wantErr string // "" = pass
	}{
		{"all good", func(c *Canary, l *Launch, o *Observation) { write(filepath.Join(c.ArmDir, "i.txt"), c.token) }, ""},
		{"tools in another order", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.InitTools = []string{"NotebookEdit", "Grep", "Glob", "Write", "Edit", "Read", "Bash"}
		}, ""},
		{"outside file present", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			write(filepath.Join(c.OutsideDir, "o.txt"), c.token)
		}, "outside"},
		{"inside file missing", func(c *Canary, l *Launch, o *Observation) {}, "inside"},
		{"wrong token", func(c *Canary, l *Launch, o *Observation) { write(filepath.Join(c.ArmDir, "i.txt"), "nope") }, "content"},
		{"token plus newline", func(c *Canary, l *Launch, o *Observation) { write(filepath.Join(c.ArmDir, "i.txt"), c.token+"\n") }, "content"},
		{"inside is a symlink", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.OutsideDir, "real"), c.token)
			_ = os.Symlink(filepath.Join(c.OutsideDir, "real"), filepath.Join(c.ArmDir, "i.txt"))
		}, "inside"},
		{"extra tool", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.InitTools = append(o.InitTools, "WebFetch")
		}, "tool list"},
		{"missing tool", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.InitTools = o.InitTools[1:]
		}, "tool list"},
		{"duplicate hides a missing tool", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.InitTools = append(o.InitTools[1:], o.InitTools[1])
		}, "tool list"},
		{"no init seen", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.InitTools = nil
		}, "tool list"},
		{"mcp server", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.MCPServers = 1
		}, "MCP"},
		{"plugin", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			o.Plugins = 1
		}, "plugin"},
		{"launch not from Prepare", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			l.prepared = false
		}, "Prepare"},
		{"launch for another arm", func(c *Canary, l *Launch, o *Observation) {
			write(filepath.Join(c.ArmDir, "i.txt"), c.token)
			l.ArmDir = c.OutsideDir
		}, "canary arm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, l := canaryFixture(t)
			o := goodObs()
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
			if strings.Contains(err.Error(), c.token) {
				t.Error("error leaks the token")
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
	att, err := c.Verify(goodObs(), l, now)
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

// runFakeCanary drives RunCanary against the stand-in CLI.
func runFakeCanary(t *testing.T, writeInside bool, init, result map[string]any) (Attestation, Observation, error) {
	t.Helper()
	if !Supported() {
		t.Skip("macOS only")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCanary(filepath.Join(root))
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
	cli := fakeCLI(t, root, map[string]string{"SANDBOX_FAKE_WRITES": enc(writes), "SANDBOX_FAKE_INIT": enc(init), "SANDBOX_FAKE_RESULT": enc(result)})
	l, err := Prepare(Config{ArmDir: c.ArmDir, PrivateDir: priv, RealCLI: cli})
	if err != nil {
		t.Fatal(err)
	}
	return RunCanary(context.Background(), l, c, "claude-haiku-5-5", nil)
}

func TestRunCanary_StandIn(t *testing.T) {
	init := map[string]any{"tools": AllowedTools(), "mcp_servers": []any{}, "plugins": []any{
		map[string]any{"name": "p", "path": "builtin", "source": "p@builtin"}}}
	att, obs, err := runFakeCanary(t, true, init, map[string]any{"total_cost_usd": 0.0123, "permission_denials": []any{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !att.Valid(time.Now()) || obs.CostUSD != 0.0123 || obs.Turns != 2 || obs.PermissionDenials != 1 || obs.Plugins != 0 {
		t.Errorf("att=%+v obs=%+v", att, obs)
	}
	// A failing canary still reports its cost and returns no attestation.
	att, obs, err = runFakeCanary(t, false, init, map[string]any{"total_cost_usd": 0.02})
	if !errors.Is(err, ErrCanaryFailed) || att.Valid(time.Now()) || obs.CostUSD != 0.02 {
		t.Errorf("failing canary: att=%+v obs=%+v err=%v", att, obs, err)
	}
	// A user plugin or an extra tool fails the check.
	bad := map[string]any{"tools": append(AllowedTools(), "WebFetch"), "plugins": []any{map[string]any{"name": "u", "path": "/x", "source": "u@x"}}}
	if _, obs, err := runFakeCanary(t, true, bad, nil); !errors.Is(err, ErrCanaryFailed) || obs.Plugins != 1 {
		t.Errorf("bad init accepted: obs=%+v err=%v", obs, err)
	}
}
