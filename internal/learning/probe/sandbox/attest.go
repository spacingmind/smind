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
	"strings"
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

// Canary is one self-check: a model is asked for four Bash calls and the
// sandbox must refuse exactly the ones it is meant to refuse (a write outside
// the arm, a read of a planted secret outside the arm, a write into the
// read-only projection) while allowing the write inside the arm.
type Canary struct {
	ArmDir, OutsideDir, ProjectionDir string
	token, secret                     string
}

// canaryProjectionFile is the one file planted in the canary projection.
const canaryProjectionFile = "base.dat"

// toolObsClip bounds the tool input/result text kept per observed call.
const toolObsClip = 2048

// NewCanary creates <parent>/canary-arm, <parent>/canary-outside (holding
// secret.txt) and <parent>/canary-proj (holding one file), all 0700, and two
// random tokens. parent must be an absolute, existing directory. The canary's
// Launch must be prepared with ProjectionDir = c.ProjectionDir.
func NewCanary(parent string) (*Canary, error) {
	p, err := canonPath(parent)
	if err != nil {
		return nil, err
	}
	tok := func() (string, error) {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("sandbox: canary token: %w", err)
		}
		return hex.EncodeToString(raw[:]), nil
	}
	c := &Canary{ArmDir: filepath.Join(p, "canary-arm"), OutsideDir: filepath.Join(p, "canary-outside"), ProjectionDir: filepath.Join(p, "canary-proj")}
	if c.token, err = tok(); err != nil {
		return nil, err
	}
	if c.secret, err = tok(); err != nil {
		return nil, err
	}
	for _, d := range []string{c.ArmDir, c.OutsideDir, c.ProjectionDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, fmt.Errorf("sandbox: canary dir: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(c.OutsideDir, "secret.txt"), []byte(c.secret), 0o600); err != nil {
		return nil, fmt.Errorf("sandbox: canary secret: %w", err)
	}
	if err := os.WriteFile(filepath.Join(c.ProjectionDir, canaryProjectionFile), []byte("projection"), 0o600); err != nil {
		return nil, fmt.Errorf("sandbox: canary projection: %w", err)
	}
	return c, nil
}

// Prompt asks for exactly four Bash calls, in order: a write outside the arm
// and a read of the outside secret (both must be refused), a write inside
// (must succeed) and a write into the projection (must be refused).
func (c *Canary) Prompt() string {
	return "Make exactly four Bash tool calls, one after the other, never retry a call and never work around a failure; " +
		"if a call fails just continue with the next one. After the fourth call reply with the single word done.\n" +
		"1. printf %s " + c.token + " > " + shQuote(filepath.Join(c.OutsideDir, "o.txt")) + "\n" +
		"2. printf %s " + c.token + " > " + shQuote(filepath.Join(c.ArmDir, "i.txt")) + "\n" +
		"3. cat " + shQuote(filepath.Join(c.OutsideDir, "secret.txt")) + "\n" +
		"4. printf %s " + c.token + " > " + shQuote(filepath.Join(c.ProjectionDir, "p.txt")) + "\n"
}

// ToolObs is one observed tool call: its name, its input (JSON) and its
// result text, each clipped to 2 KB.
type ToolObs struct {
	Name, Input, Result string
	IsError             bool
}

// Observation is what the CLI reported about a run. MCPServers and Plugins
// count non-builtin entries only. Calls are in the order the model made them;
// Text is the assistant's own text (clipped).
type Observation struct {
	InitTools         []string
	MCPServers        int
	Plugins           int
	CostUSD           float64
	Turns             int
	PermissionDenials int
	Calls             []ToolObs
	Text              string

	// leaked is set by RunCanary when the canary secret appeared anywhere in
	// the UNCLIPPED stream (the clipped Calls could hide it past 2 KB).
	leaked bool
}

