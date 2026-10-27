package main

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeValCaller struct {
	method string
	params map[string]any
	result any
}

func (f *fakeValCaller) Call(_ context.Context, method string, params, out any) error {
	f.method = method
	f.params, _ = params.(map[string]any)
	raw, _ := json.Marshal(f.result)
	return json.Unmarshal(raw, out)
}
func (f *fakeValCaller) Close() error { return nil }

func runValCLI(t *testing.T, f *fakeValCaller, files map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	code := runValidation(args, validationIO{out: &o, errOut: &e,
		dial: func(context.Context) (daemonCaller, error) { return f, nil },
		readFile: func(p string) ([]byte, error) {
			if s, ok := files[p]; ok {
				return []byte(s), nil
			}
			return nil, context.Canceled
		}})
	return code, o.String(), e.String()
}

func TestValidationCLI_SubmitSendsFileInline(t *testing.T) {
	f := &fakeValCaller{result: map[string]any{"id": 3, "taskId": 7, "version": 1, "sha256": "abc"}}
	code, out, errOut := runValCLI(t, f, map[string]string{"c.md": "---\nv: 1\n---\n"}, "contract", "submit", "7", "c.md")
	if code != 0 || !strings.Contains(out, "version 1") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if f.method != "validation.contract.submit" || f.params["source"] != "inline" || f.params["body"] != "---\nv: 1\n---\n" {
		t.Fatalf("call = %s %v", f.method, f.params)
	}
}

func TestValidationCLI_ShowPrintsMergeReadyLabel(t *testing.T) {
	f := &fakeValCaller{result: map[string]any{"id": 4, "taskId": 7, "status": "pass", "commitSha": strings.Repeat("a", 40), "contractVersion": 2, "mergeReady": false, "notReadyReason": "stale: the branch head moved after this verdict"}}
	code, out, _ := runValCLI(t, f, nil, "show", "4")
	if code != 0 || !strings.Contains(out, "not merge-ready (stale") || strings.Contains(out, "merge-ready ·") {
		t.Fatalf("exit %d out %q", code, out)
	}
}

func TestValidationCLI_NoMergeOrVerdictCommand(t *testing.T) {
	f := &fakeValCaller{}
	for _, args := range [][]string{{"merge", "1"}, {"verdict", "submit", "1"}, {"waive", "1"}, {}} {
		if code, _, _ := runValCLI(t, f, nil, args...); code != 2 {
			t.Fatalf("%v exit %d, want usage (2)", args, code)
		}
	}
}

func validatorSession(t *testing.T, env validatorEnv) *mcp.ClientSession {
	t.Helper()
	base := newMCPSession(t)
	srv := newValidatorMCPServer(dialTestClient(t, base.srvURL), env)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestValidatorMCPEntry_OnlyTwoTools(t *testing.T) {
	cs := validatorSession(t, validatorEnv{ValidationID: 7, ContractID: 3, Token: "tok"})
	got := mcpToolNames(t, cs)
	sort.Strings(got)
	if strings.Join(got, ",") != "validation_contract_get,validation_verdict_submit" {
		t.Fatalf("validator catalogue = %v, want exactly its two tools", got)
	}
}

func TestValidatorMCPEntry_TokenAndIdsComeFromEnvNotArguments(t *testing.T) {
	cs := validatorSession(t, validatorEnv{ValidationID: 7, ContractID: 3, Token: "tok"})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		raw, _ := json.Marshal(tool.InputSchema)
		for _, forbidden := range []string{"token", "validationId", "runId", "taskId"} {
			if strings.Contains(string(raw), `"`+forbidden+`"`) {
				t.Errorf("%s accepts %q as an argument: %s", tool.Name, forbidden, raw)
			}
		}
	}
	var out map[string]any
	isErr := callMCPTool(t, cs, "validation_verdict_submit", map[string]any{"verdict": map[string]any{"v": 1}}, &out)
	if !isErr {
		t.Fatalf("a verdict for a validation that does not exist must fail")
	}
}

func TestLeadMCPCatalogue_HasValidationToolsButNoVerdictWriter(t *testing.T) {
	env := newMCPSession(t)
	got := strings.Join(mcpToolNames(t, env.cs), ",")
	for _, want := range []string{"validation_contract_submit", "validation_request", "validation_get"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Lead catalogue lacks %s", want)
		}
	}
	if strings.Contains(got, "validation_verdict_submit") || strings.Contains(got, "validation_contract_get") {
		t.Fatalf("the Lead must not see the validator's tools: %s", got)
	}
}

func TestValidatorEnvFromOS(t *testing.T) {
	t.Setenv("SMIND_VALIDATION_ID", "")
	if _, ok, _ := validatorEnvFromOS(); ok {
		t.Fatalf("no env must mean no validator")
	}
	t.Setenv("SMIND_VALIDATION_ID", "5")
	if _, ok, err := validatorEnvFromOS(); !ok || err == nil {
		t.Fatalf("a half-set validator env must fail closed, got ok=%v err=%v", ok, err)
	}
	t.Setenv("SMIND_CONTRACT_ID", "2")
	t.Setenv("SMIND_VERDICT_TOKEN", "t")
	if e, ok, err := validatorEnvFromOS(); !ok || err != nil || e.ValidationID != 5 || e.ContractID != 2 {
		t.Fatalf("env = %+v ok=%v err=%v", e, ok, err)
	}
}
