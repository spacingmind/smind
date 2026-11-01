package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/wsapi"
	"github.com/spacingmind/smind/internal/wsclient"
)

// stewardSessionOver wires a steward-seat MCP session whose tools talk to
// the given wsclient (the validator tests' fake-daemon shape).
func stewardSessionOver(t *testing.T, client *wsclient.Client, env stewardEnv) *mcp.ClientSession {
	t.Helper()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := newStewardMCPServer(client, env).Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// stewardSession is stewardSessionOver against the test daemon.
func stewardSession(t *testing.T, env stewardEnv) *mcp.ClientSession {
	t.Helper()
	base := newMCPSession(t)
	return stewardSessionOver(t, dialTestClient(t, base.srvURL), env)
}

// TestStewardMCPEntry_OnlyTwoToolsByDefault pins the seat's catalogue
// (ADR-0047 §1): exactly its two tools, nothing of the Lead's and nothing
// that acts.
func TestStewardMCPEntry_OnlyTwoToolsByDefault(t *testing.T) {
	cs := stewardSession(t, stewardEnv{ReviewID: 9, Token: "tok"})
	got := mcpToolNames(t, cs)
	sort.Strings(got)
	want := append([]string(nil), steward.SeatMCPTools(false)...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("steward catalogue = %v, want exactly %v", got, want)
	}
	for _, name := range got {
		for _, bad := range []string{"task_", "approve", "merge", "permission", "validation"} {
			if strings.Contains(name, bad) {
				t.Errorf("steward tool %q contains %q: the seat judges, it does not act", name, bad)
			}
		}
	}
}

// stewardSeatVerdict is one well-formed CHANGES verdict against review 9.
func stewardSeatVerdict() steward.SeatVerdict {
	return steward.SeatVerdict{
		V:        1,
		ReviewID: 9,
		Verdict:  steward.VerdictChanges,
		Summary:  "one finding",
		Findings: []steward.SeatFinding{{
			File: "cmd/smind/main.go", Line: 10,
			Why: "off by one", Check: "go test ./cmd/smind/", Severity: "P1",
		}},
	}
}

// callStewardVerdict calls steward_verdict_submit with args and returns
// the rejection text plus whether the call was rejected at all (an
// RPC-level error or a tool result with IsError both count).
func callStewardVerdict(t *testing.T, cs *mcp.ClientSession, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: steward.ToolVerdictSubmit, Arguments: args})
	switch {
	case err != nil:
		return err.Error(), true
	case res.IsError:
		return mcpToolErrorText(res), true
	default:
		return "", false
	}
}

