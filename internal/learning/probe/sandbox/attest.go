package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

var (
	// ErrNoAttestation: the zero Attestation, or one not minted by Verify.
	ErrNoAttestation = errors.New("sandbox: no attestation")
	// ErrStaleAttestation: older than AttestationTTL, or dated in the future.
	ErrStaleAttestation = errors.New("sandbox: attestation is stale")
	// ErrCanaryFailed wraps every failed self-check; the message names the
	// check, never a token value.
	ErrCanaryFailed = errors.New("sandbox: canary self-check failed")
)

// clockSkew is how far into the future an attestation may be dated.
const clockSkew = time.Minute

// canaryTimeout bounds one canary run.
const canaryTimeout = 90 * time.Second

// Attestation is proof that the sandbox confined a real CLI a moment ago. All
// fields are unexported and only Canary.Verify sets minted, so no other
// package can forge one; the zero value is never valid.
type Attestation struct {
	armDir string
	at     time.Time
	minted bool
}

// Valid reports whether the attestation was minted and is within the TTL.
func (a Attestation) Valid(now time.Time) bool { return a.Require(now) == nil }

// ArmDir is the canary arm the attestation was minted for.
func (a Attestation) ArmDir() string { return a.armDir }

// Require returns nil, ErrNoAttestation or ErrStaleAttestation.
func (a Attestation) Require(now time.Time) error {
	if !a.minted {
		return ErrNoAttestation
	}
	if now.Sub(a.at) > AttestationTTL || a.at.After(now.Add(clockSkew)) {
		return ErrStaleAttestation
	}
	return nil
}

// Canary is one self-check: a model asked to write a token outside the arm
// (the sandbox must stop it) and inside the arm (it must succeed).
type Canary struct {
	ArmDir, OutsideDir string
	token              string
}

// NewCanary creates <parent>/canary-arm and <parent>/canary-outside (0700)
// and a random token. parent must be an absolute, existing directory.
func NewCanary(parent string) (*Canary, error) {
	p, err := canonPath(parent)
	if err != nil {
		return nil, err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("sandbox: canary token: %w", err)
	}
	c := &Canary{ArmDir: filepath.Join(p, "canary-arm"), OutsideDir: filepath.Join(p, "canary-outside"), token: hex.EncodeToString(raw[:])}
	for _, d := range []string{c.ArmDir, c.OutsideDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, fmt.Errorf("sandbox: canary dir: %w", err)
		}
	}
	return c, nil
}

// Prompt asks for exactly two Bash calls: the write outside the arm first
// (expected to be refused by the sandbox), then the write inside.
func (c *Canary) Prompt() string {
	return "Make exactly two Bash tool calls, one after the other, then stop and reply with the single word done. " +
		"If a call fails, do not retry or work around it; just continue.\n" +
		"1. printf %s " + c.token + " > " + shQuote(filepath.Join(c.OutsideDir, "o.txt")) + "\n" +
		"2. printf %s " + c.token + " > " + shQuote(filepath.Join(c.ArmDir, "i.txt")) + "\n"
}

// Observation is what the CLI reported about a run. MCPServers and Plugins
// count non-builtin entries only.
type Observation struct {
	InitTools         []string
	MCPServers        int
	Plugins           int
	CostUSD           float64
	Turns             int
	PermissionDenials int
}

// Verify is the only place an Attestation is minted. It passes only if the
// outside write did not land, the inside write did, the CLI's tool list is
// exactly AllowedTools, no MCP server or non-builtin plugin loaded, and l is
// the launch of this canary's arm.
func (c *Canary) Verify(o Observation, l *Launch, now time.Time) (Attestation, error) {
	fail := func(what string) (Attestation, error) {
		return Attestation{}, fmt.Errorf("%w: %s", ErrCanaryFailed, what)
	}
	if !l.Prepared() {
		return fail("launch did not come from Prepare")
	}
	if a, err := canonPath(l.ArmDir); err != nil || a != c.ArmDir {
		return fail("launch is not for the canary arm")
	}
	if _, err := os.Lstat(filepath.Join(c.OutsideDir, "o.txt")); !errors.Is(err, os.ErrNotExist) {
		return fail("the write outside the arm was not blocked")
	}
	in := filepath.Join(c.ArmDir, "i.txt")
	if fi, err := os.Lstat(in); err != nil || !fi.Mode().IsRegular() {
		return fail("the write inside the arm did not happen")
	}
	if b, err := os.ReadFile(in); err != nil || string(b) != c.token {
		return fail("the file written inside the arm has the wrong content")
	}
	if !sameSet(o.InitTools, AllowedTools()) {
		return fail("the CLI's tool list is not the allowlist")
	}
	if o.MCPServers != 0 {
		return fail("an MCP server is loaded")
	}
	if o.Plugins != 0 {
		return fail("a non-builtin plugin is loaded")
	}
	return Attestation{armDir: c.ArmDir, at: now, minted: true}, nil
}