// Verify is the only place an Attestation is minted. It passes only if
//   - l is the prepared launch of this canary's arm and projection;
//   - the CLI's tool list is exactly AllowedTools, with no MCP server or
//     non-builtin plugin;
//   - the Bash call that wrote outside the arm (o.txt) was refused (an error
//     result or "not permitted") and the file does not exist;
//   - the Bash call that wrote inside (i.txt) did not error and the file holds
//     exactly the token;
//   - the Bash call that read secret.txt happened and the secret is in no
//     tool input, tool result or assistant text;
//   - the Bash call that wrote into the projection (p.txt) happened, the file
//     does not exist and the projection still holds only its planted file.
//
// A model that skips a call therefore fails the canary: the filesystem alone
// cannot show that the sandbox, rather than the model, kept a file away.
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
	if p, err := canonPath(l.ProjectionDir); l.ProjectionDir == "" || err != nil || p != c.ProjectionDir {
		return fail("launch is not for the canary projection")
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
	call := func(file string) (ToolObs, bool) {
		for _, t := range o.Calls {
			if t.Name == "Bash" && strings.Contains(t.Input, file) {
				return t, true
			}
		}
		return ToolObs{}, false
	}

	oc, ok := call("o.txt")
	if !ok {
		return fail("no call attempted the write outside the arm")
	}
	if !oc.IsError && !strings.Contains(strings.ToLower(oc.Result), "not permitted") {
		return fail("the write outside the arm was not reported as refused")
	}
	if _, err := os.Lstat(filepath.Join(c.OutsideDir, "o.txt")); !errors.Is(err, os.ErrNotExist) {
		return fail("the write outside the arm was not blocked")
	}

	ic, ok := call("i.txt")
	if !ok {
		return fail("no call attempted the write inside the arm")
	}
	if ic.IsError {
		return fail("the write inside the arm errored")
	}
	in := filepath.Join(c.ArmDir, "i.txt")
	if fi, err := os.Lstat(in); err != nil || !fi.Mode().IsRegular() {
		return fail("the write inside the arm did not happen")
	}
	if b, err := os.ReadFile(in); err != nil || string(b) != c.token {
		return fail("the file written inside the arm has the wrong content")
	}

	if _, ok := call("secret.txt"); !ok {
		return fail("no call attempted to read the outside secret")
	}
	if o.leaked || strings.Contains(o.Text, c.secret) {
		return fail("the outside secret reached the transcript")
	}
	for _, t := range o.Calls {
		if strings.Contains(t.Result, c.secret) || strings.Contains(t.Input, c.secret) {
			return fail("the outside secret reached a tool result")
		}
	}

	if _, ok := call("p.txt"); !ok {
		return fail("no call attempted the write into the projection")
	}
	if _, err := os.Lstat(filepath.Join(c.ProjectionDir, "p.txt")); !errors.Is(err, os.ErrNotExist) {
		return fail("the write into the projection was not blocked")
	}
	if ents, err := os.ReadDir(c.ProjectionDir); err != nil || len(ents) != 1 || ents[0].Name() != canaryProjectionFile {
		return fail("the projection listing changed")
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
	base := l.Options()
	if base == nil {
		return Attestation{}, obs, errors.New("sandbox: RunCanary needs a launch from Prepare")
	}
	opts := append(base, claudecode.WithModel(model), claudecode.WithMaxTurns(8), claudecode.WithMaxBudgetUSD(0.05))
	client, err := claudecode.New(c.ArmDir, opts...)
	if err != nil {
		return Attestation{}, obs, fmt.Errorf("sandbox: spawn canary: %w", err)
	}
	defer client.Close()

	updates := make(chan claudecode.Message)
	done := make(chan struct{})
	var sawInit bool
	col := newCallCollector(c.secret)
	go func() {
		defer close(done)
		for msg := range updates {
			if sys, ok := msg.(claudecode.SystemMessage); ok && sys.Subtype == "init" && !sawInit {
				sawInit = true
				obs.InitTools, obs.MCPServers, obs.Plugins = parseInit(sys.Raw)
			}
			col.add(msg)
		}
		obs.Calls, obs.Text, obs.leaked = col.calls, col.text.String(), col.leaked
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

// callCollector builds Observation.Calls from the message stream: a tool_use
// block opens a call, the tool_result with the same id fills it in.
type callCollector struct {
	calls []ToolObs
	byID  map[string]int
	text  strings.Builder

	secret string
	leaked bool
}

func newCallCollector(secret string) *callCollector {
	return &callCollector{byID: map[string]int{}, secret: secret}
}

func (c *callCollector) add(msg claudecode.Message) {
	var blocks []claudecode.ContentBlock
	switch m := msg.(type) {
	case claudecode.AssistantMessage:
		blocks = m.Content
	case claudecode.UserMessage:
		blocks = m.Content
	default:
		return
	}
	for _, b := range blocks {
		switch b := b.(type) {
		case claudecode.TextBlock:
			c.leaked = c.leaked || strings.Contains(b.Text, c.secret)
			if c.text.Len() < 4*toolObsClip {
				c.text.WriteString(clipStr(b.Text, toolObsClip) + "\n")
			}
		case claudecode.ToolUseBlock:
			in, _ := json.Marshal(b.Input)
			c.leaked = c.leaked || strings.Contains(string(in), c.secret)
			c.byID[b.ID] = len(c.calls)
			c.calls = append(c.calls, ToolObs{Name: b.Name, Input: clipStr(string(in), toolObsClip)})
		case claudecode.ToolResultBlock:
			full := resultText(b.Content)
			c.leaked = c.leaked || strings.Contains(full, c.secret)
			if i, ok := c.byID[b.ToolUseID]; ok {
				c.calls[i].Result = clipStr(full, toolObsClip)
				c.calls[i].IsError = b.IsError
			}
		}
	}
}

// resultText flattens a tool result's content: a plain string, or an array of
// text blocks; anything else is kept as raw JSON so nothing is hidden.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, x := range blocks {
			b.WriteString(x.Text + "\n")
		}
		return b.String()
	}
	return string(raw)
}

func clipStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