func TestStewardMCPEntry_TokenFromEnvNotArgument(t *testing.T) {
	srv, calls := fakeVerdictDaemon(t)
	client, err := wsclient.Dial(context.Background(), strings.TrimPrefix(srv.URL, "http://"), "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	env := stewardEnv{ReviewID: 9, Token: "seat-token"}
	cs := stewardSessionOver(t, client, env)

	// The tool's input schema offers no token property anywhere.
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	for _, tool := range res.Tools {
		if tool.Name != steward.ToolVerdictSubmit {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
	}
	if schema == nil {
		t.Fatalf("%s is not in the steward catalogue", steward.ToolVerdictSubmit)
	}
	var walk func(node any, path string)
	walk = func(node any, path string) {
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		for k, v := range obj {
			if k == "token" {
				t.Errorf("steward_verdict_submit schema has a token property at %s: the token comes from the seat env only", strings.TrimPrefix(path, "."))
			}
			walk(v, path+"."+k)
		}
	}
	walk(schema, "")

	// The happy path: the daemon receives the env token, the env review id
	// and the verdict body intact.
	want := stewardSeatVerdict()
	raw, _ := json.Marshal(want)
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	if msg, rejected := callStewardVerdict(t, cs, map[string]any{"submission": asMap}); rejected {
		t.Fatalf("verdict submit rejected: %s", msg)
	}
	var got fakeDaemonCall
	select {
	case got = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon never saw the verdict submit")
	}
	if got.method != "steward.verdict.submit" {
		t.Fatalf("method = %s", got.method)
	}
	if got.params["reviewId"] != float64(9) || got.params["token"] != "seat-token" {
		t.Fatalf("params = %v, want reviewId 9 and the env token", got.params)
	}
	verdict, isObject := got.params["submission"].(map[string]any)
	if !isObject {
		t.Fatalf("verdict on the wire is %T, want a JSON object", got.params["submission"])
	}
	if verdict["verdict"] != steward.VerdictChanges || verdict["summary"] != want.Summary {
		t.Fatalf("verdict = %v, want the CHANGES verdict that was sent", verdict)
	}

	// A model-supplied top-level "token" argument is rejected by the
	// tool's strict input schema (additionalProperties: false) before the
	// handler runs, so it never reaches the daemon; the only token the
	// daemon can ever see is the env's.
	msg, rejected := callStewardVerdict(t, cs, map[string]any{"submission": asMap, "token": "model-supplied-token"})
	if !rejected {
		t.Fatalf("a model-supplied token argument was accepted: %s", msg)
	}
	if !strings.Contains(msg, "token") {
		t.Errorf("error = %q, want it to name the rejected token property", msg)
	}
	select {
	case c := <-calls:
		t.Fatalf("the rejected call reached the daemon: %s %v", c.method, c.params)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStewardEnvFromOS(t *testing.T) {
	t.Setenv(steward.EnvReviewID, "")
	t.Setenv(steward.EnvToken, "")
	if _, ok, err := stewardEnvFromOS(); ok || err != nil {
		t.Fatalf("no env must mean no steward seat, got ok=%v err=%v", ok, err)
	}
	t.Setenv(steward.EnvReviewID, "9")
	t.Setenv(steward.EnvToken, "tok")
	if e, ok, err := stewardEnvFromOS(); !ok || err != nil || e.ReviewID != 9 || e.Token != "tok" {
		t.Fatalf("env = %+v ok=%v err=%v, want review 9 and the token", e, ok, err)
	}
	t.Setenv(steward.EnvToken, "")
	if _, ok, err := stewardEnvFromOS(); !ok || err == nil {
		t.Fatalf("a set review id with no token must fail closed, got ok=%v err=%v", ok, err)
	}
	t.Setenv(steward.EnvReviewID, "x")
	if _, ok, err := stewardEnvFromOS(); !ok || err == nil {
		t.Fatalf("a non-numeric review id must fail closed, got ok=%v err=%v", ok, err)
	}
}

func TestMCPServe_BothValidatorAndStewardEnvRefused(t *testing.T) {
	t.Setenv("SMIND_VALIDATION_ID", "7")
	t.Setenv("SMIND_CONTRACT_ID", "3")
	t.Setenv("SMIND_VERDICT_TOKEN", "t")
	t.Setenv(steward.EnvReviewID, "9")
	t.Setenv(steward.EnvToken, "s")
	if _, err := seatServerFor(nil); err == nil || !strings.Contains(err.Error(), "never both") {
		t.Fatalf("both seat envs set: err = %v, want a refusal naming that a seat serves one catalogue", err)
	}

	// The steward env alone serves its own catalogue, nothing of the Lead's.
	t.Setenv("SMIND_VALIDATION_ID", "")
	srv, err := seatServerFor(nil)
	if err != nil {
		t.Fatal(err)
	}
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	got := mcpToolNames(t, cs)
	sort.Strings(got)
	if strings.Join(got, ",") != "steward_digest_get,steward_verdict_submit" {
		t.Fatalf("steward-alone catalogue = %v, want exactly the seat's two tools", got)
	}
}

// TestRunScopedMethods_IncludeStewardSeatCalls keeps wsapi's run-scoped
// allowlist in step with the seat's entry: exactly its two methods, so an
// identified seat run can call them (the handler still checks token and
// run) and nothing else steward-shaped is opened up.
func TestRunScopedMethods_IncludeStewardSeatCalls(t *testing.T) {
	var got []string
	for _, m := range wsapi.RunScopedMethods {
		if strings.HasPrefix(m, "steward.") {
			got = append(got, m)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "steward.digest.get,steward.verdict.submit" {
		t.Fatalf("steward.* methods in RunScopedMethods = %v, want exactly the seat's two", got)
	}
}

// The model sees the renamed outer parameter and every field cap in the
// schema before it submits.
func TestStewardVerdictSchema_NamesSubmissionAndCaps(t *testing.T) {
	raw, err := json.Marshal(stewardVerdictSchema())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if _, old := schema.Properties["verdict"]; old || len(schema.Properties) != 1 {
		t.Fatalf("top-level properties = %v, want only submission", schema.Properties)
	}
	sub, ok := schema.Properties["submission"]
	if !ok {
		t.Fatalf("no submission property: %s", raw)
	}
	field := func(path ...string) map[string]any {
		node := sub
		for _, p := range path {
			props, _ := node["properties"].(map[string]any)
			if p == "[]" {
				node, _ = node["items"].(map[string]any)
				continue
			}
			node, _ = props[p].(map[string]any)
		}
		return node
	}
	for _, c := range []struct {
		path []string
		max  float64
	}{
		{[]string{"blockedReason"}, 200}, {[]string{"summary"}, 800},
		{[]string{"findings", "[]", "why"}, 600}, {[]string{"findings", "[]", "file"}, 300}, {[]string{"findings", "[]", "check"}, 400},
		{[]string{"gates", "[]", "note"}, 300}, {[]string{"frames", "[]", "path"}, 300}, {[]string{"frames", "[]", "observation"}, 1000},
	} {
		f := field(c.path...)
		if f["maxLength"] != c.max {
			t.Errorf("%v maxLength = %v, want %v", c.path, f["maxLength"], c.max)
		}
		if d, _ := f["description"].(string); !strings.Contains(d, "bytes") {
			t.Errorf("%v description %q does not say bytes", c.path, d)
		}
	}
	if field("findings")["maxItems"] != float64(50) {
		t.Errorf("findings maxItems = %v, want 50", field("findings")["maxItems"])
	}
}