func sameSet(a, b []string) bool {
	as, bs := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	as, bs = uniq(as), uniq(bs)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func uniq(s []string) []string {
	var out []string
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// RunCanary launches the canary arm for real, observes it and calls Verify.
// The canary's cost is in the returned Observation even on failure, so the
// caller can bill it. A failure returns a zero Attestation.
func RunCanary(ctx context.Context, l *Launch, c *Canary, model string, now func() time.Time) (Attestation, Observation, error) {
	var obs Observation
	if !Supported() {
		return Attestation{}, obs, ErrUnsupported
	}
	if l == nil || c == nil {
		return Attestation{}, obs, errors.New("sandbox: RunCanary needs a launch and a canary")
	}
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithTimeout(ctx, canaryTimeout)
	defer cancel()
	opts := append(l.Options(), claudecode.WithModel(model), claudecode.WithMaxTurns(2), claudecode.WithMaxBudgetUSD(0.05))
	client, err := claudecode.New(c.ArmDir, opts...)
	if err != nil {
		return Attestation{}, obs, fmt.Errorf("sandbox: spawn canary: %w", err)
	}
	defer client.Close()

	updates := make(chan claudecode.Message)
	done := make(chan struct{})
	var sawInit bool
	go func() {
		defer close(done)
		for msg := range updates {
			if sys, ok := msg.(claudecode.SystemMessage); ok && sys.Subtype == "init" && !sawInit {
				sawInit = true
				obs.InitTools, obs.MCPServers, obs.Plugins = parseInit(sys.Raw)
			}
		}
	}()
	res, err := client.Prompt(ctx, c.Prompt(), updates)
	<-done
	obs.CostUSD, obs.Turns, obs.PermissionDenials = res.TotalCostUSD, res.NumTurns, len(res.PermissionDenials)
	if err != nil {
		return Attestation{}, obs, fmt.Errorf("sandbox: canary run: %w", err)
	}
	if res.IsError {
		return Attestation{}, obs, fmt.Errorf("%w: the CLI ended the run with an error result", ErrCanaryFailed)
	}
	att, err := c.Verify(obs, l, now())
	return att, obs, err
}

// parseInit reads the init message: tool names, MCP server count and the
// count of plugins that are not compiled into the CLI.
func parseInit(raw json.RawMessage) (tools []string, mcp, plugins int) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil, 0, 0
	}
	var items []json.RawMessage
	_ = json.Unmarshal(m["tools"], &items)
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			tools = append(tools, s)
		} else {
			tools = append(tools, "?") // an unparsable entry is never in the allowlist
		}
	}
	items = nil
	_ = json.Unmarshal(m["mcp_servers"], &items)
	mcp = len(items)
	items = nil
	_ = json.Unmarshal(m["plugins"], &items)
	for _, it := range items {
		if !isBuiltinPlugin(it) {
			plugins++
		}
	}
	return tools, mcp, plugins
}

// isBuiltinPlugin reports whether one init "plugins" entry is compiled into
// the CLI: path "builtin" and source "<name>@builtin", both must agree.
// Live-confirmed (claude 2.1.293): the init message lists such plugins even
// with --setting-sources= and --strict-mcp-config; they bring no tools.
func isBuiltinPlugin(raw json.RawMessage) bool {
	var p struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Source string `json:"source"`
	}
	return json.Unmarshal(raw, &p) == nil && p.Path == "builtin" && p.Name != "" && p.Source == p.Name+"@builtin"
}
